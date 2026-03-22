package catchment

import (
	"math/rand/v2"
	"testing"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

func TestCalibrate(t *testing.T) {
	t.Run(
		"random search improves over defaults",
		func(t *testing.T) {
			rainfallSeries, err := hydrology.LoadAllRainfallSeries("../../dat")
			if err != nil {
				t.Skipf("rainfall data not available: %v", err)
			}
			if len(rainfallSeries) == 0 {
				t.Skip("no rainfall series found")
			}

			avgRainfall := hydrology.AverageCatchmentRainfall(rainfallSeries)

			ellandFlow, err := hydrology.LoadTimeSeries(
				"../../dat/flow/elland_daily_flow.csv", "Elland")
			if err != nil {
				t.Skipf("Elland flow data not available: %v", err)
			}

			rainAligned, flowAligned, _, _, err := hydrology.AlignDaily(
				avgRainfall, ellandFlow)
			if err != nil {
				t.Fatalf("alignment failed: %v", err)
			}

			rainfallData := hydrology.ToStorageData(rainAligned)

			rng := rand.New(rand.NewPCG(42, 99))
			result := Calibrate(rainfallData, flowAligned, DefaultBounds(), 500, 30, rng)

			t.Logf("Best calibration result (500 trials):")
			t.Logf("  NSE:          %.4f", result.NSE)
			t.Logf("  RMSE:         %.4f m³/s", result.RMSE)
			t.Logf("  Peak bias:    %.4f", result.PeakBias)
			t.Logf("  Volume error: %.4f", result.VolumeError)
			t.Logf("  Params:")
			t.Logf("    field_capacity:      %.1f mm", result.Params["field_capacity"][0])
			t.Logf("    drainage_rate:       %.4f", result.Params["drainage_rate"][0])
			t.Logf("    et_rate:             %.2f mm/day", result.Params["et_rate"][0])
			t.Logf("    runoff_shape:        %.4f", result.Params["runoff_shape"][0])
			t.Logf("    fast_recession_rate: %.4f", result.Params["fast_recession_rate"][0])
			t.Logf("    slow_recession_rate: %.4f", result.Params["slow_recession_rate"][0])
			t.Logf("    catchment_area:      %.1f km²", result.Params["catchment_area_km2"][0])

			// Default params give NSE=0.108. Calibration should beat that.
			if result.NSE <= 0.108 {
				t.Errorf("calibration NSE=%.4f did not improve over default (0.108)", result.NSE)
			}
		},
	)
}
