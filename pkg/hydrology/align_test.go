package hydrology

import (
	"testing"
	"time"

	"gonum.org/v1/gonum/floats/scalar"
)

func TestAverageCatchmentRainfall(t *testing.T) {
	t.Run("averages multiple stations per day", func(t *testing.T) {
		day1 := time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC)
		day2 := time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)
		s1 := &TimeSeries{
			Label:  "station1",
			Times:  []time.Time{day1, day2},
			Values: []float64{10.0, 20.0},
		}
		s2 := &TimeSeries{
			Label:  "station2",
			Times:  []time.Time{day1, day2},
			Values: []float64{30.0, 40.0},
		}
		avg := AverageCatchmentRainfall([]*TimeSeries{s1, s2})
		if len(avg.Values) != 2 {
			t.Fatalf("expected 2 values, got %d", len(avg.Values))
		}
		if !scalar.EqualWithinAbsOrRel(avg.Values[0], 20.0, 1e-10, 1e-10) {
			t.Errorf("day1 average: expected 20.0, got %f", avg.Values[0])
		}
		if !scalar.EqualWithinAbsOrRel(avg.Values[1], 30.0, 1e-10, 1e-10) {
			t.Errorf("day2 average: expected 30.0, got %f", avg.Values[1])
		}
	})
	t.Run("handles stations with different date ranges", func(t *testing.T) {
		day1 := time.Date(2020, 1, 1, 9, 0, 0, 0, time.UTC)
		day2 := time.Date(2020, 1, 2, 9, 0, 0, 0, time.UTC)
		s1 := &TimeSeries{
			Label:  "station1",
			Times:  []time.Time{day1, day2},
			Values: []float64{10.0, 20.0},
		}
		s2 := &TimeSeries{
			Label:  "station2",
			Times:  []time.Time{day1}, // only day1
			Values: []float64{30.0},
		}
		avg := AverageCatchmentRainfall([]*TimeSeries{s1, s2})
		if len(avg.Values) != 2 {
			t.Fatalf("expected 2 values, got %d", len(avg.Values))
		}
		// day1: (10+30)/2 = 20
		if !scalar.EqualWithinAbsOrRel(avg.Values[0], 20.0, 1e-10, 1e-10) {
			t.Errorf("day1: expected 20.0, got %f", avg.Values[0])
		}
		// day2: only station1 = 20
		if !scalar.EqualWithinAbsOrRel(avg.Values[1], 20.0, 1e-10, 1e-10) {
			t.Errorf("day2: expected 20.0, got %f", avg.Values[1])
		}
	})
}

func TestAlignDaily(t *testing.T) {
	t.Run("aligns overlapping series", func(t *testing.T) {
		// Rainfall: days 1-5, Flow: days 3-7 → overlap: days 3-5
		rain := &TimeSeries{
			Label: "rain",
			Times: []time.Time{
				time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 5, 0, 0, 0, 0, time.UTC),
			},
			Values: []float64{1, 2, 3, 4, 5},
		}
		flow := &TimeSeries{
			Label: "flow",
			Times: []time.Time{
				time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 5, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 6, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 7, 0, 0, 0, 0, time.UTC),
			},
			Values: []float64{10, 20, 30, 40, 50},
		}
		r, f, start, n, err := AlignDaily(rain, flow)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected 3 days, got %d", n)
		}
		expectedStart := time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC)
		if !start.Equal(expectedStart) {
			t.Errorf("expected start %v, got %v", expectedStart, start)
		}
		// Rain values for days 3,4,5
		if !scalar.EqualWithinAbsOrRel(r[0], 3.0, 1e-10, 1e-10) {
			t.Errorf("rain[0]: expected 3.0, got %f", r[0])
		}
		// Flow values for days 3,4,5
		if !scalar.EqualWithinAbsOrRel(f[0], 10.0, 1e-10, 1e-10) {
			t.Errorf("flow[0]: expected 10.0, got %f", f[0])
		}
		if !scalar.EqualWithinAbsOrRel(f[2], 30.0, 1e-10, 1e-10) {
			t.Errorf("flow[2]: expected 30.0, got %f", f[2])
		}
	})
	t.Run("interpolates missing flow values", func(t *testing.T) {
		rain := &TimeSeries{
			Label: "rain",
			Times: []time.Time{
				time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
				time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
			},
			Values: []float64{5, 5, 5},
		}
		flow := &TimeSeries{
			Label: "flow",
			Times: []time.Time{
				time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
				// day 2 missing
				time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC),
			},
			Values: []float64{10, 30},
		}
		_, f, _, n, err := AlignDaily(rain, flow)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n != 3 {
			t.Fatalf("expected 3 days, got %d", n)
		}
		// Day 2 flow should be interpolated: (10+30)/2 = 20
		if !scalar.EqualWithinAbsOrRel(f[1], 20.0, 1e-10, 1e-10) {
			t.Errorf("interpolated flow: expected 20.0, got %f", f[1])
		}
	})
}

func TestToStorageData(t *testing.T) {
	t.Run("wraps values in single-element slices", func(t *testing.T) {
		vals := []float64{1.0, 2.0, 3.0}
		data := ToStorageData(vals)
		if len(data) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(data))
		}
		for i, row := range data {
			if len(row) != 1 {
				t.Errorf("row %d: expected 1 element, got %d", i, len(row))
			}
			if row[0] != vals[i] {
				t.Errorf("row %d: expected %f, got %f", i, vals[i], row[0])
			}
		}
	})
}
