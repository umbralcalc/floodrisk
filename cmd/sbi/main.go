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
	multi := flag.Bool("multi", false, "use multi-sub-catchment model")
	flag.Parse()

	if *multi {
		runMultiSBI(*dataDir, *obsVariance, *windowDepth, *calibTrials, *seed)
	} else {
		runSingleSBI(*dataDir, *obsVariance, *windowDepth, *calibTrials, *seed)
	}
}

func runSingleSBI(dataDir string, obsVariance float64, windowDepth, calibTrials int, seed uint64) {
	rainfallSeries, err := hydrology.LoadAllRainfallSeries(dataDir)
	if err != nil {
		log.Fatalf("failed to load rainfall: %v", err)
	}
	avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

	ellandFlow, err := hydrology.LoadTimeSeries(
		dataDir+"/flow/elland_daily_flow.csv", "Elland")
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

	rng := rand.New(rand.NewPCG(seed, 99))
	fmt.Printf("Running %d calibration trials for prior center...\n", calibTrials)
	calibResult := catchment.Calibrate(
		rainfallData, flowAligned, catchment.DefaultBounds(), calibTrials, 30, rng)
	fmt.Printf("Calibration NSE: %.4f\n", calibResult.NSE)

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

	cfg := catchment.DefaultSBIConfig(calibResult)
	cfg.ObsVariance = obsVariance
	cfg.WindowDepth = windowDepth

	fmt.Printf("Running SBI (window=%d days) over %d steps...\n",
		cfg.WindowDepth, nSteps)

	storage = catchment.BuildSBI(storage, cfg)
	printPosterior(storage)
}

func runMultiSBI(dataDir string, obsVariance float64, windowDepth, calibTrials int, seed uint64) {
	// Load station metadata and rainfall series.
	stations, err := hydrology.LoadRainfallStationMeta(dataDir + "/rainfall/stations.csv")
	if err != nil {
		log.Fatalf("failed to load station metadata: %v", err)
	}
	rainfallSeries, err := hydrology.LoadAllRainfallSeries(dataDir)
	if err != nil {
		log.Fatalf("failed to load rainfall: %v", err)
	}

	// Assign to active sub-catchments.
	allSubs := hydrology.UpperCalderSubCatchments()
	var activeSubs []hydrology.SubCatchment
	for _, sc := range allSubs {
		if sc.AreaKm2 > 0 {
			activeSubs = append(activeSubs, sc)
		}
	}
	assignment := hydrology.AssignRainfallStations(stations, activeSubs)
	scRainfall, err := hydrology.SubCatchmentRainfall(rainfallSeries, assignment)
	if err != nil {
		log.Fatalf("failed to compute sub-catchment rainfall: %v", err)
	}

	ellandFlow, err := hydrology.LoadTimeSeries(
		dataDir+"/flow/elland_daily_flow.csv", "Elland")
	if err != nil {
		log.Fatalf("failed to load Elland flow: %v", err)
	}

	flowMap := map[string]*hydrology.TimeSeries{"elland": ellandFlow}
	aligned, err := hydrology.AlignMultiCatchment(scRainfall, flowMap)
	if err != nil {
		log.Fatalf("alignment failed: %v", err)
	}
	fmt.Printf("Aligned %d days across %d sub-catchments\n", aligned.NDays, len(aligned.Rainfall))

	// Build rainfall storage data per sub-catchment.
	rainfallData := make(map[string][][]float64, len(aligned.Rainfall))
	for name, vals := range aligned.Rainfall {
		data := make([][]float64, len(vals))
		for i, v := range vals {
			data[i] = []float64{v}
		}
		rainfallData[name] = data
	}

	// Build sub-catchment config.
	allAreas := hydrology.SubCatchmentAreas()
	var scNames []string
	scAreas := make(map[string]float64)
	for name := range rainfallData {
		scNames = append(scNames, name)
		scAreas[name] = allAreas[name]
	}

	// Run multi-catchment calibration to get prior center.
	rng := rand.New(rand.NewPCG(seed, 99))
	fmt.Printf("Running %d multi-catchment calibration trials for prior center...\n", calibTrials)
	calibCfg := catchment.MultiCatchmentConfig{
		SubCatchments:  scNames,
		CatchmentAreas: scAreas,
	}
	calibResult := catchment.CalibrateMultiCatchment(
		rainfallData, aligned.Flow["elland"], calibCfg,
		catchment.DefaultBounds(), calibTrials, 30, rng)
	fmt.Printf("Calibration NSE: %.4f\n", calibResult.NSE)

	routingCoeffs := calibResult.Params["routing_coefficients"]

	// Create storage: one rainfall partition per sub-catchment + flow.
	nSteps := aligned.NDays - 1
	flowData := make([][]float64, len(aligned.Flow["elland"]))
	for i, v := range aligned.Flow["elland"] {
		flowData[i] = []float64{v}
	}

	dataPartitions := make([]*simulator.PartitionConfig, 0, len(scNames)+1)
	for _, name := range scNames {
		data := rainfallData[name]
		dataPartitions = append(dataPartitions, &simulator.PartitionConfig{
			Name:              name + "_rainfall",
			Iteration:         &general.FromStorageIteration{Data: data},
			Params:            simulator.NewParams(map[string][]float64{}),
			InitStateValues:   data[0],
			StateHistoryDepth: 1,
		})
	}
	dataPartitions = append(dataPartitions, &simulator.PartitionConfig{
		Name:              "flow_data",
		Iteration:         &general.FromStorageIteration{Data: flowData},
		Params:            simulator.NewParams(map[string][]float64{}),
		InitStateValues:   flowData[0],
		StateHistoryDepth: 1,
	})

	storage := analysis.NewStateTimeStorageFromPartitions(
		dataPartitions,
		&simulator.NumberOfStepsTerminationCondition{MaxNumberOfSteps: nSteps},
		&simulator.ConstantTimestepFunction{Stepsize: 1.0},
		0.0,
	)

	// Build SBI config from calibration result.
	sbiCfg := catchment.DefaultSBIConfig(calibResult)
	sbiCfg.ObsVariance = obsVariance
	sbiCfg.WindowDepth = windowDepth

	multiCfg := catchment.MultiSBIConfig{
		SBIConfig:      sbiCfg,
		SubCatchments:  scNames,
		CatchmentAreas: scAreas,
		RoutingCoeffs:  routingCoeffs,
	}

	fmt.Printf("Running multi-catchment SBI (window=%d days) over %d steps...\n",
		windowDepth, nSteps)

	storage = catchment.BuildMultiCatchmentSBI(storage, multiCfg)
	printPosterior(storage)
}

func printPosterior(storage *simulator.StateTimeStorage) {
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
