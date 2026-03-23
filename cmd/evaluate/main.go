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
	nMembers := flag.Int("members", 50, "ensemble members per scenario")
	nSteps := flag.Int("steps", 3650, "simulation length in days (default 10 years)")
	spinUp := flag.Int("spinup", 30, "spin-up days to discard")
	calTrials := flag.Int("cal-trials", 2000, "calibration trials to find baseline params")
	seed := flag.Uint64("seed", 42, "RNG seed")
	flag.Parse()

	// Step 1: Calibrate to get baseline parameters.
	fmt.Println("=== Step 1: Calibrating baseline model ===")
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
	fmt.Printf("  Aligned %d days of observed data\n", nDays)

	rng := rand.New(rand.NewPCG(*seed, 99))
	rainfallData := hydrology.ToStorageData(rainAligned)
	calResult := catchment.Calibrate(
		rainfallData, flowAligned, catchment.DefaultBounds(),
		*calTrials, *spinUp, rng,
	)
	fmt.Printf("  Calibration NSE: %.4f\n", calResult.NSE)
	fmt.Printf("  Parameters: FC=%.0f DR=%.4f ET=%.2f RS=%.2f FR=%.2f SR=%.2f A=%.0f\n",
		calResult.Params["field_capacity"][0],
		calResult.Params["drainage_rate"][0],
		calResult.Params["et_rate"][0],
		calResult.Params["runoff_shape"][0],
		calResult.Params["fast_recession_rate"][0],
		calResult.Params["slow_recession_rate"][0],
		calResult.Params["catchment_area_km2"][0],
	)

	// Step 2: Fit stochastic rainfall generator.
	fmt.Println("\n=== Step 2: Fitting stochastic rainfall generator ===")
	wetThreshold := 0.1
	shape, scale := catchment.FitGammaParams(rainAligned, wetThreshold)
	p01, p11 := catchment.FitWetDryTransitions(rainAligned, wetThreshold)
	fmt.Printf("  Gamma: shape=%.2f scale=%.2f\n", shape, scale)
	fmt.Printf("  Markov: P(wet|dry)=%.2f P(wet|wet)=%.2f\n", p01, p11)

	rainfallParams := catchment.RainfallParams{
		WetDayShape:        shape,
		WetDayScale:        scale,
		PWetGivenDry:       p01,
		PWetGivenWet:       p11,
		RainfallMultiplier: 1.0,
		WetThreshold:       wetThreshold,
	}

	// Step 3: Define sub-catchments for routing effects.
	subs := hydrology.UpperCalderSubCatchments()
	var subNames []string
	for _, sc := range subs {
		if sc.AreaKm2 > 0 {
			subNames = append(subNames, sc.Name)
		}
	}
	// Default routing coefficients (from typical calibration).
	routingCoeffs := make([]float64, len(subNames))
	for i := range routingCoeffs {
		routingCoeffs[i] = 0.8
	}

	// Step 4: Run policy evaluation.
	fmt.Println("\n=== Step 3: Policy Evaluation ===")
	fmt.Printf("  %d ensemble members × %d-day simulations\n", *nMembers, *nSteps)

	cfg := catchment.PolicyEvaluationConfig{
		RunoffParams:   calResult.Params,
		RainfallParams: rainfallParams,
		RoutingCoeffs:  routingCoeffs,
		SubCatchments:  subNames,
		NSteps:         *nSteps,
		NMembers:       *nMembers,
		SpinUp:         *spinUp,
		BaseSeed:       *seed,
		Priors:         catchment.DefaultInterventionPriors(),
	}

	portfolios := catchment.CandidatePortfolios()
	scenarios := catchment.StandardClimateScenarios()

	results := catchment.EvaluatePolicy(portfolios, scenarios, cfg)

	// Print results table.
	fmt.Println("\n=== POLICY EVALUATION RESULTS ===")
	fmt.Printf("\n%-25s %-15s %10s %10s %10s %10s\n",
		"Portfolio", "Scenario", "MeanPeak", "StdPeak", "P95Peak", "MaxPeak")
	fmt.Println("------------------------------------------------------------------------------------")

	for _, r := range results {
		fmt.Printf("%-25s %-15s %10.2f %10.2f %10.2f %10.2f\n",
			r.PortfolioName, r.ScenarioName,
			r.Summary.MeanPeakFlow, r.Summary.StdPeakFlow,
			r.Summary.P95PeakFlow, r.Summary.MaxPeakFlow)
	}

	// Print summary: peak flow reduction by portfolio relative to no-intervention.
	fmt.Println("\n=== PEAK FLOW REDUCTION vs NO INTERVENTION ===")
	fmt.Printf("\n%-25s", "Portfolio")
	for _, s := range scenarios {
		fmt.Printf(" %12s", s.Name)
	}
	fmt.Println()
	fmt.Print("---")
	for range scenarios {
		fmt.Print("-------------")
	}
	fmt.Println()

	// Build lookup for no-intervention baseline peaks.
	baselinePeaks := make(map[string]float64)
	for _, r := range results {
		if r.PortfolioName == "no_intervention" {
			baselinePeaks[r.ScenarioName] = r.Summary.MeanPeakFlow
		}
	}

	for _, p := range portfolios {
		if p.Name == "no_intervention" {
			continue
		}
		fmt.Printf("%-25s", p.Name)
		for _, s := range scenarios {
			for _, r := range results {
				if r.PortfolioName == p.Name && r.ScenarioName == s.Name {
					baseline := baselinePeaks[s.Name]
					if baseline > 0 {
						reduction := (baseline - r.Summary.MeanPeakFlow) / baseline * 100
						fmt.Printf(" %+11.1f%%", reduction)
					}
				}
			}
		}
		fmt.Println()
	}

	fmt.Printf("\n  Cost summary:\n")
	for _, p := range portfolios {
		if p.CostGBP > 0 {
			fmt.Printf("    %-25s £%.0f\n", p.Name, p.CostGBP)
		}
	}
}
