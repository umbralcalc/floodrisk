package hydrology

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TimeSeries is a time-ordered sequence of (time, value) pairs.
type TimeSeries struct {
	Label  string
	Times  []time.Time
	Values []float64
}

// LoadTimeSeries reads a CSV file produced by the ingester into a TimeSeries.
func LoadTimeSeries(path, label string) (*TimeSeries, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("no data rows in %s", path)
	}

	ts := &TimeSeries{Label: label}
	for _, row := range records[1:] {
		t, err := parseFlexTime(row[0])
		if err != nil {
			continue
		}
		v, err := strconv.ParseFloat(row[1], 64)
		if err != nil || math.IsNaN(v) {
			continue
		}
		ts.Times = append(ts.Times, t)
		ts.Values = append(ts.Values, v)
	}
	return ts, nil
}

func parseFlexTime(s string) (time.Time, error) {
	for _, layout := range []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse time %q", s)
}

// Stats holds basic summary statistics.
type Stats struct {
	N      int
	Min    float64
	Max    float64
	Mean   float64
	StdDev float64
	P95    float64
	P99    float64
}

// Summary computes basic statistics for a TimeSeries.
func (ts *TimeSeries) Summary() Stats {
	if len(ts.Values) == 0 {
		return Stats{}
	}
	sorted := make([]float64, len(ts.Values))
	copy(sorted, ts.Values)
	sort.Float64s(sorted)

	n := len(sorted)
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)

	var ssq float64
	for _, v := range sorted {
		d := v - mean
		ssq += d * d
	}
	stddev := math.Sqrt(ssq / float64(n))

	return Stats{
		N:      n,
		Min:    sorted[0],
		Max:    sorted[n-1],
		Mean:   mean,
		StdDev: stddev,
		P95:    percentile(sorted, 0.95),
		P99:    percentile(sorted, 0.99),
	}
}

