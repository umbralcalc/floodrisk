package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/catchment"
	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

func main() {
	dataDir := flag.String("data", "./dat", "data directory")
	trials := flag.Int("trials", 5000, "number of random search trials")
	spinUp := flag.Int("spinup", 30, "spin-up days to skip")
	seed1 := flag.Uint64("seed", 42, "RNG seed")
	multi := flag.Bool("multi", false, "use multi-sub-catchment model")
	flag.Parse()

	rng := rand.New(rand.NewPCG(*seed1, 99))

	if *multi {
		runMultiCalibration(*dataDir, *trials, *spinUp, rng)
	} else {
		runSingleCalibration(*dataDir, *trials, *spinUp, rng)
	}
}

func runSingleCalibration(dataDir string, trials, spinUp int, rng *rand.Rand) {
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

	fmt.Printf("Running %d calibration trials...\n", trials)
	result := catchment.Calibrate(
		rainfallData, flowAligned, catchment.DefaultBounds(), trials, spinUp, rng)

	printResult(result)
	fmt.Printf("    catchment_area_km2:  %.1f\n", result.Params["catchment_area_km2"][0])
}

func runMultiCalibration(dataDir string, trials, spinUp int, rng *rand.Rand) {
	// Load rainfall station metadata and all rainfall series.
	stations, err := hydrology.LoadRainfallStationMeta(dataDir + "/rainfall/stations.csv")
	if err != nil {
		log.Fatalf("failed to load station metadata: %v", err)
	}

	rainfallSeries, err := hydrology.LoadAllRainfallSeries(dataDir)
	if err != nil {
		log.Fatalf("failed to load rainfall: %v", err)
	}

	// Assign stations to sub-catchments and compute averages.
	subs := hydrology.UpperCalderSubCatchments()
	assignment := hydrology.AssignRainfallStations(stations, subs)
	scRainfall, err := hydrology.SubCatchmentRainfall(rainfallSeries, assignment)
	if err != nil {
		log.Fatalf("failed to compute sub-catchment rainfall: %v", err)
	}

	// Load Elland flow for evaluation.
	ellandFlow, err := hydrology.LoadTimeSeries(
		dataDir+"/flow/elland_daily_flow.csv", "Elland")
	if err != nil {
		log.Fatalf("failed to load Elland flow: %v", err)
	}

	// Build flow map and align.
	flowMap := map[string]*hydrology.TimeSeries{"elland": ellandFlow}
	aligned, err := hydrology.AlignMultiCatchment(scRainfall, flowMap)
	if err != nil {
		log.Fatalf("alignment failed: %v", err)
	}
	fmt.Printf("Aligned %d days across %d sub-catchments\n", aligned.NDays, len(aligned.Rainfall))

	// Convert to storage data per sub-catchment.
	rainfallData := make(map[string][][]float64, len(aligned.Rainfall))
	for name, vals := range aligned.Rainfall {
		data := make([][]float64, len(vals))
		for i, v := range vals {
			data[i] = []float64{v}
		}
		rainfallData[name] = data
	}

	observedFlow := aligned.Flow["elland"]

	cfg := catchment.MultiCatchmentConfig{
		SubCatchments:  hydrology.SubCatchmentNames(),
		CatchmentAreas: hydrology.SubCatchmentAreas(),
	}

	fmt.Printf("Running %d multi-catchment calibration trials...\n", trials)
	result := catchment.CalibrateMultiCatchment(
		rainfallData, observedFlow, cfg, catchment.DefaultBounds(), trials, spinUp, rng)

	printResult(result)
	if rc, ok := result.Params["routing_coefficients"]; ok {
		fmt.Printf("    routing_coefficients: %v\n", rc)
	}
}

func printResult(result catchment.CalibrationResult) {
	fmt.Println("\n=== Best Calibration Result ===")
	fmt.Printf("  NSE:          %.4f\n", result.NSE)
	fmt.Printf("  RMSE:         %.4f m³/s\n", result.RMSE)
	fmt.Printf("  Peak bias:    %.4f\n", result.PeakBias)
	fmt.Printf("  Volume error: %.4f\n", result.VolumeError)
	fmt.Println("\n  Parameters:")
	fmt.Printf("    field_capacity:      %.1f mm\n", result.Params["field_capacity"][0])
	fmt.Printf("    drainage_rate:       %.4f\n", result.Params["drainage_rate"][0])
	fmt.Printf("    et_rate:             %.2f mm/day\n", result.Params["et_rate"][0])
	fmt.Printf("    runoff_shape:        %.4f\n", result.Params["runoff_shape"][0])
	fmt.Printf("    fast_recession_rate: %.4f\n", result.Params["fast_recession_rate"][0])
	fmt.Printf("    slow_recession_rate: %.4f\n", result.Params["slow_recession_rate"][0])
}
