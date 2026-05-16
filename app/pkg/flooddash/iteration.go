package flooddash

import (
	"math"
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/catchment"
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// SimMembers is the number of ensemble members the dashboard runs before
// halting. Each outer simulation step completes one stochastic
// rainfall-runoff member (a full 5-year simulation done in one Iterate
// call), so SimMembers also sets the visible "step count" on the page.
// 25 is enough to show distribution shape clearly without making the
// reader wait too long; raise this for more stable histograms at the cost
// of responsiveness.
const SimMembers = 25

// MemberDays is the per-member simulation horizon, in days. 1825 days =
// 5 years. The offline policy evaluation uses 3650 days (10 years), but
// in the browser we trade some statistical convergence for runtime — a
// 5-year horizon still produces multiple meaningful peak events per
// member at this catchment's flow regime.
const MemberDays = 1825

// SpinUpDays is the per-member warm-up period thrown away before peak
// detection. Matches the offline evaluation's default.
const SpinUpDays = 30

// Action vector layout. The slider/radio panel writes to
// action_state_values in this order; PolicyActionIteration latches it
// onto state.
const (
	PAIdxPortfolio = 0
	PAIdxScenario  = 1
	PolicyActionLen = 2
)

// Portfolio and scenario indices. These match CandidatePortfolios() and
// StandardClimateScenarios() in pkg/catchment, in that order.
const (
	PortfolioNoIntervention = 0
	PortfolioLeakyDams      = 1
	PortfolioWoodland       = 2
	PortfolioMixed          = 3
	NumPortfolios           = 4

	ScenarioBaseline    = 0
	ScenarioRCP45_2040  = 1
	ScenarioRCP45_2070  = 2
	ScenarioRCP85_2040  = 3
	ScenarioRCP85_2070  = 4
	NumScenarios        = 5
)

// PortfolioCosts is the cost (£) of each candidate portfolio. Order
// matches the portfolio indices above and CandidatePortfolios().
var PortfolioCosts = []float64{0, 500_000, 1_000_000, 2_000_000}

// ScenarioMultipliers is the rainfall multiplier (UKCP18-informed) of
// each climate scenario. Order matches StandardClimateScenarios().
var ScenarioMultipliers = []float64{1.0, 1.10, 1.20, 1.15, 1.35}

// ReferenceMeanPeaks is the mean peak flow (m³/s) of each portfolio
// under each scenario, taken from the project's offline policy
// evaluation (25-member ensembles, 5-year horizons, 200 calibration
// trials). Rows = portfolios (no_intervention, leaky_dams, woodland,
// mixed); cols = scenarios (baseline, RCP4.5_2040, RCP4.5_2070,
// RCP8.5_2040, RCP8.5_2070).
//
// Refresh with:
//
//	go run ./cmd/evaluate -members 25 -steps 1825 -cal-trials 200
//
// then transcribe the "MeanPeak" column from the results table.
var ReferenceMeanPeaks = [NumPortfolios][NumScenarios]float64{
	{54.59, 62.80, 70.67, 66.51, 83.43}, // no_intervention
	{51.90, 59.70, 67.18, 63.22, 79.31}, // leaky_dams_only
	{48.17, 56.05, 63.73, 59.69, 76.11}, // woodland_focus
	{48.93, 56.59, 64.05, 60.14, 76.03}, // mixed_portfolio
}

// terminated reports whether the simulation has reached SimMembers.
// Once all members are done, custom iterations freeze their state so
// switching the radio buttons after the run completes doesn't silently
// rewind the trajectory — the reader uses Reset to rerun.
func terminated(timestepsHistory *simulator.CumulativeTimestepsHistory) bool {
	return timestepsHistory.Values.AtVec(0) >= float64(SimMembers)
}

// PolicyActionIteration is the slider/radio-driven action partition.
// It echoes the most recent action_state_values vector as state so the
// downstream ensemble runner reads its choices from state history.
//
// State width: PolicyActionLen.
type PolicyActionIteration struct{}

func (p *PolicyActionIteration) Configure(int, *simulator.Settings) {}

func (p *PolicyActionIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if terminated(timestepsHistory) {
		return stateHistories[partitionIndex].CopyStateRow(0)
	}
	out := make([]float64, PolicyActionLen)
	if actions, ok := params.GetOk("action_state_values"); ok {
		for i := 0; i < PolicyActionLen && i < len(actions); i++ {
			out[i] = actions[i]
		}
		return out
	}
	prev := stateHistories[partitionIndex].CopyStateRow(0)
	copy(out, prev[:PolicyActionLen])
	return out
}

// EnsembleMemberIteration runs one stochastic rainfall-runoff member
// per outer step. Each member is a fresh MemberDays-day simulation
// with its own seed (BaseSeed + memberIndex), so the histogram fills
// in progressively as outer steps tick.
//
// Reads the current portfolio + scenario from the upstream
// policy_action partition. The portfolio is applied via
// catchment.ApplyPortfolio (sub-catchment-aware), the scenario via the
// stochastic rainfall generator's rainfall_multiplier param.
//
// State: [peak_flow, mean_flow, member_idx].
type EnsembleMemberIteration struct {
	baseRunoffParams  map[string][]float64
	rainfallParams    catchment.RainfallParams
	routingCoeffs     []float64
	subCatchments     []string
	priors            catchment.InterventionPriors
	portfolios        []catchment.Portfolio
	scenarios         []catchment.ClimateScenario
	baseSeed          uint64
	rng               *rand.Rand
}

// NewEnsembleMemberIteration constructs the runner with fixed
// background parameters — calibrated PDM, fitted rainfall generator,
// and routing coefficients. Sub-catchments and priors come from the
// project's defaults; portfolios and scenarios are the four canonical
// portfolios × five UKCP18 scenarios.
func NewEnsembleMemberIteration(
	runoffParams map[string][]float64,
	rainfallParams catchment.RainfallParams,
	routingCoeffs []float64,
	subCatchments []string,
	baseSeed uint64,
) *EnsembleMemberIteration {
	return &EnsembleMemberIteration{
		baseRunoffParams: runoffParams,
		rainfallParams:   rainfallParams,
		routingCoeffs:    routingCoeffs,
		subCatchments:    subCatchments,
		priors:           catchment.DefaultInterventionPriors(),
		portfolios:       catchment.CandidatePortfolios(),
		scenarios:        catchment.StandardClimateScenarios(),
		baseSeed:         baseSeed,
	}
}

func (e *EnsembleMemberIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {
	seed := settings.Iterations[partitionIndex].Seed
	if seed == 0 {
		seed = 1
	}
	e.rng = rand.New(rand.NewPCG(seed, seed+1))
}

func (e *EnsembleMemberIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if terminated(timestepsHistory) {
		return stateHistories[partitionIndex].CopyStateRow(0)
	}

	// member_idx is 0-based and increments each step.
	memberIdx := int(timestepsHistory.Values.AtVec(0))

	action := params.Get("policy_action")
	portfolioIdx := clampInt(int(math.Round(action[PAIdxPortfolio])), 0, NumPortfolios-1)
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)

	portfolio := e.portfolios[portfolioIdx]
	scenario := e.scenarios[scenarioIdx]

	// Apply portfolio interventions (sampled for this member).
	modifiedParams, modifiedRouting := catchment.ApplyPortfolio(
		portfolio,
		e.baseRunoffParams,
		e.routingCoeffs,
		e.subCatchments,
		e.priors,
		e.rng,
	)

	// Apply climate scenario via rainfall multiplier.
	rainfallParams := e.rainfallParams
	rainfallParams.RainfallMultiplier = scenario.RainfallMultiplier

	// Run one stochastic rainfall-runoff member.
	result := catchment.RunStochasticModel(
		modifiedParams, rainfallParams,
		MemberDays, e.baseSeed+uint64(memberIdx),
	)

	// Apply aggregate routing attenuation (mean factor across
	// sub-catchments) to the flow output, matching the way
	// RunEnsembleWithInterventions handles the single-catchment proxy
	// for multi-sub-catchment routing.
	avgRouting := meanRoutingFactor(e.routingCoeffs, modifiedRouting)
	peak := 0.0
	flowSum := 0.0
	n := 0
	for j := SpinUpDays; j < len(result.SimFlow); j++ {
		flow := result.SimFlow[j] * avgRouting
		if flow > peak {
			peak = flow
		}
		flowSum += flow
		n++
	}
	meanFlow := 0.0
	if n > 0 {
		meanFlow = flowSum / float64(n)
	}

	return []float64{peak, meanFlow, float64(memberIdx)}
}

// meanRoutingFactor mirrors the unexported helper in pkg/catchment so
// the dashboard iteration doesn't have to depend on internals — the
// modified/baseline routing coefficient ratio averaged across
// sub-catchments. A factor < 1 means interventions are attenuating
// flow.
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

// PeakStatsIteration tracks running (count, mean, std, max) of the
// peak flow values emitted by EnsembleMemberIteration. Welford's
// online algorithm for numerical stability of std under repeated
// updates.
//
// State: [count, mean, std, max].
type PeakStatsIteration struct{}

func (p *PeakStatsIteration) Configure(int, *simulator.Settings) {}

func (p *PeakStatsIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	prev := stateHistories[partitionIndex].CopyStateRow(0)
	if terminated(timestepsHistory) {
		return prev
	}
	count := prev[0]
	mean := prev[1]
	m2 := prev[2] // squared-deviation accumulator
	max := prev[3]

	peak := params.Get("member_values")[0]

	count++
	delta := peak - mean
	mean += delta / count
	delta2 := peak - mean
	m2 += delta * delta2
	if peak > max {
		max = peak
	}

	std := 0.0
	if count > 1 {
		std = math.Sqrt(m2 / (count - 1))
	}

	return []float64{count, mean, std, max}
}

// Canvas geometry shared by visualization and bar/dot iterations. Keep
// in sync with flooddash.go's AddRectangleSet / AddText / AddLine calls.
const (
	CanvasWidth  = 640
	CanvasHeight = 480

	// Histogram panel — top half of the canvas.
	HistX0     = 40
	HistY0     = 60
	HistWidth  = 560
	HistHeight = 110
	HistNBins  = 22
	// Histogram x-axis covers peak flows from HistMinFlow to HistMaxFlow.
	// Chosen so all reference means + a margin for the live ensemble
	// (which extends past the mean by 1-2 sigma per the offline std table)
	// fit inside the panel under every portfolio/scenario combo.
	HistMinFlow = 30.0
	HistMaxFlow = 130.0
	// HistBarCap: visual cap on bin count so the bars don't grow off the
	// panel top. The reset button restarts the run, so over-counting
	// past this cap just means the bar plateaus.
	HistBarCap = 6.0

	// Cost-effectiveness panel — bottom-left quadrant.
	CostX0     = 60
	CostY0     = 260
	CostWidth  = 220
	CostHeight = 140
	// Cost axis covers 0 to £2M (matches mixed_portfolio cost).
	CostMaxGBP = 2_200_000.0
	// Peak-reduction axis covers 0 to 15% (matches the highest reduction
	// across the reference table, woodland_focus baseline = 11.8%).
	CostMaxReductionPct = 15.0

	// Climate sensitivity panel — bottom-right quadrant.
	ClimateX0     = 380
	ClimateY0     = 260
	ClimateWidth  = 220
	ClimateHeight = 140
	// Climate axis covers rainfall multipliers 1.0 to 1.4 (covers
	// baseline through RCP8.5_2070 = 1.35 with a small margin).
	ClimateMinMult = 1.0
	ClimateMaxMult = 1.4
	// Climate y-axis covers peak flows 40 to 90 m³/s, the range
	// spanned by the reference table.
	ClimateMinPeak = 40.0
	ClimateMaxPeak = 95.0

	// Dot/marker size for the cost-effectiveness and climate-sensitivity
	// scatter plots. Drawn as a square via rectangleSet anchored top-left
	// then shifted by half-size so it appears centred at (x, y).
	MarkerSize     = 9
	HighlightSize  = 13
)

// HistogramBarsIteration emits one rectangle per histogram bin. The
// rectangle's height is proportional to the bin count (capped at
// HistBarCap so the bar plateaus rather than overflowing the panel).
//
// State width: HistNBins * 4 floats.
type HistogramBarsIteration struct {
	counts []float64
}

func (h *HistogramBarsIteration) Configure(int, *simulator.Settings) {
	h.counts = make([]float64, HistNBins)
}

func (h *HistogramBarsIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if !terminated(timestepsHistory) {
		peak := params.Get("member_values")[0]
		binWidth := (HistMaxFlow - HistMinFlow) / float64(HistNBins)
		idx := int(math.Floor((peak - HistMinFlow) / binWidth))
		if idx >= 0 && idx < HistNBins {
			h.counts[idx]++
		}
	}

	binWidthPx := float64(HistWidth) / float64(HistNBins)
	out := make([]float64, HistNBins*4)
	for i := 0; i < HistNBins; i++ {
		frac := math.Min(h.counts[i]/HistBarCap, 1.0)
		barH := frac * float64(HistHeight)
		out[i*4+0] = float64(HistX0) + float64(i)*binWidthPx + 1
		out[i*4+1] = float64(HistY0) + float64(HistHeight) - barH
		out[i*4+2] = binWidthPx - 2
		out[i*4+3] = barH
	}
	return out
}

// HistogramMeanMarkerIteration emits a single thin vertical bar at the
// user's running mean peak flow, sitting on top of the histogram bars
// in the action colour. This is the "your portfolio's central peak"
// readout in the panel.
//
// State width: 4.
type HistogramMeanMarkerIteration struct{}

func (m *HistogramMeanMarkerIteration) Configure(int, *simulator.Settings) {}

func (m *HistogramMeanMarkerIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	stats := params.Get("stats_values")
	count := stats[0]
	mean := stats[1]
	if count <= 0 {
		return []float64{0, 0, 0, 0}
	}
	x := flowToX(mean)
	return []float64{
		x - 1,
		float64(HistY0),
		2,
		float64(HistHeight),
	}
}

// HistogramReferenceMarkerIteration emits a single thin vertical bar
// at the no_intervention mean peak under the currently-selected
// climate scenario. This is the "what you'd get if you did nothing"
// readout in the panel — drawn before the user's mean marker so the
// reader can compare the two.
//
// State width: 4.
type HistogramReferenceMarkerIteration struct{}

func (m *HistogramReferenceMarkerIteration) Configure(int, *simulator.Settings) {}

func (m *HistogramReferenceMarkerIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	action := params.Get("policy_action")
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)
	refPeak := ReferenceMeanPeaks[PortfolioNoIntervention][scenarioIdx]
	x := flowToX(refPeak)
	return []float64{
		x - 1,
		float64(HistY0),
		2,
		float64(HistHeight),
	}
}

