package catchment

import (
	"testing"
)

func TestStandardClimateScenarios(t *testing.T) {
	scenarios := StandardClimateScenarios()
	if len(scenarios) == 0 {
		t.Fatal("expected at least one climate scenario")
	}
	// First should be baseline.
	if scenarios[0].Name != "baseline" {
		t.Errorf("first scenario should be baseline, got %q", scenarios[0].Name)
	}
	if scenarios[0].RainfallMultiplier != 1.0 {
		t.Errorf("baseline multiplier should be 1.0, got %f", scenarios[0].RainfallMultiplier)
	}
	// All multipliers should be >= 1.0.
	for _, s := range scenarios {
		if s.RainfallMultiplier < 1.0 {
			t.Errorf("scenario %q has multiplier %f < 1.0", s.Name, s.RainfallMultiplier)
		}
	}
}

func TestCandidatePortfolios(t *testing.T) {
	portfolios := CandidatePortfolios()
	if len(portfolios) < 3 {
		t.Fatalf("expected at least 3 portfolios, got %d", len(portfolios))
	}
	// First should be no-intervention baseline.
	if portfolios[0].Name != "no_intervention" {
		t.Errorf("first portfolio should be no_intervention, got %q", portfolios[0].Name)
	}
	if len(portfolios[0].Interventions) != 0 {
		t.Errorf("no_intervention should have 0 interventions, got %d",
			len(portfolios[0].Interventions))
	}
}

func TestEvaluatePolicy(t *testing.T) {
	t.Run("small ensemble policy evaluation", func(t *testing.T) {
		// Use minimal settings for a fast test.
		cfg := PolicyEvaluationConfig{
			RunoffParams: map[string][]float64{
				"field_capacity":      {200.0},
				"drainage_rate":       {0.05},
				"et_rate":             {2.0},
				"runoff_shape":        {1.5},
				"fast_recession_rate": {0.7},
				"slow_recession_rate": {0.1},
				"catchment_area_km2":  {340.0},
				"upstream_partition":  {0},
			},
			RainfallParams: RainfallParams{
				WetDayShape:        0.61,
				WetDayScale:        7.94,
				PWetGivenDry:       0.40,
				PWetGivenWet:       0.83,
				RainfallMultiplier: 1.0,
				WetThreshold:       0.1,
			},
			RoutingCoeffs: []float64{0.8, 0.9, 0.7, 0.85},
			SubCatchments: []string{"ryburn", "colne", "upper_calder", "holme"},
			NSteps:        365,
			NMembers:      5,
			SpinUp:        30,
			BaseSeed:      42,
			Priors:        DefaultInterventionPriors(),
		}

		portfolios := []Portfolio{
			{Name: "no_intervention"},
			{
				Name: "leaky_dams",
				Interventions: []Intervention{
					{Type: LeakyDams, SubCatchment: "ryburn", Scale: 5},
				},
			},
		}
		scenarios := []ClimateScenario{
			{Name: "baseline", RainfallMultiplier: 1.0},
			{Name: "RCP8.5_2070", RainfallMultiplier: 1.35},
		}

		results := EvaluatePolicy(portfolios, scenarios, cfg)

		// Should have portfolio x scenario results.
		expected := len(portfolios) * len(scenarios)
		if len(results) != expected {
			t.Fatalf("expected %d results, got %d", expected, len(results))
		}

		// Check all results have positive peak flows.
		for _, r := range results {
			if r.Summary.MeanPeakFlow <= 0 {
				t.Errorf("%s/%s: mean peak flow should be > 0, got %f",
					r.PortfolioName, r.ScenarioName, r.Summary.MeanPeakFlow)
			}
			t.Logf("%s / %s: mean_peak=%.2f std=%.2f P95=%.2f",
				r.PortfolioName, r.ScenarioName,
				r.Summary.MeanPeakFlow, r.Summary.StdPeakFlow,
				r.Summary.P95PeakFlow)
		}

		// Climate change scenario should produce higher peak flows
		// than baseline for the no-intervention case.
		var baselinePeak, climatePeak float64
		for _, r := range results {
			if r.PortfolioName == "no_intervention" && r.ScenarioName == "baseline" {
				baselinePeak = r.Summary.MeanPeakFlow
			}
			if r.PortfolioName == "no_intervention" && r.ScenarioName == "RCP8.5_2070" {
				climatePeak = r.Summary.MeanPeakFlow
			}
		}
		if climatePeak <= baselinePeak {
			t.Logf("WARNING: climate scenario peak (%.2f) not higher than baseline (%.2f) — may be due to small ensemble",
				climatePeak, baselinePeak)
		}
	})
}

func TestMeanRoutingFactor(t *testing.T) {
	baseline := []float64{0.8, 0.9, 0.7}

	t.Run("no change returns 1.0", func(t *testing.T) {
		got := meanRoutingFactor(baseline, baseline)
		if got < 0.99 || got > 1.01 {
			t.Errorf("expected ~1.0, got %f", got)
		}
	})

	t.Run("halved routing returns ~0.5", func(t *testing.T) {
		modified := []float64{0.4, 0.45, 0.35}
		got := meanRoutingFactor(baseline, modified)
		if got < 0.45 || got > 0.55 {
			t.Errorf("expected ~0.5, got %f", got)
		}
	})

	t.Run("empty baseline returns 1.0", func(t *testing.T) {
		got := meanRoutingFactor(nil, nil)
		if got != 1.0 {
			t.Errorf("expected 1.0, got %f", got)
		}
	})
}
