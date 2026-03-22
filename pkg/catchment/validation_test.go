package catchment

import (
	"testing"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

func TestRainfallRunoffValidation(t *testing.T) {
	t.Run(
		"real data validation against Elland flow",
		func(t *testing.T) {
			// Load all rainfall series.
			rainfallSeries, err := hydrology.LoadAllRainfallSeries("../../dat")
			if err != nil {
				t.Skipf("rainfall data not available: %v", err)
			}
			if len(rainfallSeries) == 0 {
				t.Skip("no rainfall series found")
			}

			// Compute catchment-average rainfall.
			avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

			// Load Elland observed flow.
			ellandFlow, err := hydrology.LoadTimeSeries(
				"../../dat/flow/elland_daily_flow.csv", "Elland")
			if err != nil {
				t.Skipf("Elland flow data not available: %v", err)
			}

			// Align to common daily timesteps.
			rainAligned, flowAligned, _, nDays, err := hydrology.AlignDaily(
				avgRainfall, ellandFlow)
			if err != nil {
				t.Fatalf("alignment failed: %v", err)
			}
			t.Logf("Aligned %d days of rainfall and flow data", nDays)

			// Build FromStorageIteration with rainfall data.
			rainfallData := hydrology.ToStorageData(rainAligned)

			// Load settings and build iterations.
			settings := simulator.LoadSettingsFromYaml(
				"./validation_settings.yaml",
			)
			// Set initial rainfall state to first data value.
			settings.Iterations[0].InitStateValues = rainfallData[0]

			rainfallIter := &general.FromStorageIteration{Data: rainfallData}
			runoffIter := &RainfallRunoffIteration{}

			// Run simulation.
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

			// Extract simulated flow (state index 1 of rainfall_runoff partition).
			simStates := store.GetValues("rainfall_runoff")
			simFlow := make([]float64, len(simStates))
			for i, row := range simStates {
				simFlow[i] = row[1]
			}

			// Compute metrics after a 30-day spin-up period.
			spinUp := 30
			if len(simFlow) <= spinUp || len(flowAligned) <= spinUp {
				t.Fatalf("not enough data for spin-up: sim=%d, obs=%d",
					len(simFlow), len(flowAligned))
			}

			// The store captures outputs starting from step 1, so simFlow
			// corresponds to flowAligned[1:]. Adjust observed flow to match.
			obsWindow := flowAligned[1+spinUp:]
			simWindow := simFlow[spinUp:]
			// Trim to shorter length if they differ.
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

			// Sanity assertions — not tight, just "model isn't broken".
			if nse < -5.0 {
				t.Errorf("NSE=%.4f is extremely poor, model may be broken", nse)
			}
			if rmse <= 0 {
				t.Errorf("RMSE should be positive, got %.4f", rmse)
			}
		},
	)
}
