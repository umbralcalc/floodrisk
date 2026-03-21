package hydrology

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	BaseURL = "https://environment.data.gov.uk/hydrology"
)

// Client is an HTTP client for the EA Hydrology API.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		baseURL:    BaseURL,
	}
}

// LabelledRef is a JSON-LD reference with an optional label, used for fields
// like status, valueStatistic, and observationType in the EA API.
type LabelledRef struct {
	ID    string `json:"@id"`
	Label string `json:"label"`
}

// Station represents an EA hydrological monitoring station.
type Station struct {
	ID            string       `json:"@id"`
	Label         string       `json:"label"`
	Notation      string       `json:"notation"`
	WiskiID       string       `json:"wiskiID"`
	RiverName     string       `json:"riverName"`
	CatchmentArea float64      `json:"catchmentArea"`
	Lat           float64      `json:"lat"`
	Long          float64      `json:"long"`
	Easting       int          `json:"easting"`
	Northing      int          `json:"northing"`
	DateOpened    string       `json:"dateOpened"`
	DateClosed    string       `json:"dateClosed"`
	Status        []LabelledRef `json:"status"`
	NrfaStationID string       `json:"nrfaStationID"`
}

// IsActive returns true if the station is not closed.
func (s *Station) IsActive() bool {
	for _, st := range s.Status {
		if st.Label == "Closed" {
			return false
		}
	}
	return true
}

// Measure represents a measurement type available at a station.
type Measure struct {
	ID              string      `json:"@id"`
	Label           string      `json:"label"`
	Notation        string      `json:"notation"`
	Parameter       string      `json:"parameter"`
	ParameterName   string      `json:"parameterName"`
	Period          int         `json:"period"`
	PeriodName      string      `json:"periodName"`
	ValueType       string      `json:"valueType"`
	ValueStatistic  LabelledRef `json:"valueStatistic"`
	ObservationType LabelledRef `json:"observationType"`
}

// Reading represents a single data reading.
type Reading struct {
	Date         string      `json:"date"`
	DateTime     string      `json:"dateTime"`
	Value        float64     `json:"value"`
	Quality      string      `json:"quality"`
	Completeness string      `json:"completeness"`
	Measure      LabelledRef `json:"measure"`
}

type stationsResponse struct {
	Items []json.RawMessage `json:"items"`
}

type readingsResponse struct {
	Items []Reading `json:"items"`
}

func (c *Client) get(path string, params url.Values) ([]byte, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	resp, err := c.httpClient.Get(u)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// FindStations searches for stations by proximity (lat/long/dist in km)
// and observed property (e.g. "waterFlow", "rainfall").
func (c *Client) FindStations(lat, long, distKm float64, observedProperty string) ([]Station, error) {
	params := url.Values{}
	params.Set("lat", fmt.Sprintf("%f", lat))
	params.Set("long", fmt.Sprintf("%f", long))
	params.Set("dist", fmt.Sprintf("%.0f", distKm))
	if observedProperty != "" {
		params.Set("observedProperty", observedProperty)
	}
	body, err := c.get("/id/stations", params)
	if err != nil {
		return nil, err
	}
	var resp stationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal stations: %w", err)
	}
	stations := make([]Station, 0, len(resp.Items))
	for _, raw := range resp.Items {
		var s Station
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		stations = append(stations, s)
	}
	return stations, nil
}

// SearchStations searches for stations by name.
func (c *Client) SearchStations(search, observedProperty string) ([]Station, error) {
	params := url.Values{}
	params.Set("search", search)
	if observedProperty != "" {
		params.Set("observedProperty", observedProperty)
	}
	body, err := c.get("/id/stations", params)
	if err != nil {
		return nil, err
	}
	var resp stationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal stations: %w", err)
	}
	stations := make([]Station, 0, len(resp.Items))
	for _, raw := range resp.Items {
		var s Station
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		stations = append(stations, s)
	}
	return stations, nil
}

// GetStationMeasures fetches the measures available for a station.
func (c *Client) GetStationMeasures(stationNotation string) ([]Measure, error) {
	body, err := c.get("/id/stations/"+stationNotation+"/measures", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Items []Measure `json:"items"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal measures: %w", err)
	}
	return resp.Items, nil
}

// GetReadings fetches readings for a measure within a date range.
// The measure notation should be the full measure ID path component.
// Dates are in YYYY-MM-DD format.
func (c *Client) GetReadings(measureNotation, minDate, maxDate string, limit int) ([]Reading, error) {
	params := url.Values{}
	if minDate != "" {
		params.Set("mineq-date", minDate)
	}
	if maxDate != "" {
		params.Set("max-date", maxDate)
	}
	if limit > 0 {
		params.Set("_limit", fmt.Sprintf("%d", limit))
	}
	body, err := c.get("/id/measures/"+measureNotation+"/readings", params)
	if err != nil {
		return nil, err
	}
	var resp readingsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal readings: %w", err)
	}
	return resp.Items, nil
}