// CostDotsIteration emits four dots — one per portfolio — positioned
// on a (cost, peak-reduction%) plot under the currently-selected
// climate scenario. The reader sees all four portfolios' positions at
// once; the diminishing-returns finding (£1M woodland beats £2M
// mixed) is visible directly from the dot heights.
//
// State width: NumPortfolios * 4 floats.
type CostDotsIteration struct{}

func (c *CostDotsIteration) Configure(int, *simulator.Settings) {}

func (c *CostDotsIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	action := params.Get("policy_action")
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)

	baseline := ReferenceMeanPeaks[PortfolioNoIntervention][scenarioIdx]
	out := make([]float64, NumPortfolios*4)
	for p := 0; p < NumPortfolios; p++ {
		reduction := 0.0
		if baseline > 0 {
			reduction = (baseline - ReferenceMeanPeaks[p][scenarioIdx]) / baseline * 100
		}
		x := costToX(PortfolioCosts[p])
		y := reductionToY(reduction)
		out[p*4+0] = x - float64(MarkerSize)/2
		out[p*4+1] = y - float64(MarkerSize)/2
		out[p*4+2] = float64(MarkerSize)
		out[p*4+3] = float64(MarkerSize)
	}
	return out
}

// CostHighlightIteration emits a single larger marker at the user's
// currently-selected portfolio's (cost, reduction) point. Drawn after
// the dots so it sits on top, in the action colour.
//
// State width: 4.
type CostHighlightIteration struct{}

