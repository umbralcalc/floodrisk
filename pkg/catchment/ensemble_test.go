package catchment

import (
	"math/rand/v2"
	"testing"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

func TestRunStochasticModel(t *testing.T) {
	t.Run("synthetic rainfall drives plausible flow", func(t *testing.T) {
		// Use realistic params (from calibration).
		params := map[string][]float64{
			"field_capacity":      {330.0},
			"drainage_rate":       {0.03},
			"et_rate":             {1.1},
			"runoff_shape":        {2.7},
			"fast_recession_rate": {0.40},
			"slow_recession_rate": {0.32},
			"catchment_area_km2":  {297.0},
			"upstream_partition":  {0},
		}

		rainfallParams := RainfallParams{
			WetDayShape:        0.609,
			WetDayScale:        7.944,
			PWetGivenDry:       0.40,
			PWetGivenWet:       0.83,
			RainfallMultiplier: 1.0,
			WetThreshold:       0.1,
		}

		result := RunStochasticModel(params, rainfallParams, 3650, 42)

		t.Logf("3650-day simulation (10 years):")
		t.Logf("  Peak flow: %.2f m³/s", result.PeakFlow)
		t.Logf("  Mean flow: %.2f m³/s", result.MeanFlow)
		t.Logf("  Rainfall days: %d", len(result.Rainfall))
		t.Logf("  Flow days: %d", len(result.SimFlow))

		// Peak should be positive and not insane.
		if result.PeakFlow <= 0 {
			t.Error("peak flow should be positive")
		}
		if result.PeakFlow > 1000 {
			t.Errorf("peak flow %.2f unrealistically high", result.PeakFlow)
		}
		// Mean flow should be positive.
		if result.MeanFlow <= 0 {
			t.Error("mean flow should be positive")
		}
	})
}

func TestRunEnsemble(t *testing.T) {
	t.Run("ensemble produces consistent statistics", func(t *testing.T) {
		// First calibrate to get realistic params.
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

		rainAligned, flowAligned, _, _, err := hydrology.AlignDaily(
			avgRainfall, ellandFlow)
		if err != nil {
			t.Fatalf("alignment failed: %v", err)
		}

		// Quick calibration.
		rainfallData := hydrology.ToStorageData(rainAligned)
		rng := rand.New(rand.NewPCG(42, 99))
		calResult := Calibrate(rainfallData, flowAligned, DefaultBounds(), 500, 30, rng)
		t.Logf("Calibration NSE: %.4f", calResult.NSE)

		// Fit rainfall params from observed data.
		shape, scale := FitGammaParams(avgRainfall.Values, 0.1)
		p01, p11 := FitWetDryTransitions(avgRainfall.Values, 0.1)

		rainfallParams := RainfallParams{
			WetDayShape:        shape,
			WetDayScale:        scale,
			PWetGivenDry:       p01,
			PWetGivenWet:       p11,
			RainfallMultiplier: 1.0,
			WetThreshold:       0.1,
		}

		// Run a small ensemble.
		nMembers := 20
		nSteps := 3650 // 10 years
		_, summary := RunEnsemble(
			calResult.Params, rainfallParams,
			nSteps, nMembers, 100, 30,
		)

		t.Logf("Ensemble summary (%d members, %d days each):", nMembers, nSteps)
		t.Logf("  Mean peak flow:  %.2f m³/s", summary.MeanPeakFlow)
		t.Logf("  Std peak flow:   %.2f m³/s", summary.StdPeakFlow)
		t.Logf("  Max peak flow:   %.2f m³/s", summary.MaxPeakFlow)
		t.Logf("  P95 peak flow:   %.2f m³/s", summary.P95PeakFlow)
		t.Logf("  Mean mean flow:  %.2f m³/s", summary.MeanMeanFlow)

		// Basic sanity checks.
		if summary.MeanPeakFlow <= 0 {
			t.Error("mean peak flow should be positive")
		}
		if summary.StdPeakFlow < 0 {
			t.Error("std should be non-negative")
		}
		if summary.MaxPeakFlow < summary.MeanPeakFlow {
			t.Error("max peak should be >= mean peak")
		}
		if summary.P95PeakFlow < summary.MeanPeakFlow {
			t.Error("P95 should be >= mean")
		}
	})

	t.Run("climate perturbation increases peak flows", func(t *testing.T) {
		params := map[string][]float64{
			"field_capacity":      {330.0},
			"drainage_rate":       {0.03},
			"et_rate":             {1.1},
			"runoff_shape":        {2.7},
			"fast_recession_rate": {0.40},
			"slow_recession_rate": {0.32},
			"catchment_area_km2":  {297.0},
			"upstream_partition":  {0},
		}

		baseRainfall := RainfallParams{
			WetDayShape:        0.609,
			WetDayScale:        7.944,
			PWetGivenDry:       0.40,
			PWetGivenWet:       0.83,
			RainfallMultiplier: 1.0,
			WetThreshold:       0.1,
		}

		// 20% intensity increase (representative of UKCP18 RCP4.5 2050s).
		perturbedRainfall := baseRainfall
		perturbedRainfall.RainfallMultiplier = 1.2

		nMembers := 20
		nSteps := 3650

		_, baseSummary := RunEnsemble(params, baseRainfall, nSteps, nMembers, 200, 30)
		_, pertSummary := RunEnsemble(params, perturbedRainfall, nSteps, nMembers, 200, 30)

		t.Logf("Baseline  — mean peak: %.2f, P95 peak: %.2f m³/s",
			baseSummary.MeanPeakFlow, baseSummary.P95PeakFlow)
		t.Logf("Perturbed — mean peak: %.2f, P95 peak: %.2f m³/s",
			pertSummary.MeanPeakFlow, pertSummary.P95PeakFlow)

		if pertSummary.MeanPeakFlow <= baseSummary.MeanPeakFlow {
			t.Errorf("perturbed mean peak (%.2f) should exceed baseline (%.2f)",
				pertSummary.MeanPeakFlow, baseSummary.MeanPeakFlow)
		}
	})
}
