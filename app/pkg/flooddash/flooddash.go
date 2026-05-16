// Package flooddash is the dexetera dashboard for the floodrisk
// post — "Managing flood damage risk under climate change". The
// simulator under the hood is the project's stochastic
// rainfall-runoff model for the Upper Calder Valley, calibrated
// against 16 years of EA observations and driven by the four
// canonical NFM portfolios × five UKCP18 climate scenarios from
// pkg/catchment.
//
// The controls are two discrete selectors: a portfolio choice
// (no_intervention / leaky_dams / woodland / mixed) and a climate
// scenario choice (baseline / RCP4.5_2040 / RCP4.5_2070 /
// RCP8.5_2040 / RCP8.5_2070). The visualisation has three panels:
//
//   - Peak flow distribution histogram (live, fills as 25 ensemble
//     members complete) with markers for the user's running mean and
//     the no-intervention reference under the current scenario.
//   - Cost-effectiveness scatter — four portfolio dots positioned on
//     (cost £, peak-flow reduction %), with the user's selection
//     highlighted in the action colour. The diminishing-returns
//     finding (£1M woodland beats £2M mixed) shows up directly as
//     the woodland dot sitting above the mixed dot at every scenario.
//   - Climate sensitivity scatter — five scenario dots positioned on
//     (rainfall multiplier, peak flow) for the current portfolio,
//     with the current scenario highlighted. The climate-amplification
//     finding (35% rainfall → ~53% peak flow) is visible as the dots
//     rising faster than a straight line.
//
// See app/cmd/flood/{register_step,generate} for the wasm entry-point
// and the codegen that emits the widget shell respectively.
package flooddash

import (
	"fmt"

	"github.com/umbralcalc/dexetera/pkg/dashboard"
)

// actionColorHex is the magenta the Acting on Simulated Systems
// collection uses to signal "this is what the reader controls". Kept
// in sync with the recolouring constant in cmd/flood/generate so the
// canvas markers and the HTML radio accents match.
const actionColorHex = "#b0447a"

// referenceColorHex is the slate grey used for static reference
// markers (no-intervention reference, non-selected portfolios and
// scenarios). Same hue as the AMR reference bars so the visual
// language carries across the collection.
const referenceColorHex = "#7d8aa1"

// labelFontSize is the font size for static labels on the canvas
// (axis tick labels, panel captions). The canvas natively renders at
// 640px but is CSS-scaled to fit the panel — typically ~410px wide
// at standard blog embeddings, and as narrow as ~320px on mobile —
// so font sizes need to be set in canvas-space pixels that stay
// readable after up to a 2× scale-down.
const labelFontSize = 18

const (
	// Panel-title font size — larger than axis labels so the three
	// panel titles read at a glance.
	titleFontSize = 22

	// captionFontSize is the size for inline legend captions
	// ("grey: no-intervention mean ..." etc.). Smaller than axis
	// labels so they read as ancillary annotation, but still large
	// enough to remain legible at the mobile scale-down.
	captionFontSize = 15
)

