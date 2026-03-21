package hydrology

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestIngestFlowData(t *testing.T) {
	t.Run("ingest daily flow for Elland", func(t *testing.T) {
		dir := t.TempDir()
		ing := NewIngester(dir)
		station := StationSpec{
			Notation: "3e6fbf1a-f3f3-4dcf-a6fb-84a70cb6d12d",
			Label:    "Elland",
			River:    "River Calder",
		}
		path, err := ing.IngestFlowData(station, "2024-01-01", "2024-01-10")
		if err != nil {
			t.Fatalf("IngestFlowData: %v", err)
		}
		// Verify CSV was written.
		f, err := os.Open(path)
		if err != nil {
			t.Fatalf("open CSV: %v", err)
		}
		defer f.Close()
		r := csv.NewReader(f)
		records, err := r.ReadAll()
		if err != nil {
			t.Fatalf("read CSV: %v", err)
		}
		// Header + at least 1 data row.
		if len(records) < 2 {
			t.Fatalf("expected at least 2 rows (header + data), got %d", len(records))
		}
		// Check header.
		if records[0][0] != "datetime" || records[0][1] != "value" {
			t.Errorf("unexpected header: %v", records[0])
		}
		t.Logf("wrote %d data rows to %s", len(records)-1, filepath.Base(path))
	})
}

func TestIngestRainfallStations(t *testing.T) {
	t.Run("ingest rainfall for Upper Calder Valley", func(t *testing.T) {
		dir := t.TempDir()
		ing := NewIngester(dir)
		cfg := UpperCalderValley()
		paths, err := ing.IngestRainfallStations(cfg, "2024-01-01", "2024-01-10")
		if err != nil {
			t.Fatalf("IngestRainfallStations: %v", err)
		}
		if len(paths) == 0 {
			t.Fatal("expected at least one rainfall file")
		}
		t.Logf("downloaded %d rainfall station files", len(paths))
		for _, p := range paths {
			t.Logf("  %s", filepath.Base(p))
		}
	})
}
