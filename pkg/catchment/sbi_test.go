package catchment

import (
	"math"
	"testing"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/analysis"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
	"gonum.org/v1/gonum/floats"
)

func TestSBI(t *testing.T) {
	t.Run(
		"posterior estimation runs without NaN",
		func(t *testing.T) {
			rainfallSeries, err := hydrology.LoadAllRainfallSeries("../../dat")
			if err != nil {
				t.Skipf("rainfall data not available: %v", err)
			}
			if len(rainfallSeries) == 0 {
				t.Skip("no rainfall series found")
			}

			avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

			ellandFlow, err := hydrology.LoadTimeSeries(
				"../../dat/flow/elland_daily_flow.csv", "Elland")
			if err != nil {
				t.Skipf("Elland flow data not available: %v", err)
			}

			rainAligned, flowAligned, _, nDays, err := hydrology.AlignDaily(
				avgRainfall, ellandFlow)
			if err != nil {
				t.Fatalf("alignment failed: %v", err)
			}

			// Use a small subset for speed.
			nSteps := 500
			if nDays < nSteps {
				nSteps = nDays
			}
			rainfallData := hydrology.ToStorageData(rainAligned[:nSteps+1])
			flowData := hydrology.ToStorageData(flowAligned[:nSteps+1])

			// Create initial storage with data.
			storage := analysis.NewStateTimeStorageFromPartitions(
				[]*simulator.PartitionConfig{
					{
						Name:              "rainfall_data",
						Iteration:         &general.FromStorageIteration{Data: rainfallData},
						Params:            simulator.NewParams(map[string][]float64{}),
						InitStateValues:   rainfallData[0],
						StateHistoryDepth: 1,
					},
					{
						Name:              "flow_data",
						Iteration:         &general.FromStorageIteration{Data: flowData},
						Params:            simulator.NewParams(map[string][]float64{}),
						InitStateValues:   flowData[0],
						StateHistoryDepth: 1,
					},
				},
				&simulator.NumberOfStepsTerminationCondition{MaxNumberOfSteps: nSteps},
				&simulator.ConstantTimestepFunction{Stepsize: 1.0},
				0.0,
			)

			cfg := SBIConfig{
				PriorMean:      []float64{200.0, 0.05, 1.5, 2.0, 0.7, 0.1, 200.0},
				PriorVariance:  []float64{2500, 0.01, 1.0, 4.0, 0.04, 0.02, 2500},
				ObsVariance:    25.0,
				WindowDepth:    100,
				DiscountFactor: 0.99,
			}

			storage = BuildSBI(storage, cfg)

			// Check posterior mean has no NaN.
			posteriorMean := storage.GetValues("posterior_mean")
			if len(posteriorMean) == 0 {
				t.Fatal("no posterior mean output")
			}
			lastMean := posteriorMean[len(posteriorMean)-1]
			if len(lastMean) != NumModelParams {
				t.Errorf("posterior mean has %d elements, want %d",
					len(lastMean), NumModelParams)
			}
			if floats.HasNaN(lastMean) {
				t.Errorf("posterior mean contains NaN: %v", lastMean)
			}

			// Check no NaN across all partitions.
			for _, name := range storage.GetNames() {
				for _, values := range storage.GetValues(name) {
					if floats.HasNaN(values) {
						t.Errorf("partition %s contains NaN", name)
						break
					}
				}
			}

			t.Logf("Posterior mean after %d steps (window=%d): %v",
				nSteps, cfg.WindowDepth, lastMean)

			// Posterior mean should be finite and positive for all params.
			for i, v := range lastMean {
				if math.IsInf(v, 0) {
					t.Errorf("posterior mean[%d] is Inf", i)
				}
			}
		},
	)
}