func (c *CostHighlightIteration) Configure(int, *simulator.Settings) {}

func (c *CostHighlightIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	action := params.Get("policy_action")
	portfolioIdx := clampInt(int(math.Round(action[PAIdxPortfolio])), 0, NumPortfolios-1)
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)
	baseline := ReferenceMeanPeaks[PortfolioNoIntervention][scenarioIdx]
	reduction := 0.0
	if baseline > 0 {
		reduction = (baseline - ReferenceMeanPeaks[portfolioIdx][scenarioIdx]) / baseline * 100
	}
	x := costToX(PortfolioCosts[portfolioIdx])
	y := reductionToY(reduction)
	return []float64{
		x - float64(HighlightSize)/2,
		y - float64(HighlightSize)/2,
		float64(HighlightSize),
		float64(HighlightSize),
	}
}

// ClimateDotsIteration emits five dots — one per scenario — positioned
// on a (rainfall multiplier, peak flow) plot for the currently-selected
// portfolio. The reader sees the shape of the climate-sensitivity
// curve, including its nonlinearity (35% rainfall → 53% peak flow).
//
// State width: NumScenarios * 4 floats.
type ClimateDotsIteration struct{}

func (c *ClimateDotsIteration) Configure(int, *simulator.Settings) {}

func (c *ClimateDotsIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	action := params.Get("policy_action")
	portfolioIdx := clampInt(int(math.Round(action[PAIdxPortfolio])), 0, NumPortfolios-1)

	out := make([]float64, NumScenarios*4)
	for s := 0; s < NumScenarios; s++ {
		x := multiplierToX(ScenarioMultipliers[s])
		y := peakToY(ReferenceMeanPeaks[portfolioIdx][s])
		out[s*4+0] = x - float64(MarkerSize)/2
		out[s*4+1] = y - float64(MarkerSize)/2
		out[s*4+2] = float64(MarkerSize)
		out[s*4+3] = float64(MarkerSize)
	}
	return out
}

