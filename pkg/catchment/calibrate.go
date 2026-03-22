package catchment

import (
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// ParamBounds defines the search range for each calibration parameter.
type ParamBounds struct {
	FieldCapacity      [2]float64 // [min, max] mm
	DrainageRate       [2]float64 // [min, max] fraction/day
	ETRate             [2]float64 // [min, max] mm/day
	RunoffShape        [2]float64 // [min, max] PDM exponent
	FastRecessionRate  [2]float64 // [min, max] 0-1
	SlowRecessionRate  [2]float64 // [min, max] 0-1
	CatchmentAreaKm2   [2]float64 // [min, max] km²
}

// DefaultBounds returns physically reasonable parameter ranges.
func DefaultBounds() ParamBounds {
	return ParamBounds{
		FieldCapacity:     [2]float64{50, 500},
		DrainageRate:      [2]float64{0.001, 0.3},
		ETRate:            [2]float64{0.5, 5.0},
		RunoffShape:       [2]float64{0.1, 10.0},
		FastRecessionRate: [2]float64{0.3, 0.99},
		SlowRecessionRate: [2]float64{0.01, 0.5},
		CatchmentAreaKm2:  [2]float64{100, 500},
	}
}

// CalibrationResult holds the outcome of a calibration run.
type CalibrationResult struct {
	Params      map[string][]float64
	NSE         float64
	RMSE        float64
	PeakBias    float64
	VolumeError float64
}

// SampleParams draws a random parameter set within the given bounds.
func SampleParams(bounds ParamBounds, rng *rand.Rand) map[string][]float64 {
	sample := func(b [2]float64) float64 {
		return b[0] + rng.Float64()*(b[1]-b[0])
	}
	return map[string][]float64{
		"field_capacity":      {sample(bounds.FieldCapacity)},
		"drainage_rate":       {sample(bounds.DrainageRate)},
		"et_rate":             {sample(bounds.ETRate)},
		"runoff_shape":        {sample(bounds.RunoffShape)},
		"fast_recession_rate": {sample(bounds.FastRecessionRate)},
		"slow_recession_rate": {sample(bounds.SlowRecessionRate)},
		"catchment_area_km2":  {sample(bounds.CatchmentAreaKm2)},
		"upstream_partition":  {0},
	}
}

// RunModel runs the rainfall-runoff model with the given parameters and
// observed rainfall data. It returns the simulated flow time series.
// The rainfallData must be in FromStorageIteration format ([][]float64).
func RunModel(rainfallData [][]float64, params map[string][]float64, nSteps int) []float64 {
	settings := &simulator.Settings{
		Iterations: []simulator.IterationSettings{
			{
				Name:              "rainfall",
				Params:            simulator.Params{Map: map[string][]float64{}},
				InitStateValues:   rainfallData[0],
				StateWidth:        1,
				StateHistoryDepth: 2,
			},
			{
				Name:              "rainfall_runoff",
				Params:            simulator.Params{Map: params},
				InitStateValues:   []float64{100.0, 0.0, 0.0, 0.0},
				StateWidth:        4,
				StateHistoryDepth: 2,
			},
		},
		InitTimeValue:         0.0,
		TimestepsHistoryDepth: 2,
	}
	settings.Init()

	rainfallIter := &general.FromStorageIteration{Data: rainfallData}
	runoffIter := &RainfallRunoffIteration{}

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

	simStates := store.GetValues("rainfall_runoff")
	simFlow := make([]float64, len(simStates))
	for i, row := range simStates {
		simFlow[i] = row[1] // total_flow
	}
	return simFlow
}

// Calibrate runs a random-search calibration, sampling nTrials parameter
// sets from the given bounds and evaluating each against observed flow.
// Returns the best result found. The spinUp parameter controls how many
// initial days to skip when computing metrics.
func Calibrate(
	rainfallData [][]float64,
	observedFlow []float64,
	bounds ParamBounds,
	nTrials int,
	spinUp int,
	rng *rand.Rand,
) CalibrationResult {
	nSteps := len(rainfallData) - 1
	var best CalibrationResult
	best.NSE = -1e10

	for trial := 0; trial < nTrials; trial++ {
		params := SampleParams(bounds, rng)
		simFlow := RunModel(rainfallData, params, nSteps)

		// Align: simFlow starts at step 1, observedFlow starts at step 0.
		// So observedFlow[1:] aligns with simFlow[0:].
		obs := observedFlow[1+spinUp:]
		sim := simFlow[spinUp:]
		n := len(obs)
		if len(sim) < n {
			n = len(sim)
		}
		obs = obs[:n]
		sim = sim[:n]

		nse := hydrology.NashSutcliffe(obs, sim)
		if nse > best.NSE {
			best = CalibrationResult{
				Params:      params,
				NSE:         nse,
				RMSE:        hydrology.RMSE(obs, sim),
				PeakBias:    hydrology.PeakFlowBias(obs, sim),
				VolumeError: hydrology.VolumeError(obs, sim),
			}
		}
	}
	return best
}
