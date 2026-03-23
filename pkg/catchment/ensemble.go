package catchment

import (
	"math"

	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// RainfallParams holds the stochastic rainfall generator parameters.
type RainfallParams struct {
	WetDayShape       float64 // Gamma shape
	WetDayScale       float64 // Gamma scale
	PWetGivenDry      float64 // P(wet|dry)
	PWetGivenWet      float64 // P(wet|wet)
	RainfallMultiplier float64 // climate change factor (1.0 = baseline)
	WetThreshold      float64 // mm threshold for wet/dry
}

// EnsembleResult holds the output of a single ensemble member.
type EnsembleResult struct {
	SimFlow  []float64
	Rainfall []float64
	PeakFlow float64
	MeanFlow float64
}

// EnsembleSummary holds aggregate statistics across ensemble members.
type EnsembleSummary struct {
	NMembers     int
	MeanPeakFlow float64
	StdPeakFlow  float64
	MaxPeakFlow  float64
	MeanMeanFlow float64
	P95PeakFlow  float64
}

// RunStochasticModel runs the rainfall-runoff model driven by the
// stochastic rainfall generator. Returns simulated flow and rainfall.
func RunStochasticModel(
	runoffParams map[string][]float64,
	rainfallParams RainfallParams,
	nSteps int,
	seed uint64,
) EnsembleResult {
	rainfallParamMap := map[string][]float64{
		"wet_day_shape":       {rainfallParams.WetDayShape},
		"wet_day_scale":       {rainfallParams.WetDayScale},
		"p_wet_given_dry":     {rainfallParams.PWetGivenDry},
		"p_wet_given_wet":     {rainfallParams.PWetGivenWet},
		"rainfall_multiplier": {rainfallParams.RainfallMultiplier},
		"wet_threshold":       {rainfallParams.WetThreshold},
	}

	settings := &simulator.Settings{
		Iterations: []simulator.IterationSettings{
			{
				Name:              "stochastic_rainfall",
				Params:            simulator.Params{Map: rainfallParamMap},
				InitStateValues:   []float64{0.0},
				Seed:              seed,
				StateWidth:        1,
				StateHistoryDepth: 2,
			},
			{
				Name:              "rainfall_runoff",
				Params:            simulator.Params{Map: runoffParams},
				InitStateValues:   []float64{100.0, 0.0, 0.0, 0.0},
				StateWidth:        4,
				StateHistoryDepth: 2,
			},
		},
		InitTimeValue:         0.0,
		TimestepsHistoryDepth: 2,
	}
	settings.Init()

	rainfallIter := &StochasticRainfallIteration{}
	runoffIter := &RainfallRunoffIteration{}
	rainfallIter.Configure(0, settings)
	runoffIter.Configure(1, settings)

	store := simulator.NewStateTimeStorage()
	implementations := &simulator.Implementations{
		Iterations:      []simulator.Iteration{rainfallIter, runoffIter},
		OutputCondition: &simulator.EveryStepOutputCondition{},
		OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: nSteps,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
	}
	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	// Extract rainfall.
	rainStates := store.GetValues("stochastic_rainfall")
	rainfall := make([]float64, len(rainStates))
	for i, row := range rainStates {
		rainfall[i] = row[0]
	}

	// Extract flow.
	flowStates := store.GetValues("rainfall_runoff")
	simFlow := make([]float64, len(flowStates))
	peakFlow := 0.0
	sumFlow := 0.0
	for i, row := range flowStates {
		simFlow[i] = row[1]
		if row[1] > peakFlow {
			peakFlow = row[1]
		}
		sumFlow += row[1]
	}

	return EnsembleResult{
		SimFlow:  simFlow,
		Rainfall: rainfall,
		PeakFlow: peakFlow,
		MeanFlow: sumFlow / float64(len(simFlow)),
	}
}

// RunEnsemble runs multiple realisations of the stochastic rainfall-runoff
// model and returns per-member results and an aggregate summary.
// Each member uses a different seed (baseSeed + memberIndex).
func RunEnsemble(
	runoffParams map[string][]float64,
	rainfallParams RainfallParams,
	nSteps int,
	nMembers int,
	baseSeed uint64,
	spinUp int,
) ([]EnsembleResult, EnsembleSummary) {
	results := make([]EnsembleResult, nMembers)
	peakFlows := make([]float64, nMembers)

	var sumPeak, sumMean float64
	maxPeak := 0.0

	for i := 0; i < nMembers; i++ {
		result := RunStochasticModel(
			runoffParams, rainfallParams,
			nSteps, baseSeed+uint64(i),
		)

		// Compute peak/mean after spin-up.
		peak := 0.0
		flowSum := 0.0
		n := 0
		for j := spinUp; j < len(result.SimFlow); j++ {
			if result.SimFlow[j] > peak {
				peak = result.SimFlow[j]
			}
			flowSum += result.SimFlow[j]
			n++
		}
		result.PeakFlow = peak
		if n > 0 {
			result.MeanFlow = flowSum / float64(n)
		}

		results[i] = result
		peakFlows[i] = peak
		sumPeak += peak
		sumMean += result.MeanFlow
		if peak > maxPeak {
			maxPeak = peak
		}
	}

	meanPeak := sumPeak / float64(nMembers)
	var varPeak float64
	for _, p := range peakFlows {
		d := p - meanPeak
		varPeak += d * d
	}
	varPeak /= float64(nMembers)

	// P95 of peak flows.
	sorted := make([]float64, len(peakFlows))
	copy(sorted, peakFlows)
	sortFloat64s(sorted)
	p95Idx := int(0.95 * float64(len(sorted)-1))

	return results, EnsembleSummary{
		NMembers:     nMembers,
		MeanPeakFlow: meanPeak,
		StdPeakFlow:  math.Sqrt(varPeak),
		MaxPeakFlow:  maxPeak,
		MeanMeanFlow: sumMean / float64(nMembers),
		P95PeakFlow:  sorted[p95Idx],
	}
}

func sortFloat64s(s []float64) {
	// Simple insertion sort for small slices.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
