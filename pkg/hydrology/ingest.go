package hydrology

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ingester fetches data from the EA Hydrology API and writes it to CSV files.
type Ingester struct {
	Client  *Client
	DataDir string
}

func NewIngester(dataDir string) *Ingester {
	return &Ingester{
		Client:  NewClient(),
		DataDir: dataDir,
	}
}

// IngestFlowData fetches daily mean flow readings for a station and writes to CSV.
// It discovers measures automatically and downloads qualified daily mean flow.
func (ing *Ingester) IngestFlowData(station StationSpec, minDate, maxDate string) (string, error) {
	measures, err := ing.Client.GetStationMeasures(station.Notation)
	if err != nil {
		return "", fmt.Errorf("get measures for %s: %w", station.Label, err)
	}

	// Find the daily mean flow measure (qualified preferred).
	var measureNotation string
	for _, m := range measures {
		isFlow := m.Parameter == "flow" || m.ParameterName == "Flow"
		isDaily := m.Period == 86400
		isMean := m.ValueType == "mean" || m.ValueStatistic.Label == "mean"
		if isFlow && isDaily && isMean {
			measureNotation = m.Notation
			if m.ObservationType.Label == "Qualified" {
				break // prefer qualified
			}
		}
	}

	// Fallback: any daily flow measure.
	if measureNotation == "" {
		for _, m := range measures {
			isFlow := m.Parameter == "flow" || m.ParameterName == "Flow"
			if isFlow && m.Period == 86400 {
				measureNotation = m.Notation
				break
			}
		}
	}

	if measureNotation == "" {
		return "", fmt.Errorf("no flow measure found for station %s", station.Label)
	}

	readings, err := ing.Client.GetReadings(measureNotation, minDate, maxDate, 0)
	if err != nil {
		return "", fmt.Errorf("get readings for %s: %w", station.Label, err)
	}

	// Sort by date ascending.
	sort.Slice(readings, func(i, j int) bool {
		return readings[i].DateTime < readings[j].DateTime
	})

	outDir := filepath.Join(ing.DataDir, "flow")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, sanitise(station.Label)+"_daily_flow.csv")
	return outPath, writeReadingsCSV(outPath, readings, station.Label, station.River)
}

// IngestSubDailyFlowData fetches 15-min or instantaneous flow readings.
func (ing *Ingester) IngestSubDailyFlowData(station StationSpec, minDate, maxDate string) (string, error) {
	measures, err := ing.Client.GetStationMeasures(station.Notation)
	if err != nil {
		return "", fmt.Errorf("get measures for %s: %w", station.Label, err)
	}

	// Find the highest-frequency flow measure (900s = 15-min preferred).
	var measureNotation string
	var bestPeriod int = 999999
	for _, m := range measures {
		isFlow := m.Parameter == "flow" || m.ParameterName == "Flow"
		if isFlow && m.Period > 0 && m.Period < bestPeriod {
			measureNotation = m.Notation
			bestPeriod = m.Period
		}
	}

	if measureNotation == "" {
		return "", fmt.Errorf("no sub-daily flow measure found for station %s", station.Label)
	}

	readings, err := ing.Client.GetReadings(measureNotation, minDate, maxDate, 0)
	if err != nil {
		return "", fmt.Errorf("get readings for %s: %w", station.Label, err)
	}

	sort.Slice(readings, func(i, j int) bool {
		return readings[i].DateTime < readings[j].DateTime
	})

	outDir := filepath.Join(ing.DataDir, "flow")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, sanitise(station.Label)+"_subdaily_flow.csv")
	return outPath, writeReadingsCSV(outPath, readings, station.Label, station.River)
}

// IngestRainfallStations discovers and ingests daily rainfall data for all
// stations within the catchment's rainfall search radius.
func (ing *Ingester) IngestRainfallStations(cfg CatchmentConfig, minDate, maxDate string) ([]string, error) {
	stations, err := ing.Client.FindStations(cfg.RainfallLat, cfg.RainfallLong, cfg.RainfallRadiusKm, "rainfall")
	if err != nil {
		return nil, fmt.Errorf("find rainfall stations: %w", err)
	}

	var paths []string
	for _, s := range stations {
		measures, err := ing.Client.GetStationMeasures(s.Notation)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: skip rainfall station %s: %v\n", s.Label, err)
			continue
		}

		// Find daily rainfall total.
		var measureNotation string
		for _, m := range measures {
			isRain := m.Parameter == "rainfall" || m.ParameterName == "Rainfall"
			if isRain && m.Period == 86400 {
				measureNotation = m.Notation
				break
			}
		}
		if measureNotation == "" {
			continue
		}

		readings, err := ing.Client.GetReadings(measureNotation, minDate, maxDate, 0)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: skip rainfall readings for %s: %v\n", s.Label, err)
			continue
		}

		sort.Slice(readings, func(i, j int) bool {
			return readings[i].DateTime < readings[j].DateTime
		})

		outDir := filepath.Join(ing.DataDir, "rainfall")
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return paths, err
		}
		outPath := filepath.Join(outDir, sanitise(s.Label)+"_daily_rainfall.csv")
		if err := writeReadingsCSV(outPath, readings, s.Label, ""); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: write rainfall CSV for %s: %v\n", s.Label, err)
			continue
		}
		paths = append(paths, outPath)
	}
	return paths, nil
}

func writeReadingsCSV(path string, readings []Reading, stationLabel, river string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"datetime", "value", "station", "river"}); err != nil {
		return err
	}
	for _, r := range readings {
		if err := w.Write([]string{
			r.DateTime,
			fmt.Sprintf("%g", r.Value),
			stationLabel,
			river,
		}); err != nil {
			return err
		}
	}
	return nil
}

func sanitise(name string) string {
	r := strings.NewReplacer(" ", "_", "/", "_", "\\", "_")
	return strings.ToLower(r.Replace(name))
}
