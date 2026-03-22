package catchment

import (
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