// ClimateHighlightIteration emits a single larger marker at the user's
// currently-selected scenario's (multiplier, peak) point. Drawn after
// the dots so it sits on top, in the action colour.
//
// State width: 4.
type ClimateHighlightIteration struct{}

func (c *ClimateHighlightIteration) Configure(int, *simulator.Settings) {}

func (c *ClimateHighlightIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	action := params.Get("policy_action")
	portfolioIdx := clampInt(int(math.Round(action[PAIdxPortfolio])), 0, NumPortfolios-1)
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)
	x := multiplierToX(ScenarioMultipliers[scenarioIdx])
	y := peakToY(ReferenceMeanPeaks[portfolioIdx][scenarioIdx])
	return []float64{
		x - float64(HighlightSize)/2,
		y - float64(HighlightSize)/2,
		float64(HighlightSize),
		float64(HighlightSize),
	}
}

// DisplayProgressIteration is the readout partition that surfaces the
// member count + the live ensemble's running mean peak flow + std.
// Bound to a readout template like:
//
//	member {v0} of 25 · mean peak {v1} ± {v2} m³/s
//
// State width: 3.
type DisplayProgressIteration struct{}

func (d *DisplayProgressIteration) Configure(int, *simulator.Settings) {}

func (d *DisplayProgressIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if terminated(timestepsHistory) {
		return stateHistories[partitionIndex].CopyStateRow(0)
	}
	stats := params.Get("stats_values")
	return []float64{stats[0], stats[1], stats[2]}
}

