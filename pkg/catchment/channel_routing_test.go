package catchment

import (
	"testing"

	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"gonum.org/v1/gonum/floats/scalar"
)

func TestChannelRouting(t *testing.T) {
	t.Run("harness", func(t *testing.T) {
		settings := simulator.LoadSettingsFromYaml("channel_routing_settings.yaml")
		implementations := &simulator.Implementations{
			Iterations: []simulator.Iteration{
				&general.ParamValuesIteration{},
				&RainfallRunoffIteration{},
				&RainfallRunoffIteration{},
				&ChannelRoutingIteration{},
			},
			OutputCondition:      &simulator.EveryStepOutputCondition{},
			OutputFunction:       &simulator.NilOutputFunction{},
			TerminationCondition: &simulator.NumberOfStepsTerminationCondition{MaxNumberOfSteps: 50},
			TimestepFunction:     &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		}
		if err := simulator.RunWithHarnesses(settings, implementations); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("pass-through sums flows", func(t *testing.T) {
		// With K=1.0 (default, pass-through), routing should sum upstream flows.
		// Use the harness settings which have two runoff partitions feeding routing.
		settings := simulator.LoadSettingsFromYaml("channel_routing_settings.yaml")
		// Override routing coefficients to 1.0 (pass-through).
		settings.Iterations[3].Params.Map["routing_coefficients"] = []float64{1.0, 1.0}

		iterations := []simulator.Iteration{
			&general.ParamValuesIteration{},
			&RainfallRunoffIteration{},
			&RainfallRunoffIteration{},
			&ChannelRoutingIteration{},
		}
		for i, iter := range iterations {
			iter.Configure(i, settings)
		}

		store := simulator.NewStateTimeStorage()
		implementations := &simulator.Implementations{
			Iterations: iterations,
			OutputCondition: &simulator.EveryStepOutputCondition{},
			OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
			TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: 50,
			},
			TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		}
		coordinator := simulator.NewPartitionCoordinator(settings, implementations)
		coordinator.Run()

		runoffA := store.GetValues("runoff_a")
		runoffB := store.GetValues("runoff_b")
		routed := store.GetValues("routing")
		last := len(routed) - 1

		// Total routed flow should equal sum of upstream total flows.
		expectedTotal := runoffA[last][1] + runoffB[last][1]
		if !scalar.EqualWithinAbsOrRel(routed[last][0], expectedTotal, 0.01, 0.01) {
			t.Errorf("total routed flow = %.4f, want %.4f (A=%.4f + B=%.4f)",
				routed[last][0], expectedTotal, runoffA[last][1], runoffB[last][1])
		}
	})

	t.Run("attenuation smooths response", func(t *testing.T) {
		// With K<1, routing should produce a smoothed/lagged response.
		settings := simulator.LoadSettingsFromYaml("channel_routing_settings.yaml")
		// Set low K for both upstreams.
		settings.Iterations[3].Params.Map["routing_coefficients"] = []float64{0.3, 0.3}

		iterations := []simulator.Iteration{
			&general.ParamValuesIteration{},
			&RainfallRunoffIteration{},
			&RainfallRunoffIteration{},
			&ChannelRoutingIteration{},
		}
		for i, iter := range iterations {
			iter.Configure(i, settings)
		}

		store := simulator.NewStateTimeStorage()
		implementations := &simulator.Implementations{
			Iterations: iterations,
			OutputCondition: &simulator.EveryStepOutputCondition{},
			OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
			TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
				MaxNumberOfSteps: 50,
			},
			TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		}
		coordinator := simulator.NewPartitionCoordinator(settings, implementations)
		coordinator.Run()

		runoffA := store.GetValues("runoff_a")
		runoffB := store.GetValues("runoff_b")
		routed := store.GetValues("routing")
		last := len(routed) - 1

		// With attenuation, at steady state the routed flow should converge
		// to the sum of upstream flows.
		expectedTotal := runoffA[last][1] + runoffB[last][1]
		if !scalar.EqualWithinAbsOrRel(routed[last][0], expectedTotal, 0.5, 0.05) {
			t.Errorf("converged routed flow = %.4f, want ~%.4f", routed[last][0], expectedTotal)
		}

		// Early routed flow should be less than converged (attenuation delays response).
		if routed[0][0] >= routed[last][0] {
			t.Errorf("expected early flow (%.4f) < converged flow (%.4f)", routed[0][0], routed[last][0])
		}
	})
}
