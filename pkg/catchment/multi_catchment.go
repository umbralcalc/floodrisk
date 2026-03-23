package catchment

import (
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// MultiCatchmentConfig describes the layout of a multi-sub-catchment model.
type MultiCatchmentConfig struct {
	SubCatchments  []string           // ordered sub-catchment names
	CatchmentAreas map[string]float64 // km² per sub-catchment
	RoutingCoeffs  []float64          // one per sub-catchment (optional; defaults to 1.0)
}

// RunMultiCatchmentModel runs the multi-sub-catchment rainfall-runoff model
// with channel routing. rainfallData maps sub-catchment name to
// FromStorageIteration-format data ([][]float64). params holds the shared
// PDM model parameters (area is overridden per sub-catchment). Returns the
// total routed flow time series at the downstream gauge.
func RunMultiCatchmentModel(
	rainfallData map[string][][]float64,
	params map[string][]float64,
	cfg MultiCatchmentConfig,
	nSteps int,
) []float64 {
	n := len(cfg.SubCatchments)

	// Partition layout:
	//   0..n-1:   rainfall data (FromStorageIteration)
	//   n..2n-1:  rainfall-runoff (RainfallRunoffIteration)
	//   2n:       channel routing (ChannelRoutingIteration)
	totalPartitions := 2*n + 1

	iterSettings := make([]simulator.IterationSettings, totalPartitions)
	iterations := make([]simulator.Iteration, totalPartitions)

	// Rainfall data partitions.
	for i, name := range cfg.SubCatchments {
		data := rainfallData[name]
		iterSettings[i] = simulator.IterationSettings{
			Name:              name + "_rainfall",
			Params:            simulator.Params{Map: map[string][]float64{}},
			InitStateValues:   data[0],
			StateWidth:        1,
			StateHistoryDepth: 2,
		}
		iterations[i] = &general.FromStorageIteration{Data: data}
	}

	// Rainfall-runoff partitions.
	upstreamPartitions := make([]float64, n)
	for i, name := range cfg.SubCatchments {
		runoffParams := make(map[string][]float64, len(params))
		for k, v := range params {
			runoffParams[k] = v
		}
		// Override area for this sub-catchment.
		runoffParams["catchment_area_km2"] = []float64{cfg.CatchmentAreas[name]}
		// Point to this sub-catchment's rainfall partition.
		runoffParams["upstream_partition"] = []float64{float64(i)}

		iterSettings[n+i] = simulator.IterationSettings{
			Name:              name + "_runoff",
			Params:            simulator.Params{Map: runoffParams},
			InitStateValues:   []float64{100.0, 0.0, 0.0, 0.0},
			StateWidth:        4,
			StateHistoryDepth: 2,
		}
		iterations[n+i] = &RainfallRunoffIteration{}
		upstreamPartitions[i] = float64(n + i)
	}

	// Channel routing partition.
	routingParams := map[string][]float64{
		"upstream_partitions": upstreamPartitions,
	}
	if len(cfg.RoutingCoeffs) > 0 {
		routingParams["routing_coefficients"] = cfg.RoutingCoeffs
	}
	routingStateWidth := 1 + n
	routingInit := make([]float64, routingStateWidth)
	iterSettings[2*n] = simulator.IterationSettings{
		Name:              "routing",
		Params:            simulator.Params{Map: routingParams},
		InitStateValues:   routingInit,
		StateWidth:        routingStateWidth,
		StateHistoryDepth: 2,
	}
	iterations[2*n] = &ChannelRoutingIteration{}

	settings := &simulator.Settings{
		Iterations:            iterSettings,
		InitTimeValue:         0.0,
		TimestepsHistoryDepth: 2,
	}
	settings.Init()

	// Configure all iterations before creating the coordinator.
	for i, iter := range iterations {
		iter.Configure(i, settings)
	}

	store := simulator.NewStateTimeStorage()
	implementations := &simulator.Implementations{
		Iterations:      iterations,
		OutputCondition: &simulator.EveryStepOutputCondition{},
		OutputFunction:  &simulator.StateTimeStorageOutputFunction{Store: store},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: nSteps,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
	}
	coordinator := simulator.NewPartitionCoordinator(settings, implementations)
	coordinator.Run()

	simStates := store.GetValues("routing")
	simFlow := make([]float64, len(simStates))
	for i, row := range simStates {
		simFlow[i] = row[0] // total_routed_flow
	}
	return simFlow
}

// CalibrateMultiCatchment runs a random-search calibration over shared PDM
// parameters (areas fixed from EA metadata) plus optional routing coefficients.
// Evaluates against observed flow at the downstream gauge.
func CalibrateMultiCatchment(
	rainfallData map[string][][]float64,
	observedFlow []float64,
	cfg MultiCatchmentConfig,
	bounds ParamBounds,
	nTrials int,
	spinUp int,
	rng *rand.Rand,
) CalibrationResult {
	// Find shortest rainfall series to determine nSteps.
	nSteps := -1
	for _, data := range rainfallData {
		if nSteps < 0 || len(data)-1 < nSteps {
			nSteps = len(data) - 1
		}
	}

	var best CalibrationResult
	best.NSE = -1e10

	n := len(cfg.SubCatchments)

	for trial := 0; trial < nTrials; trial++ {
		params := SampleParams(bounds, rng)
		// Remove catchment_area_km2 — it's fixed per sub-catchment from EA data.
		delete(params, "catchment_area_km2")
		delete(params, "upstream_partition")

		// Optionally sample routing coefficients.
		routingCoeffs := make([]float64, n)
		for i := range routingCoeffs {
			routingCoeffs[i] = 0.3 + rng.Float64()*0.7 // [0.3, 1.0]
		}
		trialCfg := MultiCatchmentConfig{
			SubCatchments:  cfg.SubCatchments,
			CatchmentAreas: cfg.CatchmentAreas,
			RoutingCoeffs:  routingCoeffs,
		}

		simFlow := RunMultiCatchmentModel(rainfallData, params, trialCfg, nSteps)

		obs := observedFlow[1+spinUp:]
		sim := simFlow[spinUp:]
		length := len(obs)
		if len(sim) < length {
			length = len(sim)
		}
		obs = obs[:length]
		sim = sim[:length]

		nse := hydrology.NashSutcliffe(obs, sim)
		if nse > best.NSE {
			params["routing_coefficients"] = routingCoeffs
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