func percentile(sorted []float64, p float64) float64 {
	idx := p * float64(len(sorted)-1)
	lo := int(math.Floor(idx))
	hi := int(math.Ceil(idx))
	if lo == hi || hi >= len(sorted) {
		return sorted[lo]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

// FloodEvent represents a period where flow exceeded a threshold.
type FloodEvent struct {
	Station   string
	StartTime time.Time
	EndTime   time.Time
	PeakTime  time.Time
	PeakFlow  float64
	Duration  time.Duration
}

// DetectFloodEvents identifies periods where daily flow exceeds the given
// threshold (e.g. the 95th percentile). Consecutive exceedance days are
// grouped into a single event.
func (ts *TimeSeries) DetectFloodEvents(threshold float64) []FloodEvent {
	var events []FloodEvent
	var current *FloodEvent

	for i, v := range ts.Values {
		if v >= threshold {
			if current == nil {
				current = &FloodEvent{
					Station:   ts.Label,
					StartTime: ts.Times[i],
					PeakFlow:  v,
					PeakTime:  ts.Times[i],
				}
			}
			if v > current.PeakFlow {
				current.PeakFlow = v
				current.PeakTime = ts.Times[i]
			}
			current.EndTime = ts.Times[i]
		} else if current != nil {
			current.Duration = current.EndTime.Sub(current.StartTime) + 24*time.Hour
			events = append(events, *current)
			current = nil
		}
	}
	if current != nil {
		current.Duration = current.EndTime.Sub(current.StartTime) + 24*time.Hour
		events = append(events, *current)
	}
	return events
}

// FloodFrequency returns empirical flood frequency data using the Weibull
// plotting position: return period T = (n+1)/rank for annual maxima.
type FloodFrequencyPoint struct {
	Year         int
	AnnualMax    float64
	Rank         int
	ReturnPeriod float64
}

// AnnualMaxima extracts the maximum value per calendar year.
func (ts *TimeSeries) AnnualMaxima() []FloodFrequencyPoint {
	yearMax := make(map[int]float64)
	for i, t := range ts.Times {
		y := t.Year()
		if ts.Values[i] > yearMax[y] {
			yearMax[y] = ts.Values[i]
		}
	}

	points := make([]FloodFrequencyPoint, 0, len(yearMax))
	for y, m := range yearMax {
		points = append(points, FloodFrequencyPoint{Year: y, AnnualMax: m})
	}
	// Sort by annual max descending for ranking.
	sort.Slice(points, func(i, j int) bool {
		return points[i].AnnualMax > points[j].AnnualMax
	})

	n := len(points)
	for i := range points {
		points[i].Rank = i + 1
		points[i].ReturnPeriod = float64(n+1) / float64(i+1)
	}
	return points
}

// PrintAnalysisReport prints a summary analysis to stdout.
func PrintAnalysisReport(flowSeries []*TimeSeries, rainfallSeries []*TimeSeries) {
	fmt.Println("=== EXPLORATORY ANALYSIS: Upper Calder Valley ===")
	fmt.Println()

	// Flow station summaries.
	fmt.Println("--- Flow Station Summaries (daily mean flow, m³/s) ---")
	fmt.Printf("%-20s %6s %8s %8s %8s %8s %8s %8s\n",
		"Station", "N", "Min", "Max", "Mean", "StdDev", "P95", "P99")
	for _, ts := range flowSeries {
		s := ts.Summary()
		fmt.Printf("%-20s %6d %8.2f %8.2f %8.2f %8.2f %8.2f %8.2f\n",
			ts.Label, s.N, s.Min, s.Max, s.Mean, s.StdDev, s.P95, s.P99)
	}

	// Flood events for each station.
	fmt.Println("\n--- Major Flood Events (above P95 threshold) ---")
	for _, ts := range flowSeries {
		s := ts.Summary()
		events := ts.DetectFloodEvents(s.P95)
		// Show top 10 by peak flow.
		sort.Slice(events, func(i, j int) bool {
			return events[i].PeakFlow > events[j].PeakFlow
		})
		n := len(events)
		if n > 10 {
			n = 10
		}
		fmt.Printf("\n%s — %d events total, top %d:\n", ts.Label, len(events), n)
		fmt.Printf("  %-12s  %-12s  %10s  %8s\n", "Peak Date", "Start", "Peak(m³/s)", "Duration")
		for _, e := range events[:n] {
			fmt.Printf("  %-12s  %-12s  %10.2f  %8s\n",
				e.PeakTime.Format("2006-01-02"),
				e.StartTime.Format("2006-01-02"),
				e.PeakFlow,
				formatDuration(e.Duration))
		}
	}

	// Flood frequency curves.
	fmt.Println("\n--- Empirical Flood Frequency (Annual Maxima, Weibull plotting position) ---")
	for _, ts := range flowSeries {
		points := ts.AnnualMaxima()
		fmt.Printf("\n%s:\n", ts.Label)
		fmt.Printf("  %6s  %10s  %6s  %12s\n", "Year", "AMax(m³/s)", "Rank", "ReturnPeriod")
		for _, p := range points {
			fmt.Printf("  %6d  %10.2f  %6d  %12.1f yr\n",
				p.Year, p.AnnualMax, p.Rank, p.ReturnPeriod)
		}
	}

	// Rainfall summaries.
	if len(rainfallSeries) > 0 {
		fmt.Println("\n--- Rainfall Station Summaries (daily total, mm) ---")
		fmt.Printf("%-25s %6s %8s %8s %8s %8s\n",
			"Station", "N", "Max", "Mean", "P95", "P99")
		for _, ts := range rainfallSeries {
			s := ts.Summary()
			fmt.Printf("%-25s %6d %8.2f %8.2f %8.2f %8.2f\n",
				truncate(ts.Label, 25), s.N, s.Max, s.Mean, s.P95, s.P99)
		}
	}
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days <= 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// LoadAllFlowSeries loads all flow CSV files matching the catchment config.
func LoadAllFlowSeries(dataDir string, cfg CatchmentConfig) ([]*TimeSeries, error) {
	var series []*TimeSeries
	for _, st := range cfg.FlowStations {
		path := dataDir + "/flow/" + sanitise(st.Label) + "_daily_flow.csv"
		ts, err := LoadTimeSeries(path, st.Label)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: skip flow %s: %v\n", st.Label, err)
			continue
		}
		series = append(series, ts)
	}
	return series, nil
}

// LoadAllRainfallSeries loads all rainfall CSV files from the data directory.
func LoadAllRainfallSeries(dataDir string) ([]*TimeSeries, error) {
	entries, err := os.ReadDir(dataDir + "/rainfall")
	if err != nil {
		return nil, err
	}
	var series []*TimeSeries
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".csv") {
			continue
		}
		label := strings.TrimSuffix(e.Name(), "_daily_rainfall.csv")
		label = strings.ReplaceAll(label, "_", " ")
		ts, err := LoadTimeSeries(dataDir+"/rainfall/"+e.Name(), label)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: skip rainfall %s: %v\n", label, err)
			continue
		}
		series = append(series, ts)
	}
	return series, nil
}
