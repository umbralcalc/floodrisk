package catchment

import (
	"math/rand/v2"
	"sort"
	"testing"
	"time"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// loadAlignedData is a test helper that loads rainfall and Elland flow
// data and aligns them to a common daily series.
func loadAlignedData(t *testing.T) (rainAligned, flowAligned []float64, start time.Time, nDays int) {
	t.Helper()
	rainfallSeries, err := hydrology.LoadAllRainfallSeries("../../dat")
	if err != nil {
		t.Skipf("rainfall data not available: %v", err)
	}
	if len(rainfallSeries) == 0 {
		t.Skip("no rainfall series found")
	}
	avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

	ellandFlow, err := hydrology.LoadTimeSeries(
		"../../dat/flow/elland_daily_flow.csv", "Elland")
	if err != nil {
		t.Skipf("Elland flow data not available: %v", err)
	}

	rainAligned, flowAligned, start, nDays, err = hydrology.AlignDaily(
		avgRainfall, ellandFlow)
	if err != nil {
		t.Fatalf("alignment failed: %v", err)
	}
	return
}

func TestRainfallRunoffValidation(t *testing.T) {
	t.Run(
		"real data validation against Elland flow",
		func(t *testing.T) {
			rainAligned, flowAligned, _, nDays := loadAlignedData(t)
			t.Logf("Aligned %d days of rainfall and flow data", nDays)

			// Build FromStorageIteration with rainfall data.
			rainfallData := hydrology.ToStorageData(rainAligned)

			// Load settings and build iterations.
			settings := simulator.LoadSettingsFromYaml(
				"./validation_settings.yaml",
			)
			settings.Iterations[0].InitStateValues = rainfallData[0]

			rainfallIter := &general.FromStorageIteration{Data: rainfallData}
			runoffIter := &RainfallRunoffIteration{}

			store := simulator.NewStateTimeStorage()
			implementations := &simulator.Implementations{
				Iterations:      []simulator.Iteration{rainfallIter, runoffIter},
				OutputCondition: &simulator.EveryStepOutputCondition{},
				OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
				TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
					MaxNumberOfSteps: nDays - 1,
				},
				TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
			}
			coordinator := simulator.NewPartitionCoordinator(
				settings,
				implementations,
			)
			coordinator.Run()

			simStates := store.GetValues("rainfall_runoff")
			simFlow := make([]float64, len(simStates))
			for i, row := range simStates {
				simFlow[i] = row[1]
			}

			spinUp := 30
			if len(simFlow) <= spinUp || len(flowAligned) <= spinUp {
				t.Fatalf("not enough data for spin-up: sim=%d, obs=%d",
					len(simFlow), len(flowAligned))
			}

			obsWindow := flowAligned[1+spinUp:]
			simWindow := simFlow[spinUp:]
			n := len(obsWindow)
			if len(simWindow) < n {
				n = len(simWindow)
			}
			obsWindow = obsWindow[:n]
			simWindow = simWindow[:n]

			nse := hydrology.NashSutcliffe(obsWindow, simWindow)
			rmse := hydrology.RMSE(obsWindow, simWindow)
			peakBias := hydrology.PeakFlowBias(obsWindow, simWindow)
			volErr := hydrology.VolumeError(obsWindow, simWindow)

			t.Logf("Validation metrics (%d days after %d-day spin-up):", n, spinUp)
			t.Logf("  Nash-Sutcliffe Efficiency: %.4f", nse)
			t.Logf("  RMSE: %.4f m³/s", rmse)
			t.Logf("  Peak flow bias: %.4f", peakBias)
			t.Logf("  Volume error: %.4f", volErr)

			if nse < -5.0 {
				t.Errorf("NSE=%.4f is extremely poor, model may be broken", nse)
			}
			if rmse <= 0 {
				t.Errorf("RMSE should be positive, got %.4f", rmse)
			}
		},
	)
}

func TestTemporalHoldoutValidation(t *testing.T) {
	t.Run(
		"calibrate on 2010-2022 and validate on 2023-2025",
		func(t *testing.T) {
			rainAligned, flowAligned, start, nDays := loadAlignedData(t)
			t.Logf("Total aligned data: %d days starting %s", nDays, start.Format("2006-01-02"))

			// Split at 2023-01-01.
			splitDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
			splitIdx := hydrology.DayIndex(start, splitDate)
			if splitIdx <= 0 || splitIdx >= nDays {
				t.Fatalf("split index %d is out of range [1, %d)", splitIdx, nDays)
			}
			t.Logf("Train: %d days, Test: %d days (split at %s)",
				splitIdx, nDays-splitIdx, splitDate.Format("2006-01-02"))

			rng := rand.New(rand.NewPCG(42, 99))
			result := ValidateHoldout(
				rainAligned, flowAligned, splitIdx,
				DefaultBounds(), 1000, 30, rng,
			)

			t.Logf("Training period metrics:")
			t.Logf("  NSE:          %.4f", result.TrainResult.NSE)
			t.Logf("  RMSE:         %.4f m³/s", result.TrainResult.RMSE)
			t.Logf("  Peak bias:    %.4f", result.TrainResult.PeakBias)
			t.Logf("  Volume error: %.4f", result.TrainResult.VolumeError)

			t.Logf("Holdout period metrics (2023-2025):")
			t.Logf("  NSE:          %.4f", result.TestNSE)
			t.Logf("  RMSE:         %.4f m³/s", result.TestRMSE)
			t.Logf("  Peak bias:    %.4f", result.TestPeakBias)
			t.Logf("  Volume error: %.4f", result.TestVolumeError)

			t.Logf("Calibrated params:")
			for k, v := range result.TrainResult.Params {
				if k == "upstream_partition" {
					continue
				}
				t.Logf("  %-25s %.4f", k, v[0])
			}

			// The holdout NSE may be lower than training, but should not
			// be catastrophically bad — that would indicate overfitting.
			if result.TestNSE < -5.0 {
				t.Errorf("holdout NSE=%.4f is extremely poor — likely overfitting", result.TestNSE)
			}
			// Train NSE should be positive (calibration worked).
			if result.TrainResult.NSE <= 0 {
				t.Errorf("training NSE=%.4f — calibration failed to beat mean", result.TrainResult.NSE)
			}
		},
	)
}

