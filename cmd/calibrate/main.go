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

	// Run calibration.
	rng := rand.New(rand.NewPCG(*seed1, 99))
	fmt.Printf("Running %d calibration trials...\n", *trials)
	result := catchment.Calibrate(
		rainfallData, flowAligned, catchment.DefaultBounds(), *trials, *spinUp, rng)

	// Report results.
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
	fmt.Printf("    catchment_area_km2:  %.1f\n", result.Params["catchment_area_km2"][0])
}
