package catchment

import (
	"math"
	"math/rand/v2"
	"testing"

	"gonum.org/v1/gonum/floats"
)

func TestSampleEffectiveness(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	priors := DefaultInterventionPriors()

	t.Run("leaky dams reduce routing coefficient", func(t *testing.T) {
		intervention := Intervention{
			Type:         LeakyDams,
			SubCatchment: "Ryburn",
			Scale:        5,
		}
		eff := SampleEffectiveness(intervention, priors, rng)

		if eff.RoutingCoefficientReduction >= 1.0 {
			t.Errorf("expected routing reduction < 1.0, got %f", eff.RoutingCoefficientReduction)
		}
		if eff.RoutingCoefficientReduction <= 0.0 {
			t.Errorf("expected routing reduction > 0.0, got %f", eff.RoutingCoefficientReduction)
		}
		if eff.FieldCapacityIncreaseMM != 0.0 {
			t.Errorf("leaky dams should not affect field capacity, got %f", eff.FieldCapacityIncreaseMM)
		}
	})

	t.Run("woodland increases field capacity and ET", func(t *testing.T) {
		intervention := Intervention{
			Type:         WoodlandPlanting,
			SubCatchment: "Colne",
			Scale:        30, // 30 ha = 3 units of 10ha
		}
		eff := SampleEffectiveness(intervention, priors, rng)

		if eff.FieldCapacityIncreaseMM <= 0 {
			t.Errorf("expected field capacity increase > 0, got %f", eff.FieldCapacityIncreaseMM)
		}
		if eff.ETRateIncreaseMM <= 0 {
			t.Errorf("expected ET rate increase > 0, got %f", eff.ETRateIncreaseMM)
		}
		if math.Abs(eff.RoutingCoefficientReduction-1.0) > 1e-10 {
			t.Errorf("woodland should not affect routing, got %f", eff.RoutingCoefficientReduction)
		}
	})

	t.Run("floodplain reconnection reduces routing", func(t *testing.T) {
		intervention := Intervention{
			Type:         FloodplainReconnection,
			SubCatchment: "Colne",
			Scale:        2,
		}
		eff := SampleEffectiveness(intervention, priors, rng)

		if eff.RoutingCoefficientReduction >= 1.0 {
			t.Errorf("expected routing reduction < 1.0, got %f", eff.RoutingCoefficientReduction)
		}
	})

	t.Run("peat restoration increases field capacity only", func(t *testing.T) {
		intervention := Intervention{
			Type:         PeatRestoration,
			SubCatchment: "Upper Calder",
			Scale:        20, // 20 ha = 2 units
		}
		eff := SampleEffectiveness(intervention, priors, rng)

		if eff.FieldCapacityIncreaseMM <= 0 {
			t.Errorf("expected field capacity increase > 0, got %f", eff.FieldCapacityIncreaseMM)
		}
		if eff.ETRateIncreaseMM != 0 {
			t.Errorf("peat restoration should not affect ET, got %f", eff.ETRateIncreaseMM)
		}
	})
}

func TestApplyPortfolio(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 99))
	priors := DefaultInterventionPriors()
	subCatchments := []string{"Ryburn", "Colne", "Upper Calder"}

	baseParams := map[string][]float64{
		"field_capacity":      {200.0},
		"drainage_rate":       {0.05},
		"et_rate":             {2.0},
		"runoff_shape":        {1.5},
		"fast_recession_rate": {0.7},
		"slow_recession_rate": {0.1},
		"catchment_area_km2":  {340.0},
		"upstream_partition":  {0},
	}
	baseRouting := []float64{0.8, 0.9, 0.7}

	t.Run("no intervention returns copies of originals", func(t *testing.T) {
		portfolio := Portfolio{Name: "none"}
		params, routing := ApplyPortfolio(
			portfolio, baseParams, baseRouting,
			subCatchments, priors, rng,
		)

		if math.Abs(params["field_capacity"][0]-200.0) > 1e-10 {
			t.Errorf("field capacity changed without intervention: %f", params["field_capacity"][0])
		}
		if !floats.EqualApprox(routing, baseRouting, 1e-10) {
			t.Errorf("routing changed without intervention: %v", routing)
		}

		// Verify it's a copy, not a reference.
		params["field_capacity"][0] = 999.0
		if baseParams["field_capacity"][0] == 999.0 {
			t.Error("ApplyPortfolio returned reference, not copy")
		}
	})

	t.Run("mixed portfolio modifies params and routing", func(t *testing.T) {
		portfolio := Portfolio{
			Name: "mixed",
			Interventions: []Intervention{
				{Type: LeakyDams, SubCatchment: "Ryburn", Scale: 5},
				{Type: WoodlandPlanting, SubCatchment: "Colne", Scale: 20},
				{Type: PeatRestoration, SubCatchment: "Upper Calder", Scale: 20},
			},
		}
		params, routing := ApplyPortfolio(
			portfolio, baseParams, baseRouting,
			subCatchments, priors, rng,
		)

		// Field capacity should increase (woodland + peat).
		if params["field_capacity"][0] <= baseParams["field_capacity"][0] {
			t.Errorf("expected field capacity increase, got %f (base %f)",
				params["field_capacity"][0], baseParams["field_capacity"][0])
		}

		// Ryburn routing should decrease (leaky dams).
		if routing[0] >= baseRouting[0] {
			t.Errorf("expected Ryburn routing decrease, got %f (base %f)",
				routing[0], baseRouting[0])
		}

		// Colne and Upper Calder routing should be unchanged
		// (no routing interventions there).
		if math.Abs(routing[1]-baseRouting[1]) > 1e-10 {
			t.Errorf("Colne routing changed unexpectedly: %f (base %f)",
				routing[1], baseRouting[1])
		}
	})
}

func TestInterventionTypeString(t *testing.T) {
	tests := []struct {
		typ  InterventionType
		want string
	}{
		{LeakyDams, "leaky_dams"},
		{WoodlandPlanting, "woodland_planting"},
		{FloodplainReconnection, "floodplain_reconnection"},
		{PeatRestoration, "peat_restoration"},
	}
	for _, tc := range tests {
		if got := tc.typ.String(); got != tc.want {
			t.Errorf("InterventionType(%d).String() = %q, want %q", tc.typ, got, tc.want)
		}
	}
}