// DisplayCostIteration is the readout partition that surfaces the
// user's selected portfolio cost (£) and live peak-flow reduction (%)
// vs the no-intervention reference under the same scenario.
//
// State width: 2.
type DisplayCostIteration struct{}

func (d *DisplayCostIteration) Configure(int, *simulator.Settings) {}

func (d *DisplayCostIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	if terminated(timestepsHistory) {
		return stateHistories[partitionIndex].CopyStateRow(0)
	}
	action := params.Get("policy_action")
	stats := params.Get("stats_values")

	portfolioIdx := clampInt(int(math.Round(action[PAIdxPortfolio])), 0, NumPortfolios-1)
	scenarioIdx := clampInt(int(math.Round(action[PAIdxScenario])), 0, NumScenarios-1)

	// Cost is surfaced in millions of pounds (£M), with one decimal —
	// so the readout reads "cost £0.5M" / "£1.0M" / "£2.0M" rather than
	// "£500000.0" / "£1000000.0" etc. The matching readout template in
	// flooddash.go is "cost £{v0}M".
	costM := PortfolioCosts[portfolioIdx] / 1_000_000
	baseline := ReferenceMeanPeaks[PortfolioNoIntervention][scenarioIdx]
	mean := stats[1]
	count := stats[0]

	reduction := 0.0
	if count > 0 && baseline > 0 {
		reduction = (baseline - mean) / baseline * 100
	}
	return []float64{costM, reduction}
}

// Coordinate-system helpers — keep canvas-space math in one place so
// the panel positions are easy to tune.

func flowToX(flow float64) float64 {
	clamped := math.Min(math.Max(flow, HistMinFlow), HistMaxFlow)
	frac := (clamped - HistMinFlow) / (HistMaxFlow - HistMinFlow)
	return float64(HistX0) + frac*float64(HistWidth)
}

func costToX(cost float64) float64 {
	clamped := math.Min(math.Max(cost, 0), CostMaxGBP)
	frac := clamped / CostMaxGBP
	return float64(CostX0) + frac*float64(CostWidth)
}

func reductionToY(reduction float64) float64 {
	if reduction < 0 {
		reduction = 0
	}
	if reduction > CostMaxReductionPct {
		reduction = CostMaxReductionPct
	}
	frac := reduction / CostMaxReductionPct
	return float64(CostY0) + float64(CostHeight) - frac*float64(CostHeight)
}

func multiplierToX(mult float64) float64 {
	clamped := math.Min(math.Max(mult, ClimateMinMult), ClimateMaxMult)
	frac := (clamped - ClimateMinMult) / (ClimateMaxMult - ClimateMinMult)
	return float64(ClimateX0) + frac*float64(ClimateWidth)
}

func peakToY(peak float64) float64 {
	clamped := math.Min(math.Max(peak, ClimateMinPeak), ClimateMaxPeak)
	frac := (clamped - ClimateMinPeak) / (ClimateMaxPeak - ClimateMinPeak)
	return float64(ClimateY0) + float64(ClimateHeight) - frac*float64(ClimateHeight)
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
