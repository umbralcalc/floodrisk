package catchment

import (
	"testing"

	"gonum.org/v1/gonum/floats/scalar"
)

func TestMultiCatchment(t *testing.T) {
	t.Run("constant rainfall sums flows", func(t *testing.T) {
		// With constant rainfall across two sub-catchments and K=1.0 (pass-through),
		// the total routed flow should equal the sum of individual flows.
		nSteps := 50
		rainfall := 10.0

		// Build rainfall data for two sub-catchments.
		rainfallA := make([][]float64, nSteps+1)
		rainfallB := make([][]float64, nSteps+1)
		for i := range rainfallA {
			rainfallA[i] = []float64{rainfall}
			rainfallB[i] = []float64{rainfall}
		}

		params := map[string][]float64{
			"field_capacity":      {200.0},
			"drainage_rate":       {0.05},
			"et_rate":             {1.5},
			"runoff_shape":        {2.0},
			"fast_recession_rate": {0.7},
			"slow_recession_rate": {0.1},
		}

		cfg := MultiCatchmentConfig{
			SubCatchments:  []string{"a", "b"},
			CatchmentAreas: map[string]float64{"a": 100.0, "b": 50.0},
			RoutingCoeffs:  []float64{1.0, 1.0}, // pass-through
		}

		rainfallData := map[string][][]float64{
			"a": rainfallA,
			"b": rainfallB,
		}

		multiFlow := RunMultiCatchmentModel(rainfallData, params, cfg, nSteps)

		// Run individual models for comparison.
		paramsA := make(map[string][]float64, len(params))
		for k, v := range params {
			paramsA[k] = v
		}
		paramsA["catchment_area_km2"] = []float64{100.0}
		paramsA["upstream_partition"] = []float64{0}
		singleA := RunModel(rainfallA, paramsA, nSteps)

		paramsB := make(map[string][]float64, len(params))
		for k, v := range params {
			paramsB[k] = v
		}
		paramsB["catchment_area_km2"] = []float64{50.0}
		paramsB["upstream_partition"] = []float64{0}
		singleB := RunModel(rainfallB, paramsB, nSteps)

		// Routing adds a 1-step lag (reads previous step's runoff state),
		// so multi[i] corresponds to singleA[i-1] + singleB[i-1].
		for i := 1; i < len(multiFlow) && i <= len(singleA) && i <= len(singleB); i++ {
			expected := singleA[i-1] + singleB[i-1]
			if !scalar.EqualWithinAbsOrRel(multiFlow[i], expected, 0.01, 0.01) {
				t.Errorf("step %d: multi=%.4f, want %.4f (A[%d]=%.4f + B[%d]=%.4f)",
					i, multiFlow[i], expected, i-1, singleA[i-1], i-1, singleB[i-1])
			}
		}
	})

	t.Run("routing attenuates flow", func(t *testing.T) {
		// With K<1, routed flow should be less than pass-through at early steps.
		nSteps := 50
		rainfall := make([][]float64, nSteps+1)
		for i := range rainfall {
			rainfall[i] = []float64{10.0}
		}

		params := map[string][]float64{
			"field_capacity":      {200.0},
			"drainage_rate":       {0.05},
			"et_rate":             {1.5},
			"runoff_shape":        {2.0},
			"fast_recession_rate": {0.7},
			"slow_recession_rate": {0.1},
		}

		rainfallData := map[string][][]float64{"a": rainfall}

		passThrough := RunMultiCatchmentModel(rainfallData, params, MultiCatchmentConfig{
			SubCatchments:  []string{"a"},
			CatchmentAreas: map[string]float64{"a": 100.0},
			RoutingCoeffs:  []float64{1.0},
		}, nSteps)

		attenuated := RunMultiCatchmentModel(rainfallData, params, MultiCatchmentConfig{
			SubCatchments:  []string{"a"},
			CatchmentAreas: map[string]float64{"a": 100.0},
			RoutingCoeffs:  []float64{0.3},
		}, nSteps)

		// Early attenuated flow should be less than pass-through (skip step 0
		// which is zero for both due to routing lag).
		if attenuated[2] >= passThrough[2] {
			t.Errorf("expected attenuated early flow (%.4f) < pass-through (%.4f)",
				attenuated[2], passThrough[2])
		}

		// At steady state they should converge.
		last := len(attenuated) - 1
		if !scalar.EqualWithinAbsOrRel(attenuated[last], passThrough[last], 0.5, 0.05) {
			t.Errorf("converged attenuated=%.4f, pass-through=%.4f",
				attenuated[last], passThrough[last])
		}
	})
}
