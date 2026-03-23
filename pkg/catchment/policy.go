package catchment

import (
	"math"
	"math/rand/v2"
)

// ClimateScenario defines a rainfall perturbation representing a
// UKCP18 climate projection at a specific time horizon.
type ClimateScenario struct {
	Name               string
	RainfallMultiplier float64 // e.g. 1.0 = baseline, 1.2 = +20%
}

// StandardClimateScenarios returns the set of scenarios to evaluate.
// Based on UKCP18 projections for winter rainfall intensity in northern
// England: RCP4.5 and RCP8.5 at 2040 and 2070 horizons.
func StandardClimateScenarios() []ClimateScenario {
	return []ClimateScenario{
		{Name: "baseline", RainfallMultiplier: 1.0},
		{Name: "RCP4.5_2040", RainfallMultiplier: 1.10}, // +10% winter rainfall
		{Name: "RCP4.5_2070", RainfallMultiplier: 1.20}, // +20%
		{Name: "RCP8.5_2040", RainfallMultiplier: 1.15}, // +15%
		{Name: "RCP8.5_2070", RainfallMultiplier: 1.35}, // +35%
	}
}

// CandidatePortfolios returns example intervention portfolios for the
// Upper Calder Valley at different budget levels. Sub-catchment names
// match those from UpperCalderSubCatchments().
func CandidatePortfolios() []Portfolio {
	return []Portfolio{
		{
			Name:    "no_intervention",
			CostGBP: 0,
		},
		{
			Name:    "leaky_dams_only",
			CostGBP: 500_000,
			Interventions: []Intervention{
				{Type: LeakyDams, SubCatchment: "Ryburn", Scale: 10},
				{Type: LeakyDams, SubCatchment: "Upper Calder", Scale: 15},
				{Type: LeakyDams, SubCatchment: "Colne", Scale: 15},
			},
		},
		{
			Name:    "woodland_focus",
			CostGBP: 1_000_000,
			Interventions: []Intervention{
				{Type: WoodlandPlanting, SubCatchment: "Ryburn", Scale: 30},       // 30 ha
				{Type: WoodlandPlanting, SubCatchment: "Upper Calder", Scale: 50}, // 50 ha
				{Type: WoodlandPlanting, SubCatchment: "Colne", Scale: 40},        // 40 ha
			},
		},
		{
			Name:    "mixed_portfolio",
			CostGBP: 2_000_000,
			Interventions: []Intervention{
				{Type: LeakyDams, SubCatchment: "Ryburn", Scale: 10},
				{Type: LeakyDams, SubCatchment: "Upper Calder", Scale: 10},
				{Type: WoodlandPlanting, SubCatchment: "Ryburn", Scale: 20},
				{Type: WoodlandPlanting, SubCatchment: "Colne", Scale: 30},
				{Type: FloodplainReconnection, SubCatchment: "Colne", Scale: 2},
				{Type: PeatRestoration, SubCatchment: "Upper Calder", Scale: 30},
			},
		},
	}
}

// PolicyResult holds the ensemble outcome for one portfolio under one
// climate scenario.
type PolicyResult struct {
	PortfolioName string
	ScenarioName  string
	Summary       EnsembleSummary
}

// PolicyEvaluationConfig holds the settings for a policy evaluation run.
type PolicyEvaluationConfig struct {
	RunoffParams   map[string][]float64 // calibrated PDM params
	RainfallParams RainfallParams       // fitted rainfall generator params
	RoutingCoeffs  []float64            // calibrated routing coefficients
	SubCatchments  []string             // ordered sub-catchment names
	NSteps         int                  // simulation length (days)
	NMembers       int                  // ensemble size per scenario
	SpinUp         int                  // days to discard
	BaseSeed       uint64
	Priors         InterventionPriors
}

