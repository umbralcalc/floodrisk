package catchment

import (
	"testing"

	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

func TestRainfallRunoff(t *testing.T) {
	t.Run(
		"test that rainfall-runoff produces sensible steady-state flow",
		func(t *testing.T) {
			settings := simulator.LoadSettingsFromYaml(
				"./rainfall_runoff_settings.yaml",
			)
			iterations := []simulator.Iteration{
				&general.ParamValuesIteration{},
				&RainfallRunoffIteration{},
			}
			for i, iter := range iterations {
				iter.Configure(i, settings)
			}
			store := simulator.NewStateTimeStorage()
			implementations := &simulator.Implementations{
				Iterations:      iterations,
				OutputCondition: &simulator.EveryStepOutputCondition{},
				OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
				TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
					MaxNumberOfSteps: 500,
				},
				TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
			}
			coordinator := simulator.NewPartitionCoordinator(
				settings,
				implementations,
			)
			coordinator.Run()

			// After 500 days of constant 10mm/day rainfall with 1.5mm/day ET,
			// the model should reach a steady state. Check that:
			// 1. Soil moisture is positive and <= field capacity.
			// 2. Flow is positive (catchment is generating runoff).
			finalStates := store.GetValues("rainfall_runoff")
			lastRow := len(finalStates) - 1
			soilMoisture := finalStates[lastRow][0]
			flow := finalStates[lastRow][1]

			if soilMoisture < 0 || soilMoisture > 200.0+1e-6 {
				t.Errorf("soil moisture %.2f outside valid range [0, 200]", soilMoisture)
			}
			if flow <= 0 {
				t.Errorf("expected positive flow at steady state, got %.4f", flow)
			}
			// With 10mm rain, 1.5mm ET, 200km² catchment, we expect roughly:
			// Net rainfall = 8.5 mm/day
			// At steady state, soil hits capacity → excess ≈ net_rain - drainage
			// Rough expected flow order of magnitude: ~10-30 m³/s
			if flow > 200.0 {
				t.Errorf("flow %.2f seems unreasonably large", flow)
			}
		},
	)
	t.Run(
		"test that rainfall-runoff runs with harnesses",
		func(t *testing.T) {
			settings := simulator.LoadSettingsFromYaml(
				"./rainfall_runoff_settings.yaml",
			)
			iterations := []simulator.Iteration{
				&general.ParamValuesIteration{},
				&RainfallRunoffIteration{},
			}
			store := simulator.NewStateTimeStorage()
			implementations := &simulator.Implementations{
				Iterations:      iterations,
				OutputCondition: &simulator.EveryStepOutputCondition{},
				OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
				TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
					MaxNumberOfSteps: 100,
				},
				TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
			}
			if err := simulator.RunWithHarnesses(settings, implementations); err != nil {
				t.Errorf("test harness failed: %v", err)
			}
		},
	)
}
