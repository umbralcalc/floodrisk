package flooddash

import (
	"math"
	"testing"

	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// TestSimulationRunsWithoutPanic exercises the BuildFloodSimulation
// generator through simulator.RunWithHarnesses, which checks for
// nil-pointer panics, NaN outputs, wrong state widths, params
// mutation, state history integrity, and statefulness residues. This
// is the project-convention sanity test for any stochadex iteration
// (see pkg/catchment/*_test.go for the matching pattern).
//
// Runs 5 outer steps (= 5 ensemble members), enough to make sure
// every partition's Iterate is exercised at least once including the
// "received first member" branches (peak_stats, histogram_bars).
func TestSimulationRunsWithoutPanic(t *testing.T) {
	gen := BuildFloodSimulation()
	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition: &simulator.EveryStepOutputCondition{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 5,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:    0.0,
	})
	settings, implementations := gen.GenerateConfigs()
	simulator.RunWithHarnesses(settings, implementations)
}

// TestEnsembleMemberProducesPositivePeak runs the simulation for a
// few outer steps and checks that every member's emitted peak flow
// is finite and strictly positive — a zero or NaN peak would mean
// the inner stochastic rainfall-runoff blew up, which is the most
// likely class of bug from a parameter mismatch.
func TestEnsembleMemberProducesPositivePeak(t *testing.T) {
	gen := BuildFloodSimulation()
	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition: &simulator.EveryStepOutputCondition{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 3,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:    0.0,
	})
	settings, implementations := gen.GenerateConfigs()

	store := simulator.NewStateTimeStorage()
	implementations.OutputFunction = &simulator.StateTimeStorageOutputFunction{Store: store}

	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	values := store.GetValues("ensemble_member")
	if len(values) < 2 {
		t.Fatalf("expected at least 2 ensemble_member outputs, got %d", len(values))
	}
	// Skip the t=0 init row; check rows 1..N actually contain runs.
	for i, row := range values[1:] {
		peak := row[0]
		if math.IsNaN(peak) || math.IsInf(peak, 0) {
			t.Errorf("member %d emitted non-finite peak: %v", i, peak)
		}
		if peak <= 0 {
			t.Errorf("member %d emitted non-positive peak: %v", i, peak)
		}
	}
}

// TestHistogramBarsAccumulate checks that the histogram partition
// produces non-zero bar heights after a few ensemble members have
// completed — otherwise the visualisation will stay empty even when
// the simulation is running fine.
func TestHistogramBarsAccumulate(t *testing.T) {
	gen := BuildFloodSimulation()
	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition: &simulator.EveryStepOutputCondition{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: 5,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:    0.0,
	})
	settings, implementations := gen.GenerateConfigs()

	store := simulator.NewStateTimeStorage()
	implementations.OutputFunction = &simulator.StateTimeStorageOutputFunction{Store: store}

	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	final := store.GetValues("histogram_bars")
	if len(final) == 0 {
		t.Fatal("expected histogram_bars output rows, got none")
	}
	last := final[len(final)-1]
	totalBarHeight := 0.0
	for i := 0; i < HistNBins; i++ {
		totalBarHeight += last[i*4+3] // h is the 4th field of each (x,y,w,h) group
	}
	if totalBarHeight == 0 {
		t.Errorf("expected at least one non-zero histogram bin after 5 members, got 0")
	}
}
