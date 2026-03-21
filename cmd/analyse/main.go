package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

func main() {
	dataDir := flag.String("data", "./dat", "data directory (output of ingest command)")
	flag.Parse()

	cfg := hydrology.UpperCalderValley()

	flowSeries, err := hydrology.LoadAllFlowSeries(*dataDir, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR loading flow data: %v\n", err)
		os.Exit(1)
	}

	rainfallSeries, err := hydrology.LoadAllRainfallSeries(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARN: could not load rainfall: %v\n", err)
	}

	hydrology.PrintAnalysisReport(flowSeries, rainfallSeries)
}
