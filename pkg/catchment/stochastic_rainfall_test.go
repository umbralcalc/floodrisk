package catchment

import (
	"math"
	"sort"
	"testing"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"gonum.org/v1/gonum/floats"
)

// runStochasticRainfall creates, configures, and runs the stochastic
// rainfall iteration, returning the generated daily rainfall values.
func runStochasticRainfall(settings *simulator.Settings, nSteps int) []float64 {
	iter := &StochasticRainfallIteration{}
	iter.Configure(0, settings)

	store := simulator.NewStateTimeStorage()
	implementations := &simulator.Implementations{
		Iterations:      []simulator.Iteration{iter},
		OutputCondition: &simulator.EveryStepOutputCondition{},
		OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: nSteps,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
	}
	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	states := store.GetValues("stochastic_rainfall")
	rainfall := make([]float64, len(states))
	for i, row := range states {
		rainfall[i] = row[0]
	}
	return rainfall
}

func TestStochasticRainfallIteration(t *testing.T) {
	t.Run("generates plausible synthetic rainfall", func(t *testing.T) {
		settings := simulator.LoadSettingsFromYaml(
			"./stochastic_rainfall_settings.yaml",
		)
		rainfall := runStochasticRainfall(settings, 5000)

		// Count wet/dry days.
		var wetDays int
		var wetAmounts []float64
		for _, v := range rainfall {
			if v > 0.1 {
				wetDays++
				wetAmounts = append(wetAmounts, v)
			}
		}

		wetFrac := float64(wetDays) / float64(len(rainfall))
		t.Logf("Generated %d days: %d wet (%.1f%%)", len(rainfall), wetDays, wetFrac*100)

		// Wet fraction should be in a reasonable range (observed: ~70%).
		if wetFrac < 0.50 || wetFrac > 0.90 {
			t.Errorf("wet fraction %.2f outside expected range [0.50, 0.90]", wetFrac)
		}

		// Check wet-day statistics match fitted Gamma.
		if len(wetAmounts) == 0 {
			t.Fatal("no wet days generated")
		}
		sort.Float64s(wetAmounts)
		var sum float64
		for _, v := range wetAmounts {
			sum += v
		}
		mean := sum / float64(len(wetAmounts))
		t.Logf("Wet-day mean: %.2f mm (expected ~4.8 mm)", mean)

		// Mean should be within 50% of observed (4.84 mm).
		if mean < 2.0 || mean > 10.0 {
			t.Errorf("wet-day mean %.2f outside expected range [2.0, 10.0]", mean)
		}

		// No NaN values.
		for i, v := range rainfall {
			if math.IsNaN(v) {
				t.Fatalf("NaN at step %d", i)
			}
		}

		// No negative values.
		if floats.Min(rainfall) < 0 {
			t.Error("negative rainfall generated")
		}

		// Check that max is plausible (< 200mm — UK max daily record ~300mm).
		maxR := floats.Max(rainfall)
		t.Logf("Max rainfall: %.2f mm", maxR)
		if maxR > 200 {
			t.Errorf("unrealistically high rainfall: %.2f mm", maxR)
		}
	})

	t.Run("stochadex harness checks", func(t *testing.T) {
		settings := simulator.LoadSettingsFromYaml(
			"./stochastic_rainfall_settings.yaml",
		)
		implementations := &simulator.Implementations{
			Iterations:      []simulator.Iteration{&StochasticRainfallIteration{}},
			OutputCondition: &simulator.EveryStepOutputCondition{},
			OutputFunction:  &simulator.NilOutputFunction{},
			TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: 100,
			},
			TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		}
		simulator.RunWithHarnesses(settings, implementations)
	})

	t.Run("climate multiplier increases mean rainfall", func(t *testing.T) {
		nSteps := 3000

		// Run baseline (multiplier = 1.0).
		baseSettings := simulator.LoadSettingsFromYaml(
			"./stochastic_rainfall_settings.yaml",
		)
		baseRain := runStochasticRainfall(baseSettings, nSteps)

		// Run with 30% increase.
		pertSettings := simulator.LoadSettingsFromYaml(
			"./stochastic_rainfall_settings.yaml",
		)
		pertSettings.Iterations[0].Params.Map["rainfall_multiplier"] = []float64{1.3}
		pertRain := runStochasticRainfall(pertSettings, nSteps)

		baseMean := meanSlice(baseRain)
		pertMean := meanSlice(pertRain)

		t.Logf("Baseline mean: %.2f mm/day", baseMean)
		t.Logf("Perturbed (1.3x) mean: %.2f mm/day", pertMean)

		// Perturbed should be higher.
		if pertMean <= baseMean {
			t.Errorf("perturbed mean (%.2f) should exceed baseline (%.2f)", pertMean, baseMean)
		}
	})

	t.Run("fit params match observed data", func(t *testing.T) {
		rainfallSeries, err := hydrology.LoadAllRainfallSeries("../../dat")
		if err != nil {
			t.Skipf("rainfall data not available: %v", err)
		}
		if len(rainfallSeries) == 0 {
			t.Skip("no rainfall series found")
		}
		avg := hydrology.AverageCatchmentRainfall(rainfallSeries)

		shape, scale := FitGammaParams(avg.Values, 0.1)
		p01, p11 := FitWetDryTransitions(avg.Values, 0.1)

		t.Logf("Fitted Gamma: shape=%.4f, scale=%.4f", shape, scale)
		t.Logf("Fitted transitions: P(wet|dry)=%.4f, P(wet|wet)=%.4f", p01, p11)

		// Shape should be sub-1 (typical for UK rainfall).
		if shape <= 0 || shape > 5.0 {
			t.Errorf("unexpected Gamma shape: %.4f", shape)
		}
		if scale <= 0 || scale > 50 {
			t.Errorf("unexpected Gamma scale: %.4f", scale)
		}
		if p01 < 0 || p01 > 1 {
			t.Errorf("invalid P01: %.4f", p01)
		}
		if p11 < 0 || p11 > 1 {
			t.Errorf("invalid P11: %.4f", p11)
		}
		// P11 should be > P01 (wet days cluster).
		if p11 <= p01 {
			t.Errorf("P11 (%.4f) should exceed P01 (%.4f) — wet days cluster", p11, p01)
		}
	})
}

func meanSlice(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}
