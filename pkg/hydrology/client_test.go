package hydrology

import (
	"testing"
)

func TestFindFlowStations(t *testing.T) {
	t.Run("find flow stations near Upper Calder Valley", func(t *testing.T) {
		client := NewClient()
		stations, err := client.FindStations(53.72, -1.9, 15, "waterFlow")
		if err != nil {
			t.Fatalf("FindStations: %v", err)
		}
		if len(stations) == 0 {
			t.Fatal("expected at least one flow station")
		}
		t.Logf("found %d flow stations:", len(stations))
		for _, s := range stations {
			t.Logf("  %s (notation=%s, river=%s, catchment=%.1f km²)",
				s.Label, s.Notation, s.RiverName, s.CatchmentArea)
		}
	})
}

func TestFindRainfallStations(t *testing.T) {
	t.Run("find rainfall stations near Upper Calder Valley", func(t *testing.T) {
		client := NewClient()
		stations, err := client.FindStations(53.72, -1.9, 15, "rainfall")
		if err != nil {
			t.Fatalf("FindStations: %v", err)
		}
		if len(stations) == 0 {
			t.Fatal("expected at least one rainfall station")
		}
		t.Logf("found %d rainfall stations:", len(stations))
		for _, s := range stations {
			t.Logf("  %s (notation=%s, lat=%.4f, long=%.4f)",
				s.Label, s.Notation, s.Lat, s.Long)
		}
	})
}

func TestGetStationMeasures(t *testing.T) {
	t.Run("get measures for Elland station", func(t *testing.T) {
		client := NewClient()
		// Elland station notation
		measures, err := client.GetStationMeasures("3e6fbf1a-f3f3-4dcf-a6fb-84a70cb6d12d")
		if err != nil {
			t.Fatalf("GetStationMeasures: %v", err)
		}
		if len(measures) == 0 {
			t.Fatal("expected at least one measure")
		}
		t.Logf("found %d measures for Elland:", len(measures))
		for _, m := range measures {
			t.Logf("  %s (notation=%s, param=%s, period=%d, stat=%s)",
				m.Label, m.Notation, m.ParameterName, m.Period, m.ValueStatistic)
		}
	})
}

func TestGetReadings(t *testing.T) {
	t.Run("get recent flow readings for Elland", func(t *testing.T) {
		client := NewClient()
		// First get the measures to find a valid notation.
		measures, err := client.GetStationMeasures("3e6fbf1a-f3f3-4dcf-a6fb-84a70cb6d12d")
		if err != nil {
			t.Fatalf("GetStationMeasures: %v", err)
		}
		if len(measures) == 0 {
			t.Fatal("no measures found")
		}
		// Use the first measure.
		notation := measures[0].Notation
		readings, err := client.GetReadings(notation, "2024-01-01", "2024-01-10", 10)
		if err != nil {
			t.Fatalf("GetReadings: %v", err)
		}
		t.Logf("got %d readings (limit 10) for measure %s", len(readings), notation)
		for _, r := range readings {
			t.Logf("  %s = %g", r.DateTime, r.Value)
		}
	})
}
