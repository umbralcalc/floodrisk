package catchment

import (
	"fmt"

	"github.com/umbralcalc/stochadex/pkg/analysis"
	"github.com/umbralcalc/stochadex/pkg/inference"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// SBIConfig holds the configuration for building an SBI simulation.
type SBIConfig struct {
	PriorMean      []float64 // 7-element prior center
	PriorVariance  []float64 // 7-element diagonal prior variance
	ObsVariance    float64   // observation noise variance (m³/s)²
	WindowDepth    int       // sliding window size in days
	DiscountFactor float64
}

// DefaultSBIConfig returns an SBI config centered on calibration results.
func DefaultSBIConfig(calibResult CalibrationResult) SBIConfig {
	mean := ModelParamsFromMap(calibResult.Params)
	// Wide diagonal prior: variance = (0.3 * mean)² for each param.
	variance := make([]float64, NumModelParams)
	for i, m := range mean {
		spread := 0.3 * m
		if spread < 1e-6 {
			spread = 1.0
		}
		variance[i] = spread * spread
	}
	return SBIConfig{
		PriorMean:      mean,
		PriorVariance:  variance,
		ObsVariance:    25.0, // (5 m³/s)²
		WindowDepth:    200,
		DiscountFactor: 0.99,
	}
}

// MultiSBIConfig holds configuration for building a multi-catchment SBI.
type MultiSBIConfig struct {
	SBIConfig
	SubCatchments  []string           // ordered sub-catchment names
	CatchmentAreas map[string]float64 // km² per sub-catchment
	RoutingCoeffs  []float64          // fixed routing coefficients from calibration
}

// BuildMultiCatchmentSBI constructs posterior estimation partitions for the
// multi-sub-catchment model. The storage must contain one rainfall data
// partition per sub-catchment (named "<name>_rainfall") plus a "flow_data"
// partition.
func BuildMultiCatchmentSBI(
	storage *simulator.StateTimeStorage,
	cfg MultiSBIConfig,
) *simulator.StateTimeStorage {
	n := len(cfg.SubCatchments)

	// Build diagonal covariance matrix (flattened 7x7).
	covMatrix := make([]float64, NumModelParams*NumModelParams)
	for i := 0; i < NumModelParams; i++ {
		covMatrix[i*NumModelParams+i] = cfg.PriorVariance[i]
	}

	compModelParams := simulator.NewParams(map[string][]float64{
		"variance": {cfg.ObsVariance},
	})

	// Build windowed partitions: N runoff + 1 routing.
	windowPartitions := make([]analysis.WindowedPartition, 0, n+1)

	// One rainfall-runoff partition per sub-catchment.
	for i, name := range cfg.SubCatchments {
		rainfallDataName := name + "_rainfall"
		runoffName := name + "_runoff"
		windowPartitions = append(windowPartitions, analysis.WindowedPartition{
			Partition: &simulator.PartitionConfig{
				Name:      runoffName,
				Iteration: &RainfallRunoffIteration{},
				Params: simulator.NewParams(map[string][]float64{
					"upstream_partition":  {float64(i)},
					"catchment_area_km2": {cfg.CatchmentAreas[name]},
				}),
				ParamsAsPartitions: map[string][]string{
					"upstream_partition": {rainfallDataName},
				},
				InitStateValues:   []float64{100.0, 0.0, 0.0, 0.0},
				StateHistoryDepth: 2,
			},
			OutsideUpstreams: map[string]simulator.NamedUpstreamConfig{
				"model_params": {Upstream: "sampler"},
			},
		})
	}

	// Channel routing partition reads from all runoff partitions.
	upstreamPartitions := make([]float64, n)
	routingParamsAsPartitions := make(map[string][]string)
	for i, name := range cfg.SubCatchments {
		upstreamPartitions[i] = float64(i)
		routingParamsAsPartitions[fmt.Sprintf("upstream_partitions_%d", i)] =
			[]string{name + "_runoff"}
	}
	routingParams := map[string][]float64{
		"upstream_partitions": upstreamPartitions,
	}
	if len(cfg.RoutingCoeffs) > 0 {
		routingParams["routing_coefficients"] = cfg.RoutingCoeffs
	}
	routingStateWidth := 1 + n
	windowPartitions = append(windowPartitions, analysis.WindowedPartition{
		Partition: &simulator.PartitionConfig{
			Name:              "routing",
			Iteration:         &ChannelRoutingIteration{},
			Params:            simulator.NewParams(routingParams),
			InitStateValues:   make([]float64, routingStateWidth),
			StateHistoryDepth: 2,
		},
	})

	// Data refs: one per sub-catchment rainfall + flow.
	dataRefs := make([]analysis.DataRef, 0, n+1)
	for _, name := range cfg.SubCatchments {
		dataRefs = append(dataRefs, analysis.DataRef{
			PartitionName: name + "_rainfall",
		})
	}
	dataRefs = append(dataRefs, analysis.DataRef{PartitionName: "flow_data"})

	partitions := analysis.NewPosteriorEstimationPartitions(
		analysis.AppliedPosteriorEstimation{
			LogNorm: analysis.PosteriorLogNorm{
				Name:    "log_normalisation",
				Default: 0.0,
			},
			Mean: analysis.PosteriorMean{
				Name:    "posterior_mean",
				Default: append([]float64{}, cfg.PriorMean...),
			},
			Covariance: analysis.PosteriorCovariance{
				Name:    "posterior_covariance",
				Default: append([]float64{}, covMatrix...),
			},
			Sampler: analysis.PosteriorSampler{
				Name:    "sampler",
				Default: append([]float64{}, cfg.PriorMean...),
				Distribution: analysis.ParameterisedModel{
					Likelihood: &inference.NormalLikelihoodDistribution{},
					Params: simulator.NewParams(map[string][]float64{
						"default_covariance": append([]float64{}, covMatrix...),
						"cov_burn_in_steps":  {float64(cfg.WindowDepth)},
					}),
					ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
						"mean":              {Upstream: "posterior_mean"},
						"covariance_matrix": {Upstream: "posterior_covariance"},
					},
				},
			},
			Comparison: analysis.AppliedLikelihoodComparison{
				Name: "likelihood",
				Model: analysis.ParameterisedModel{
					Likelihood: &inference.NormalLikelihoodDistribution{},
					Params:     compModelParams,
					ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
						"mean": {Upstream: "routing", Indices: []int{0}},
					},
				},
				Data: analysis.DataRef{PartitionName: "flow_data"},
				Window: analysis.WindowedPartitions{
					Partitions: windowPartitions,
					Data:       dataRefs,
					Depth:      cfg.WindowDepth,
				},
			},
			PastDiscount: cfg.DiscountFactor,
			MemoryDepth:  cfg.WindowDepth,
			Seed:         42,
		},
		storage,
	)

	windowSizes := make(map[string]int, n+1)
	for _, name := range cfg.SubCatchments {
		windowSizes[name+"_rainfall"] = cfg.WindowDepth
	}
	windowSizes["flow_data"] = cfg.WindowDepth

	return analysis.AddPartitionsToStateTimeStorage(
		storage, partitions, windowSizes,
	)
}

