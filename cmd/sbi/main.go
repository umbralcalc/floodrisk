package main

import (
	"flag"
	"fmt"
	"log"
	"math"
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/catchment"
	"github.com/umbralcalc/floodrisk/pkg/hydrology"
	"github.com/umbralcalc/stochadex/pkg/analysis"
	"github.com/umbralcalc/stochadex/pkg/general"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

func main() {
	dataDir := flag.String("data", "./dat", "data directory")
	obsVariance := flag.Float64("variance", 25.0, "observation noise variance (m³/s)²")
	windowDepth := flag.Int("window", 200, "sliding window depth in days")
	calibTrials := flag.Int("calibrate", 5000, "calibration trials for prior center")
	seed := flag.Uint64("seed", 42, "RNG seed for calibration")
	flag.Parse()

	// Load and align data.
	rainfallSeries, err := hydrology.LoadAllRainfallSeries(*dataDir)
	if err != nil {
		log.Fatalf("failed to load rainfall: %v", err)
	}
	avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

	ellandFlow, err := hydrology.LoadTimeSeries(
		*dataDir+"/flow/elland_daily_flow.csv", "Elland")
	if err != nil {
		log.Fatalf("failed to load Elland flow: %v", err)
	}

	rainAligned, flowAligned, _, nDays, err := hydrology.AlignDaily(
		avgRainfall, ellandFlow)
	if err != nil {
		log.Fatalf("alignment failed: %v", err)
	}
	fmt.Printf("Aligned %d days of data\n", nDays)

	rainfallData := hydrology.ToStorageData(rainAligned)
	flowData := hydrology.ToStorageData(flowAligned)

	// Run calibration to get prior center.
	rng := rand.New(rand.NewPCG(*seed, 99))
	fmt.Printf("Running %d calibration trials for prior center...\n", *calibTrials)
	calibResult := catchment.Calibrate(
		rainfallData, flowAligned, catchment.DefaultBounds(), *calibTrials, 30, rng)
	fmt.Printf("Calibration NSE: %.4f\n", calibResult.NSE)

	// Create storage with data.
	nSteps := len(rainfallData) - 1
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

	// Build and run SBI.
	cfg := catchment.DefaultSBIConfig(calibResult)
	cfg.ObsVariance = *obsVariance
	cfg.WindowDepth = *windowDepth

	fmt.Printf("Running SBI (window=%d days) over %d steps...\n",
		cfg.WindowDepth, nSteps)

	storage = catchment.BuildSBI(storage, cfg)

	// Extract results.
	posteriorMean := storage.GetValues("posterior_mean")
	posteriorCov := storage.GetValues("posterior_covariance")
	if len(posteriorMean) == 0 {
		log.Fatal("no posterior mean output")
	}

	lastMean := posteriorMean[len(posteriorMean)-1]
	lastCov := posteriorCov[len(posteriorCov)-1]

	paramLabels := []string{
		"field_capacity",
		"drainage_rate",
		"et_rate",
		"runoff_shape",
		"fast_recession_rate",
		"slow_recession_rate",
		"catchment_area_km2",
	}

	fmt.Println("\n=== SBI Posterior Results ===")
	fmt.Printf("%-22s %12s %12s\n", "Parameter", "Mean", "Std Dev")
	fmt.Println("----------------------------------------------")
	n := catchment.NumModelParams
	for i, label := range paramLabels {
		std := 0.0
		if i*n+i < len(lastCov) {
			v := lastCov[i*n+i]
			if v > 0 {
				std = math.Sqrt(v)
			}
		}
		fmt.Printf("%-22s %12.4f %12.4f\n", label, lastMean[i], std)
	}
}
