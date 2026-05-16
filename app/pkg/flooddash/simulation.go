package flooddash

import (
	"github.com/umbralcalc/floodrisk/pkg/catchment"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// CalibratedRunoffParams is the calibrated PDM rainfall-runoff
// parameter set for the Upper Calder Valley at Elland. Values come
// from the same calibration the offline policy evaluation uses
// (200 trials over the EA-observed time series). Refresh by running:
//
//	go run ./cmd/evaluate -cal-trials 200
//
// and copying the "Parameters" line from the output.
//
// Order matches catchment.RainfallRunoffIteration's named params.
func CalibratedRunoffParams() map[string][]float64 {
	return map[string][]float64{
		"field_capacity":      {360.0},
		"drainage_rate":       {0.0138},
		"et_rate":             {1.64},
		"runoff_shape":        {2.69},
		"fast_recession_rate": {0.52},
		"slow_recession_rate": {0.44},
		"catchment_area_km2":  {279.0},
		// upstream_partition tells RainfallRunoffIteration.Configure
		// which partition (in the inner RunStochasticModel coordinator)
		// supplies the daily rainfall input. The inner coordinator
		// always sets up [stochastic_rainfall, rainfall_runoff] in that
		// order, so upstream_partition is 0. catchment.SampleParams
		// adds this entry to its sampled params automatically; the
		// offline calibrate's printf doesn't surface it but it is
		// part of CalibrationResult.Params.
		"upstream_partition": {0},
	}
}

// FittedRainfallParams is the rainfall generator's parameters fitted
// from EA rainfall observations at the same time as the calibration
// above. Refresh alongside CalibratedRunoffParams.
//
// RainfallMultiplier is left at 1.0 (baseline); the dashboard's
// scenario selector overrides this per ensemble member.
func FittedRainfallParams() catchment.RainfallParams {
	return catchment.RainfallParams{
		WetDayShape:        0.61,
		WetDayScale:        7.94,
		PWetGivenDry:       0.40,
		PWetGivenWet:       0.83,
		RainfallMultiplier: 1.0,
		WetThreshold:       0.1,
	}
}

// DefaultSubCatchments returns the four-name sub-catchment list the
// portfolio config uses. Matches the calibration in
// pkg/catchment.CandidatePortfolios and the offline evaluation.
func DefaultSubCatchments() []string {
	return []string{"ryburn", "upper_calder", "colne", "holme"}
}

// DefaultRoutingCoeffs returns a uniform 0.8 routing coefficient per
// sub-catchment — the same default the offline evaluation uses when
// the multi-catchment calibration isn't available.
func DefaultRoutingCoeffs() []float64 {
	subs := DefaultSubCatchments()
	out := make([]float64, len(subs))
	for i := range out {
		out[i] = 0.8
	}
	return out
}

// BuildFloodSimulation constructs the stochadex generator for the
// floodrisk dashboard. Eleven partitions, wired in declaration order:
//
//	policy_action            action-state input from the sliders (portfolio + scenario)
//	ensemble_member          one stochastic rainfall-runoff member per outer step
//	peak_stats               running (count, mean, std, max) of member peak flows
//	histogram_bars           rectangleSet for the peak-flow distribution panel
//	histogram_mean_marker    vertical bar at the live mean
//	histogram_ref_marker     vertical bar at the no-intervention reference under
//	                         the current scenario
//	cost_dots                rectangleSet for the four portfolios on the
//	                         cost-effectiveness scatter
//	cost_highlight           larger marker at the user's selected portfolio
//	climate_dots             rectangleSet for the five scenarios on the
//	                         climate-sensitivity scatter
//	climate_highlight        larger marker at the user's selected scenario
//	display_progress         readout: member count + live mean ± std
//	display_cost             readout: cost £ + live peak reduction %
func BuildFloodSimulation() *simulator.ConfigGenerator {
	runoffParams := CalibratedRunoffParams()
	rainfallParams := FittedRainfallParams()
	routingCoeffs := DefaultRoutingCoeffs()
	subCatchments := DefaultSubCatchments()

	policyAction := &simulator.PartitionConfig{
		Name:      "policy_action",
		Iteration: &PolicyActionIteration{},
		Params: simulator.NewParams(map[string][]float64{
			"action_state_values": {
				PortfolioNoIntervention,
				ScenarioBaseline,
			},
		}),
		InitStateValues: []float64{
			PortfolioNoIntervention,
			ScenarioBaseline,
		},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	ensembleMember := &simulator.PartitionConfig{
		Name:      "ensemble_member",
		Iteration: NewEnsembleMemberIteration(runoffParams, rainfallParams, routingCoeffs, subCatchments, 42),
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   []float64{0.0, 0.0, 0.0},
		StateHistoryDepth: 1,
		Seed:              42,
	}

	peakStats := &simulator.PartitionConfig{
		Name:      "peak_stats",
		Iteration: &PeakStatsIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"member_values": {Upstream: "ensemble_member"},
		},
		InitStateValues:   []float64{0.0, 0.0, 0.0, 0.0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	histogramBars := &simulator.PartitionConfig{
		Name:      "histogram_bars",
		Iteration: &HistogramBarsIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"member_values": {Upstream: "ensemble_member"},
		},
		InitStateValues:   make([]float64, HistNBins*4),
		StateHistoryDepth: 1,
		Seed:              0,
	}

	histogramMeanMarker := &simulator.PartitionConfig{
		Name:      "histogram_mean_marker",
		Iteration: &HistogramMeanMarkerIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"stats_values": {Upstream: "peak_stats"},
		},
		InitStateValues:   []float64{0, 0, 0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	histogramRefMarker := &simulator.PartitionConfig{
		Name:      "histogram_ref_marker",
		Iteration: &HistogramReferenceMarkerIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   []float64{0, 0, 0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	costDots := &simulator.PartitionConfig{
		Name:      "cost_dots",
		Iteration: &CostDotsIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   make([]float64, NumPortfolios*4),
		StateHistoryDepth: 1,
		Seed:              0,
	}

	costHighlight := &simulator.PartitionConfig{
		Name:      "cost_highlight",
		Iteration: &CostHighlightIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   []float64{0, 0, 0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	climateDots := &simulator.PartitionConfig{
		Name:      "climate_dots",
		Iteration: &ClimateDotsIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   make([]float64, NumScenarios*4),
		StateHistoryDepth: 1,
		Seed:              0,
	}

	climateHighlight := &simulator.PartitionConfig{
		Name:      "climate_highlight",
		Iteration: &ClimateHighlightIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
		},
		InitStateValues:   []float64{0, 0, 0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	displayProgress := &simulator.PartitionConfig{
		Name:      "display_progress",
		Iteration: &DisplayProgressIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"stats_values": {Upstream: "peak_stats"},
		},
		InitStateValues:   []float64{0, 0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	displayCost := &simulator.PartitionConfig{
		Name:      "display_cost",
		Iteration: &DisplayCostIteration{},
		Params:    simulator.NewParams(map[string][]float64{}),
		ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
			"policy_action": {Upstream: "policy_action"},
			"stats_values":  {Upstream: "peak_stats"},
		},
		InitStateValues:   []float64{0, 0},
		StateHistoryDepth: 1,
		Seed:              0,
	}

	gen := simulator.NewConfigGenerator()
	for _, p := range []*simulator.PartitionConfig{
		policyAction,
		ensembleMember,
		peakStats,
		histogramBars,
		histogramMeanMarker,
		histogramRefMarker,
		costDots,
		costHighlight,
		climateDots,
		climateHighlight,
		displayProgress,
		displayCost,
	} {
		gen.SetPartition(p)
	}

	gen.SetSimulation(&simulator.SimulationConfig{
		OutputCondition: &simulator.EveryStepOutputCondition{},
		TerminationCondition: &simulator.NumberOfStepsTerminationCondition{
			MaxNumberOfSteps: SimMembers,
		},
		TimestepFunction: &simulator.ConstantTimestepFunction{Stepsize: 1.0},
		InitTimeValue:    0.0,
	})
	return gen
}
