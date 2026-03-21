package hydrology

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const FloodMonitoringBaseURL = "https://environment.data.gov.uk/flood-monitoring"

// FloodArea represents an EA flood alert or warning area.
type FloodArea struct {
	ID          string `json:"@id"`
	Label       string `json:"label"`
	Notation    string `json:"notation"`
	Description string `json:"description"`
	County      string `json:"county"`
	RiverOrSea  string `json:"riverOrSea"`
	Lat         float64 `json:"lat"`
	Long        float64 `json:"long"`
}

type floodAreasResponse struct {
	Items []json.RawMessage `json:"items"`
}

// FetchFloodAreas fetches flood areas within a radius of a point.
func FetchFloodAreas(lat, long, distKm float64) ([]FloodArea, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%f", lat))
	params.Set("long", fmt.Sprintf("%f", long))
	params.Set("dist", fmt.Sprintf("%.0f", distKm))
	u := FloodMonitoringBaseURL + "/id/floodAreas?" + params.Encode()

	resp, err := client.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var raw floodAreasResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal flood areas: %w", err)
	}
	areas := make([]FloodArea, 0, len(raw.Items))
	for _, item := range raw.Items {
		var a FloodArea
		if err := json.Unmarshal(item, &a); err != nil {
			continue
		}
		areas = append(areas, a)
	}
	return areas, nil
}

// WriteFloodAreasCSV writes flood areas to a CSV file.
func WriteFloodAreasCSV(areas []FloodArea, dataDir string) (string, error) {
	outDir := filepath.Join(dataDir, "flood_risk")
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}
	outPath := filepath.Join(outDir, "flood_areas.csv")
	f, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	if err := w.Write([]string{"notation", "label", "description", "river_or_sea", "county", "lat", "long"}); err != nil {
		return "", err
	}
	for _, a := range areas {
		if err := w.Write([]string{
			a.Notation,
			a.Label,
			a.Description,
			a.RiverOrSea,
			a.County,
			fmt.Sprintf("%f", a.Lat),
			fmt.Sprintf("%f", a.Long),
		}); err != nil {
			return "", err
		}
	}
	return outPath, nil
}