func TestFloodEventReproduction(t *testing.T) {
	t.Run(
		"calibrated model reproduces major flood events",
		func(t *testing.T) {
			rainAligned, flowAligned, start, _ := loadAlignedData(t)

			// Calibrate on full dataset.
			rainfallData := hydrology.ToStorageData(rainAligned)
			rng := rand.New(rand.NewPCG(42, 99))
			calResult := Calibrate(rainfallData, flowAligned, DefaultBounds(), 1000, 30, rng)

			t.Logf("Calibration NSE: %.4f", calResult.NSE)

			// Run model with best params.
			nSteps := len(rainfallData) - 1
			simFlow := RunModel(rainfallData, calResult.Params, nSteps)

			// Align simFlow with flowAligned: simFlow[i] corresponds to
			// flowAligned[i+1].
			obsFlow := flowAligned[1:]
			if len(simFlow) < len(obsFlow) {
				obsFlow = obsFlow[:len(simFlow)]
			} else {
				simFlow = simFlow[:len(obsFlow)]
			}

			// Detect flood events using P95 threshold.
			flowStats := basicStats(obsFlow)
			threshold := flowStats.p95
			t.Logf("Flood event threshold (P95): %.2f m³/s", threshold)

			spinUp := 30
			events := EvaluateFloodEvents(obsFlow, simFlow, threshold, spinUp)

			// Sort events by observed peak (largest first).
			sort.Slice(events, func(i, j int) bool {
				return events[i].ObsPeak > events[j].ObsPeak
			})

			t.Logf("Detected %d flood events above P95 threshold", len(events))
			if len(events) == 0 {
				t.Fatal("no flood events detected — threshold may be wrong")
			}

			// Report top 10 events.
			nReport := 10
			if len(events) < nReport {
				nReport = len(events)
			}
			t.Logf("\nTop %d flood events by observed peak:", nReport)
			t.Logf("  %-8s  %-8s  %10s  %10s  %8s",
				"Start", "End", "Obs(m³/s)", "Sim(m³/s)", "Bias")
			for _, e := range events[:nReport] {
				startDate := start.AddDate(0, 0, e.StartDay+1) // +1 for sim offset
				endDate := start.AddDate(0, 0, e.EndDay+1)
				t.Logf("  %-8s  %-8s  %10.2f  %10.2f  %+8.2f",
					startDate.Format("06-01-02"),
					endDate.Format("06-01-02"),
					e.ObsPeak, e.SimPeak, e.PeakBias)
			}

			// Check the largest event (likely Boxing Day 2015).
			largest := events[0]
			largestDate := start.AddDate(0, 0, largest.StartDay+1)
			t.Logf("\nLargest event: %s, obs=%.2f m³/s, sim=%.2f m³/s, bias=%.2f",
				largestDate.Format("2006-01-02"), largest.ObsPeak, largest.SimPeak, largest.PeakBias)

			// The model should at least detect that large events produce
			// elevated flow — sim peak should be above the median flow.
			medianFlow := flowStats.median
			if largest.SimPeak <= medianFlow {
				t.Errorf("simulated peak (%.2f) for largest event is at or below median flow (%.2f) — model not responding to extreme rainfall",
					largest.SimPeak, medianFlow)
			}

			// Compute aggregate event reproduction metrics.
			var sumAbsBias float64
			for _, e := range events {
				if e.PeakBias < 0 {
					sumAbsBias -= e.PeakBias
				} else {
					sumAbsBias += e.PeakBias
				}
			}
			meanAbsBias := sumAbsBias / float64(len(events))
			t.Logf("Mean absolute peak bias across %d events: %.4f", len(events), meanAbsBias)
		},
	)
}

type flowStats struct {
	p95    float64
	median float64
}

func basicStats(values []float64) flowStats {
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
	n := len(sorted)
	p95Idx := int(0.95 * float64(n-1))
	medIdx := n / 2
	return flowStats{
		p95:    sorted[p95Idx],
		median: sorted[medIdx],
	}
}