// EvaluatePolicy runs ensemble simulations for every combination of
// portfolio and climate scenario. Returns one PolicyResult per combo.
func EvaluatePolicy(
	portfolios []Portfolio,
	scenarios []ClimateScenario,
	cfg PolicyEvaluationConfig,
) []PolicyResult {
	var results []PolicyResult

	for _, portfolio := range portfolios {
		for _, scenario := range scenarios {
			rng := rand.New(rand.NewPCG(cfg.BaseSeed, cfg.BaseSeed+1))

			// Apply interventions (re-sampled per member inside RunEnsembleWithInterventions).
			rainfallParams := cfg.RainfallParams
			rainfallParams.RainfallMultiplier = scenario.RainfallMultiplier

			summary := RunEnsembleWithInterventions(
				cfg.RunoffParams,
				rainfallParams,
				cfg.RoutingCoeffs,
				cfg.SubCatchments,
				portfolio,
				cfg.Priors,
				cfg.NSteps,
				cfg.NMembers,
				cfg.SpinUp,
				cfg.BaseSeed,
				rng,
			)

			results = append(results, PolicyResult{
				PortfolioName: portfolio.Name,
				ScenarioName:  scenario.Name,
				Summary:       summary,
			})
		}
	}
	return results
}

// RunEnsembleWithInterventions runs an ensemble of single-catchment
// stochastic rainfall-runoff simulations with NFM interventions applied.
// Each member samples intervention effectiveness independently to
// propagate uncertainty.
//
// Note: this uses the single-catchment model (not multi-catchment routing)
// because intervention effects on shared PDM params are averaged across
// sub-catchments. The routing coefficient effects are summarised as an
// aggregate attenuation factor applied to the flow output.
func RunEnsembleWithInterventions(
	baseRunoffParams map[string][]float64,
	rainfallParams RainfallParams,
	baseRoutingCoeffs []float64,
	subCatchments []string,
	portfolio Portfolio,
	priors InterventionPriors,
	nSteps int,
	nMembers int,
	spinUp int,
	baseSeed uint64,
	rng *rand.Rand,
) EnsembleSummary {
	peakFlows := make([]float64, nMembers)
	var sumPeak, sumMean, maxPeak float64

	for i := 0; i < nMembers; i++ {
		// Sample intervention effectiveness for this member.
		modifiedParams, modifiedRouting := ApplyPortfolio(
			portfolio, baseRunoffParams, baseRoutingCoeffs,
			subCatchments, priors, rng,
		)

		// Run stochastic rainfall-runoff.
		result := RunStochasticModel(
			modifiedParams, rainfallParams,
			nSteps, baseSeed+uint64(i),
		)

		// Apply aggregate routing attenuation to flow output.
		// The routing effect is the mean attenuation across sub-catchments.
		avgRouting := meanRoutingFactor(baseRoutingCoeffs, modifiedRouting)

		// Compute peak/mean after spin-up with routing attenuation.
		peak := 0.0
		flowSum := 0.0
		n := 0
		for j := spinUp; j < len(result.SimFlow); j++ {
			flow := result.SimFlow[j] * avgRouting
			if flow > peak {
				peak = flow
			}
			flowSum += flow
			n++
		}

		peakFlows[i] = peak
		sumPeak += peak
		if n > 0 {
			sumMean += flowSum / float64(n)
		}
		if peak > maxPeak {
			maxPeak = peak
		}
	}

	meanPeak := sumPeak / float64(nMembers)
	var varPeak float64
	for _, p := range peakFlows {
		d := p - meanPeak
		varPeak += d * d
	}
	varPeak /= float64(nMembers)

	sorted := make([]float64, len(peakFlows))
	copy(sorted, peakFlows)
	sortFloat64s(sorted)
	p95Idx := int(0.95 * float64(len(sorted)-1))

	return EnsembleSummary{
		NMembers:     nMembers,
		MeanPeakFlow: meanPeak,
		StdPeakFlow:  math.Sqrt(varPeak),
		MaxPeakFlow:  maxPeak,
		MeanMeanFlow: sumMean / float64(nMembers),
		P95PeakFlow:  sorted[p95Idx],
	}
}

// meanRoutingFactor computes the ratio of modified to baseline routing
// coefficients, averaged across sub-catchments. A value < 1 means
// interventions are attenuating flow.
func meanRoutingFactor(baseline, modified []float64) float64 {
	if len(baseline) == 0 {
		return 1.0
	}
	sum := 0.0
	for i := range baseline {
		if i < len(modified) && baseline[i] > 0 {
			sum += modified[i] / baseline[i]
		} else {
			sum += 1.0
		}
	}
	return sum / float64(len(baseline))
}
