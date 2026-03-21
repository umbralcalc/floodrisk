package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

func main() {
	dataDir := flag.String("data", "./dat", "output directory for downloaded data")
	minDate := flag.String("from", "2010-01-01", "start date (YYYY-MM-DD)")
	maxDate := flag.String("to", "2025-12-31", "end date (YYYY-MM-DD)")
	subdaily := flag.Bool("subdaily", false, "also fetch sub-daily (15-min) flow data")
	rainfall := flag.Bool("rainfall", true, "fetch rainfall station data")
	flag.Parse()

	cfg := hydrology.UpperCalderValley()
	ing := hydrology.NewIngester(*dataDir)

	fmt.Printf("=== Ingesting data for %s ===\n", cfg.Name)
	fmt.Printf("Date range: %s to %s\n", *minDate, *maxDate)
	fmt.Printf("Output directory: %s\n\n", *dataDir)

	// Fetch daily flow data for each station.
	fmt.Println("--- Flow stations ---")
	for _, station := range cfg.FlowStations {
		fmt.Printf("Fetching daily flow: %s (%s)... ", station.Label, station.River)
		path, err := ing.IngestFlowData(station, *minDate, *maxDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
			continue
		}
		fmt.Printf("OK → %s\n", path)

		if *subdaily {
			fmt.Printf("Fetching sub-daily flow: %s... ", station.Label)
			path, err := ing.IngestSubDailyFlowData(station, *minDate, *maxDate)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
				continue
			}
			fmt.Printf("OK → %s\n", path)
		}
	}

	// Fetch rainfall data.
	if *rainfall {
		fmt.Println("\n--- Rainfall stations ---")
		fmt.Printf("Searching within %.0f km of catchment centre...\n", cfg.RainfallRadiusKm)
		paths, err := ing.IngestRainfallStations(cfg, *minDate, *maxDate)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR fetching rainfall: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Downloaded %d rainfall station files\n", len(paths))
		for _, p := range paths {
			fmt.Printf("  %s\n", p)
		}
	}

	// Fetch flood area data.
	fmt.Println("\n--- Flood areas ---")
	areas, err := hydrology.FetchFloodAreas(cfg.CentreLat, cfg.CentreLong, cfg.SearchRadiusKm)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR fetching flood areas: %v\n", err)
	} else {
		path, err := hydrology.WriteFloodAreasCSV(areas, *dataDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR writing flood areas: %v\n", err)
		} else {
			fmt.Printf("Downloaded %d flood areas → %s\n", len(areas), path)
		}
	}

	fmt.Println("\nDone.")
}