// BuildSBI constructs the posterior estimation partitions and runs them
// against existing rainfall and flow data in the provided storage.
// Returns the storage augmented with posterior mean and covariance.
func BuildSBI(
	storage *simulator.StateTimeStorage,
	cfg SBIConfig,
) *simulator.StateTimeStorage {
	// Build diagonal covariance matrix (flattened 7x7).
	covMatrix := make([]float64, NumModelParams*NumModelParams)
	for i := 0; i < NumModelParams; i++ {
		covMatrix[i*NumModelParams+i] = cfg.PriorVariance[i]
	}

	// The comparison model evaluates how well the rainfall-runoff model
	// predicts observed flow. Inside the windowed embedded simulation:
	//   - rainfall_data is replayed via FromHistoryIteration
	//   - rainfall_runoff runs using model_params from the sampler (outside)
	//   - comparison evaluates Normal log-likelihood of model flow vs observed
	compModelParams := simulator.NewParams(map[string][]float64{
		"variance": {cfg.ObsVariance},
	})

	partitions := analysis.NewPosteriorEstimationPartitions(
		analysis.AppliedPosteriorEstimation{
			LogNorm: analysis.PosteriorLogNorm{
				Name:    "log_normalisation",
				Default: 0.0,
			},
			Mean: analysis.PosteriorMean{
				Name:    "posterior_mean",
				Default: append([]float64{}, cfg.PriorMean...),
			},
			Covariance: analysis.PosteriorCovariance{
				Name:    "posterior_covariance",
				Default: append([]float64{}, covMatrix...),
			},
			Sampler: analysis.PosteriorSampler{
				Name:    "sampler",
				Default: append([]float64{}, cfg.PriorMean...),
				Distribution: analysis.ParameterisedModel{
					Likelihood: &inference.NormalLikelihoodDistribution{},
					Params: simulator.NewParams(map[string][]float64{
						"default_covariance": append([]float64{}, covMatrix...),
						"cov_burn_in_steps":  {float64(cfg.WindowDepth)},
					}),
					ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
						"mean":              {Upstream: "posterior_mean"},
						"covariance_matrix": {Upstream: "posterior_covariance"},
					},
				},
			},
			Comparison: analysis.AppliedLikelihoodComparison{
				Name: "likelihood",
				Model: analysis.ParameterisedModel{
					Likelihood: &inference.NormalLikelihoodDistribution{},
					Params:     compModelParams,
					ParamsFromUpstream: map[string]simulator.NamedUpstreamConfig{
						"mean": {Upstream: "rainfall_runoff", Indices: []int{1}},
					},
				},
				Data: analysis.DataRef{PartitionName: "flow_data"},
				Window: analysis.WindowedPartitions{
					Partitions: []analysis.WindowedPartition{
						{
							Partition: &simulator.PartitionConfig{
								Name:      "rainfall_runoff",
								Iteration: &RainfallRunoffIteration{},
								Params: simulator.NewParams(map[string][]float64{
									"upstream_partition": {0},
								}),
								ParamsAsPartitions: map[string][]string{
									"upstream_partition": {"rainfall_data"},
								},
								InitStateValues:   []float64{100.0, 0.0, 0.0, 0.0},
								StateHistoryDepth: 2,
							},
							OutsideUpstreams: map[string]simulator.NamedUpstreamConfig{
								"model_params": {Upstream: "sampler"},
							},
						},
					},
					Data: []analysis.DataRef{
						{PartitionName: "rainfall_data"},
						{PartitionName: "flow_data"},
					},
					Depth: cfg.WindowDepth,
				},
			},
			PastDiscount: cfg.DiscountFactor,
			MemoryDepth:  cfg.WindowDepth,
			Seed:         42,
		},
		storage,
	)

	windowSizes := map[string]int{
		"rainfall_data": cfg.WindowDepth,
		"flow_data":     cfg.WindowDepth,
	}

	return analysis.AddPartitionsToStateTimeStorage(
		storage, partitions, windowSizes,
	)
}
