package hydrology

import (
	"math"
	"testing"

	"gonum.org/v1/gonum/floats/scalar"
)

func TestNashSutcliffe(t *testing.T) {
	t.Run("perfect match gives NSE=1", func(t *testing.T) {
		obs := []float64{1, 2, 3, 4, 5}
		sim := []float64{1, 2, 3, 4, 5}
		nse := NashSutcliffe(obs, sim)
		if !scalar.EqualWithinAbsOrRel(nse, 1.0, 1e-10, 1e-10) {
			t.Errorf("expected NSE=1.0, got %f", nse)
		}
	})
	t.Run("mean predictor gives NSE=0", func(t *testing.T) {
		obs := []float64{1, 2, 3, 4, 5}
		mean := 3.0
		sim := []float64{mean, mean, mean, mean, mean}
		nse := NashSutcliffe(obs, sim)
		if !scalar.EqualWithinAbsOrRel(nse, 0.0, 1e-10, 1e-10) {
			t.Errorf("expected NSE=0.0, got %f", nse)
		}
	})
	t.Run("worse than mean gives negative NSE", func(t *testing.T) {
		obs := []float64{1, 2, 3, 4, 5}
		sim := []float64{5, 4, 3, 2, 1}
		nse := NashSutcliffe(obs, sim)
		if nse >= 0 {
			t.Errorf("expected negative NSE, got %f", nse)
		}
	})
}

func TestRMSE(t *testing.T) {
	t.Run("perfect match gives RMSE=0", func(t *testing.T) {
		obs := []float64{1, 2, 3}
		sim := []float64{1, 2, 3}
		r := RMSE(obs, sim)
		if !scalar.EqualWithinAbsOrRel(r, 0.0, 1e-10, 1e-10) {
			t.Errorf("expected RMSE=0.0, got %f", r)
		}
	})
	t.Run("known offset", func(t *testing.T) {
		obs := []float64{0, 0, 0}
		sim := []float64{1, 1, 1}
		r := RMSE(obs, sim)
		if !scalar.EqualWithinAbsOrRel(r, 1.0, 1e-10, 1e-10) {
			t.Errorf("expected RMSE=1.0, got %f", r)
		}
	})
}

func TestPeakFlowBias(t *testing.T) {
	t.Run("no bias", func(t *testing.T) {
		obs := []float64{1, 5, 3}
		sim := []float64{2, 5, 4}
		bias := PeakFlowBias(obs, sim)
		if !scalar.EqualWithinAbsOrRel(bias, 0.0, 1e-10, 1e-10) {
			t.Errorf("expected bias=0, got %f", bias)
		}
	})
	t.Run("over-prediction", func(t *testing.T) {
		obs := []float64{1, 10, 3}
		sim := []float64{2, 15, 4}
		bias := PeakFlowBias(obs, sim)
		if !scalar.EqualWithinAbsOrRel(bias, 0.5, 1e-10, 1e-10) {
			t.Errorf("expected bias=0.5, got %f", bias)
		}
	})
}

func TestVolumeError(t *testing.T) {
	t.Run("no error", func(t *testing.T) {
		obs := []float64{1, 2, 3}
		sim := []float64{3, 2, 1}
		ve := VolumeError(obs, sim)
		if !scalar.EqualWithinAbsOrRel(ve, 0.0, 1e-10, 1e-10) {
			t.Errorf("expected volume error=0, got %f", ve)
		}
	})
	t.Run("double volume", func(t *testing.T) {
		obs := []float64{1, 2, 3}
		sim := []float64{2, 4, 6}
		ve := VolumeError(obs, sim)
		if !scalar.EqualWithinAbsOrRel(ve, 1.0, 1e-10, 1e-10) {
			t.Errorf("expected volume error=1.0, got %f", ve)
		}
	})
	t.Run("zero observed gives NaN", func(t *testing.T) {
		obs := []float64{0, 0, 0}
		sim := []float64{1, 2, 3}
		ve := VolumeError(obs, sim)
		if !math.IsNaN(ve) {
			t.Errorf("expected NaN, got %f", ve)
		}
	})
}
