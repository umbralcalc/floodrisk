package hydrology

import (
	"testing"
	"time"

	"gonum.org/v1/gonum/floats/scalar"
)

func TestUpperCalderSubCatchments(t *testing.T) {
	subs := UpperCalderSubCatchments()
	if len(subs) != 5 {
		t.Fatalf("expected 5 sub-catchments, got %d", len(subs))
	}

	// Verify all areas are non-negative and that active catchment areas
	// (excluding Spen Beck which is below Elland) sum to ~340 km².
	totalArea := 0.0
	for _, sc := range subs {
		if sc.AreaKm2 < 0 {
			t.Errorf("sub-catchment %s has negative area: %.1f", sc.Name, sc.AreaKm2)
		}
		totalArea += sc.AreaKm2
	}
	if !scalar.EqualWithinAbsOrRel(totalArea, 340.0, 5.0, 0.02) {
		t.Errorf("total area = %.1f, want ~340 km²", totalArea)
	}
}

func TestAssignRainfallStations(t *testing.T) {
	subs := UpperCalderSubCatchments()

	// Create synthetic stations at known locations near each gauge.
	stations := []RainfallStationMeta{
		{Label: "near_ripponden", Lat: 53.674, Long: -1.955},
		{Label: "near_colne", Lat: 53.645, Long: -1.778},
		{Label: "near_holme", Lat: 53.592, Long: -1.785},
		{Label: "near_spen", Lat: 53.717, Long: -1.669},
		{Label: "near_calder", Lat: 53.688, Long: -1.837},
	}

	assignment := AssignRainfallStations(stations, subs)

	// Each station should be assigned to the nearest sub-catchment.
	expected := map[string]string{
		"near_ripponden": "ryburn",
		"near_colne":     "colne",
		"near_holme":     "holme",
		"near_spen":      "spen",
		"near_calder":    "upper_calder",
	}

	for label, wantSC := range expected {
		found := false
		for sc, labels := range assignment {
			for _, l := range labels {
				if l == label {
					if sc != wantSC {
						t.Errorf("station %s assigned to %s, want %s", label, sc, wantSC)
					}
					found = true
				}
			}
		}
		if !found {
			t.Errorf("station %s not found in any assignment", label)
		}
	}
}

func TestSubCatchmentRainfall(t *testing.T) {
	// Create minimal time series.
	ts1 := &TimeSeries{Label: "station_a", Times: parseTimes("2020-01-01", "2020-01-02"), Values: []float64{5.0, 10.0}}
	ts2 := &TimeSeries{Label: "station_b", Times: parseTimes("2020-01-01", "2020-01-02"), Values: []float64{3.0, 8.0}}
	ts3 := &TimeSeries{Label: "station_c", Times: parseTimes("2020-01-01", "2020-01-02"), Values: []float64{7.0, 2.0}}

	assignment := map[string][]string{
		"group1": {"station_a", "station_b"},
		"group2": {"station_c"},
	}

	result, err := SubCatchmentRainfall([]*TimeSeries{ts1, ts2, ts3}, assignment)
	if err != nil {
		t.Fatal(err)
	}

	// group1 average: (5+3)/2=4, (10+8)/2=9
	g1 := result["group1"]
	if !scalar.EqualWithinAbsOrRel(g1.Values[0], 4.0, 0.01, 0.01) {
		t.Errorf("group1 day 1 = %.2f, want 4.0", g1.Values[0])
	}
	if !scalar.EqualWithinAbsOrRel(g1.Values[1], 9.0, 0.01, 0.01) {
		t.Errorf("group1 day 2 = %.2f, want 9.0", g1.Values[1])
	}

	// group2: just station_c
	g2 := result["group2"]
	if !scalar.EqualWithinAbsOrRel(g2.Values[0], 7.0, 0.01, 0.01) {
		t.Errorf("group2 day 1 = %.2f, want 7.0", g2.Values[0])
	}
}

func TestAlignMultiCatchment(t *testing.T) {
	rainfall := map[string]*TimeSeries{
		"sc1": {Label: "sc1", Times: parseTimes("2020-01-01", "2020-01-02", "2020-01-03"), Values: []float64{1, 2, 3}},
		"sc2": {Label: "sc2", Times: parseTimes("2020-01-01", "2020-01-02", "2020-01-03"), Values: []float64{4, 5, 6}},
	}
	flow := map[string]*TimeSeries{
		"gauge1": {Label: "gauge1", Times: parseTimes("2020-01-01", "2020-01-02", "2020-01-03"), Values: []float64{10, 20, 30}},
	}

	aligned, err := AlignMultiCatchment(rainfall, flow)
	if err != nil {
		t.Fatal(err)
	}
	if aligned.NDays != 3 {
		t.Fatalf("NDays = %d, want 3", aligned.NDays)
	}
	if len(aligned.Rainfall) != 2 {
		t.Fatalf("expected 2 rainfall series, got %d", len(aligned.Rainfall))
	}
	if len(aligned.Flow) != 1 {
		t.Fatalf("expected 1 flow series, got %d", len(aligned.Flow))
	}
	// All series should have NDays values.
	for name, vals := range aligned.Rainfall {
		if len(vals) != aligned.NDays {
			t.Errorf("rainfall %s has %d values, want %d", name, len(vals), aligned.NDays)
		}
	}
}

// parseTimes is a test helper to create time slices from date strings.
func parseTimes(dates ...string) []time.Time {
	times := make([]time.Time, len(dates))
	for i, d := range dates {
		t, _ := time.Parse("2006-01-02", d)
		times[i] = t
	}
	return times
}