// NewConfig returns the dashboard.Config for the floodrisk widget.
// Declaration order of renderers matters: later renderers draw on
// top. Static frame elements (panel separators, axis lines, text
// labels) are added first; partition-bound markers (histogram bars,
// scatter dots, highlights) on top.
func NewConfig() *dashboard.Config {
	vb := dashboard.NewVisualizationBuilder().
		WithCanvas(CanvasWidth, CanvasHeight).
		WithBackground("#fafafa").
		WithUpdateInterval(0)

	// ---- Histogram panel (top half) ----

	// Panel title.
	vb = vb.AddText("", "Peak flow distribution (m³/s)",
		HistX0, HistY0-20,
		&dashboard.TextOptions{
			Color:     "#2c3e50",
			FontSize:  titleFontSize,
			TextAlign: "left",
		})

	// Panel frame — top + baseline.
	vb = vb.AddLine("",
		HistX0, HistY0,
		HistX0+HistWidth, HistY0,
		&dashboard.LineOptions{Color: "#e3e6ec", Width: 1}).
		AddLine("",
			HistX0, HistY0+HistHeight,
			HistX0+HistWidth, HistY0+HistHeight,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})

	// X-axis tick labels — every 20 m³/s along the panel.
	for flow := 40.0; flow <= 120.0; flow += 20.0 {
		x := flowToX(flow)
		// Short tick mark.
		vb = vb.AddLine("",
			int(x), HistY0+HistHeight,
			int(x), HistY0+HistHeight+4,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})
		vb = vb.AddText("",
			fmt.Sprintf("%.0f", flow),
			int(x), HistY0+HistHeight+24,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: "center",
			})
	}

	// Histogram bars themselves — the live, accumulating distribution.
	vb = vb.AddRectangleSet("histogram_bars", 0, 0, &dashboard.ShapeOptions{
		FillColor: "#7da8d8",
		Anchor:    "topLeft",
	})

	// Reference marker (grey vertical bar) at the no-intervention
	// mean under the current scenario.
	vb = vb.AddRectangleSet("histogram_ref_marker", 0, 0, &dashboard.ShapeOptions{
		FillColor: referenceColorHex,
		Anchor:    "topLeft",
	})

	// Live mean marker (magenta vertical bar) — drawn last so it sits
	// on top of both the bars and the reference marker. The marker
	// semantics (grey = no-intervention reference; magenta = your
	// live mean) match the action-colour convention used throughout
	// the post, so they're left to be discovered visually rather
	// than spelled out in an on-canvas legend that would compete
	// with the panel title at the larger font size.

	// ---- Cost-effectiveness panel (bottom-left) ----

	// Panel title.
	vb = vb.AddText("", "Cost vs peak-flow reduction",
		CostX0, CostY0-22,
		&dashboard.TextOptions{
			Color:     "#2c3e50",
			FontSize:  titleFontSize,
			TextAlign: "left",
		})

	// X-axis (cost) + Y-axis (reduction %).
	vb = vb.AddLine("",
		CostX0, CostY0+CostHeight,
		CostX0+CostWidth, CostY0+CostHeight,
		&dashboard.LineOptions{Color: "#2c3e50", Width: 1}).
		AddLine("",
			CostX0, CostY0,
			CostX0, CostY0+CostHeight,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})

	// Y-axis tick labels (peak reduction %).
	for pct := 0.0; pct <= CostMaxReductionPct; pct += 5.0 {
		y := reductionToY(pct)
		vb = vb.AddLine("",
			CostX0-4, int(y),
			CostX0, int(y),
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})
		vb = vb.AddText("",
			fmt.Sprintf("%.0f%%", pct),
			CostX0-8, int(y)+6,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: "right",
			})
	}

	// X-axis tick labels (cost in £M).
	for _, m := range []float64{0, 1_000_000, 2_000_000} {
		x := costToX(m)
		vb = vb.AddLine("",
			int(x), CostY0+CostHeight,
			int(x), CostY0+CostHeight+4,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})
		label := fmt.Sprintf("£%.0fM", m/1_000_000)
		if m == 0 {
			label = "£0"
		}
		vb = vb.AddText("",
			label,
			int(x), CostY0+CostHeight+24,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: "center",
			})
	}

	// Cost-effectiveness dots — all four portfolios in reference grey.
	vb = vb.AddRectangleSet("cost_dots", 0, 0, &dashboard.ShapeOptions{
		FillColor: referenceColorHex,
		Anchor:    "topLeft",
	})
	// User's selected portfolio highlight — drawn last so it sits on
	// top of the reference dots.
	vb = vb.AddRectangleSet("cost_highlight", 0, 0, &dashboard.ShapeOptions{
		FillColor: actionColorHex,
		Anchor:    "topLeft",
	})

	// Portfolio name labels next to each dot — drawn at the reference
	// positions so the reader can identify which dot is which without
	// hovering. Labels use labelFontSize (the same as axis tick
	// labels) for readability, with positions hand-picked so the
	// wood/mixed labels don't crowd each other under the baseline
	// climate scenario (where the dots sit closest vertically).
	portfolioLabels := []string{"none", "dams", "wood", "mixed"}
	for p := 0; p < NumPortfolios; p++ {
		baseline := ReferenceMeanPeaks[PortfolioNoIntervention][ScenarioBaseline]
		reduction := 0.0
		if baseline > 0 {
			reduction = (baseline - ReferenceMeanPeaks[p][ScenarioBaseline]) / baseline * 100
		}
		x := costToX(PortfolioCosts[p])
		y := reductionToY(reduction)
		labelX := int(x) + 12
		labelY := int(y) - 12 // above-and-right of the dot by default
		align := "left"
		switch p {
		case PortfolioMixed:
			// Mixed sits just below woodland on the y-axis (the
			// diminishing-returns finding the panel is built to
			// show). Place its label BELOW the dot so the wood and
			// mixed labels can't ever overlap, even with the wider
			// labelFontSize text. Right-align so it stays inside
			// the panel from the right edge of the dot. The +28 y
			// offset accounts for the font ascent so the label top
			// clears the dot bottom by ~5px.
			labelX = int(x) - 12
			labelY = int(y) + 28
			align = "right"
		case PortfolioWoodland:
			// Right-align woodland's label too so it sits between
			// the dam dot (to its left) and the mixed dot (to its
			// right) without colliding with either.
			labelX = int(x) - 12
			align = "right"
		}
		vb = vb.AddText("",
			portfolioLabels[p],
			labelX, labelY,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: align,
			})
	}

	// ---- Climate-sensitivity panel (bottom-right) ----

	// Panel title.
	vb = vb.AddText("", "Climate sensitivity",
		ClimateX0, ClimateY0-22,
		&dashboard.TextOptions{
			Color:     "#2c3e50",
			FontSize:  titleFontSize,
			TextAlign: "left",
		})

	// Axes.
	vb = vb.AddLine("",
		ClimateX0, ClimateY0+ClimateHeight,
		ClimateX0+ClimateWidth, ClimateY0+ClimateHeight,
		&dashboard.LineOptions{Color: "#2c3e50", Width: 1}).
		AddLine("",
			ClimateX0, ClimateY0,
			ClimateX0, ClimateY0+ClimateHeight,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})

	// Y-axis tick labels (peak flow m³/s).
	for peak := 40.0; peak <= 90.0; peak += 10.0 {
		y := peakToY(peak)
		vb = vb.AddLine("",
			ClimateX0-4, int(y),
			ClimateX0, int(y),
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})
		vb = vb.AddText("",
			fmt.Sprintf("%.0f", peak),
			ClimateX0-8, int(y)+6,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: "right",
			})
	}

	// X-axis tick labels (rainfall multipliers).
	for _, mult := range []float64{1.0, 1.1, 1.2, 1.3, 1.4} {
		x := multiplierToX(mult)
		vb = vb.AddLine("",
			int(x), ClimateY0+ClimateHeight,
			int(x), ClimateY0+ClimateHeight+4,
			&dashboard.LineOptions{Color: "#2c3e50", Width: 1})
		vb = vb.AddText("",
			fmt.Sprintf("×%.1f", mult),
			int(x), ClimateY0+ClimateHeight+24,
			&dashboard.TextOptions{
				Color:     "#2c3e50",
				FontSize:  labelFontSize,
				TextAlign: "center",
			})
	}

	// "Linear reference" — a dashed grey line from (multiplier 1.0,
	// no-intervention baseline peak) to (multiplier 1.4, baseline × 1.4).
	// This is what a perfectly linear climate response would look like.
	// The actual scenario dots curve above this line, making the
	// nonlinearity visible at a glance. DashPattern is sized for the
	// CSS-scaled canvas — 12/8 reads as roughly 7/5 visible pixels at
	// the typical ~0.6× embed scale.
	baselinePeak := ReferenceMeanPeaks[PortfolioNoIntervention][ScenarioBaseline]
	linearStartX := multiplierToX(1.0)
	linearStartY := peakToY(baselinePeak)
	linearEndX := multiplierToX(ClimateMaxMult)
	linearEndY := peakToY(baselinePeak * ClimateMaxMult)
	vb = vb.AddLine("",
		int(linearStartX), int(linearStartY),
		int(linearEndX), int(linearEndY),
		&dashboard.LineOptions{
			Color:       referenceColorHex,
			Width:       2,
			DashPattern: []int{12, 8},
		})

	// Climate-sensitivity dots — five scenarios for the current portfolio.
	vb = vb.AddRectangleSet("climate_dots", 0, 0, &dashboard.ShapeOptions{
		FillColor: referenceColorHex,
		Anchor:    "topLeft",
	})
	// User's selected scenario highlight.
	vb = vb.AddRectangleSet("climate_highlight", 0, 0, &dashboard.ShapeOptions{
		FillColor: actionColorHex,
		Anchor:    "topLeft",
	})

	// Caption explaining the dashed line — stacked on two lines so
	// each phrase reads at a glance rather than as a single 45-char
	// strip that's hard to scan at the embed-scaled font size.
	vb = vb.AddText("",
		"dashed: linear response",
		ClimateX0, ClimateY0+ClimateHeight+40,
		&dashboard.TextOptions{
			Color:     "#2c3e50",
			FontSize:  captionFontSize,
			TextAlign: "left",
		})
	vb = vb.AddText("",
		"dots: model response",
		ClimateX0, ClimateY0+ClimateHeight+60,
		&dashboard.TextOptions{
			Color:     "#2c3e50",
			FontSize:  captionFontSize,
			TextAlign: "left",
		})

	vis := vb.Build()

	cfg := dashboard.NewConfigBuilder("flood").
		WithDescription("Catchment flood policy support: pick a portfolio of natural flood management interventions and a climate scenario; the simulator (calibrated to Environment Agency data from the Upper Calder Valley) shows peak flow distributions over a 25-member ensemble of 5-year stochastic runs. This is a research model fitted to surveillance data, not a flood-risk planning tool.").
		WithServerPartition("ensemble_member").
		WithServerPartition("peak_stats").
		WithServerPartition("histogram_bars").
		WithServerPartition("histogram_mean_marker").
		WithServerPartition("histogram_ref_marker").
		WithServerPartition("cost_dots").
		WithServerPartition("cost_highlight").
		WithServerPartition("climate_dots").
		WithServerPartition("climate_highlight").
		WithServerPartition("display_progress").
		WithServerPartition("display_cost").
		WithActionStatePartition("policy_action").
		WithVisualization(vis).
		WithSimulation(BuildFloodSimulation)

	// The two policy sliders are replaced with radio buttons by
	// post-processing in cmd/flood/generate. They're kept in the data
	// model so dexetera's slider→worker action publication mechanism
	// still carries the values to wasm. The labels below are what
	// generate.go uses to find and hide them.
	cfg = cfg.
		WithSlider(dashboard.Slider{
			Name:       "portfolio",
			Label:      "Portfolio (radio-controlled)",
			Partition:  "policy_action",
			ValueIndex: PAIdxPortfolio,
			Min:        0,
			Max:        NumPortfolios - 1,
			Step:       1,
			Default:    PortfolioNoIntervention,
			Decimals:   0,
		}).
		WithSlider(dashboard.Slider{
			Name:       "scenario",
			Label:      "Scenario (radio-controlled)",
			Partition:  "policy_action",
			ValueIndex: PAIdxScenario,
			Min:        0,
			Max:        NumScenarios - 1,
			Step:       1,
			Default:    ScenarioBaseline,
			Decimals:   0,
		})

	cfg = cfg.
		WithReadout(dashboard.Readout{
			Partition: "display_progress",
			Template:  fmt.Sprintf("member {v%d} of %d · live mean peak {v%d} ± {v%d} m³/s",
				0, SimMembers, 1, 2),
			Decimals: 1,
		}).
		WithReadout(dashboard.Readout{
			Partition: "display_cost",
			Template:  fmt.Sprintf("cost £{v%d}M · live peak reduction {v%d}%%", 0, 1),
			Decimals:  1,
		}).
		WithResetButton().
		WithInlineDriver(30)

	return cfg.Build()
}
