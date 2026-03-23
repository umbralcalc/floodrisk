package hydrology

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

// SubCatchment defines a sub-catchment within the study area.
type SubCatchment struct {
	Name         string
	FlowStation  StationSpec
	GaugeLat     float64
	GaugeLong    float64
	AreaKm2      float64 // approximate catchment area
}

// RainfallStationMeta holds location metadata for a rainfall station.
type RainfallStationMeta struct {
	Label string
	Lat   float64
	Long  float64
}

// UpperCalderSubCatchments returns the 5 sub-catchment definitions for
// the Upper Calder Valley. Gauge coordinates and areas are from EA metadata.
// The "Upper Calder" sub-catchment represents the residual main-stem area
// not covered by the four tributaries.
func UpperCalderSubCatchments() []SubCatchment {
	cfg := UpperCalderValley()
	// Elland total catchment area is approximately 340 km².
	// Colne Bridge (245 km²) includes the Holme sub-catchment, so the
	// Colne-only area is ~195 km². Spen Beck drains below Elland so is
	// excluded from the Elland total; we include it for Dewsbury validation.
	const ellandTotalArea = 340.0
	const ryburnArea = 25.0
	const colneArea = 195.0 // Colne Bridge minus Holme
	const holmeArea = 50.0
	const spenArea = 0.0 // below Elland, excluded from total
	calderResidual := ellandTotalArea - ryburnArea - colneArea - holmeArea - spenArea
	if calderResidual < 0 {
		calderResidual = 10.0 // safety floor
	}

	return []SubCatchment{
		{
			Name:        "ryburn",
			FlowStation: cfg.FlowStations[2], // Ripponden
			GaugeLat:    53.6740,
			GaugeLong:   -1.9540,
			AreaKm2:     ryburnArea,
		},
		{
			Name:        "colne",
			FlowStation: cfg.FlowStations[3], // Colne Bridge
			GaugeLat:    53.6450,
			GaugeLong:   -1.7780,
			AreaKm2:     colneArea,
		},
		{
			Name:        "holme",
			FlowStation: cfg.FlowStations[5], // Queens Mill
			GaugeLat:    53.5920,
			GaugeLong:   -1.7850,
			AreaKm2:     holmeArea,
		},
		{
			Name:        "spen",
			FlowStation: cfg.FlowStations[6], // Northorpe
			GaugeLat:    53.7170,
			GaugeLong:   -1.6690,
			AreaKm2:     spenArea, // below Elland; used for Dewsbury validation
		},
		{
			Name:        "upper_calder",
			FlowStation: cfg.FlowStations[0], // Elland (residual)
			GaugeLat:    53.6880,
			GaugeLong:   -1.8370,
			AreaKm2:     calderResidual,
		},
	}
}

// SubCatchmentNames returns the ordered list of sub-catchment names.
func SubCatchmentNames() []string {
	subs := UpperCalderSubCatchments()
	names := make([]string, len(subs))
	for i, s := range subs {
		names[i] = s.Name
	}
	return names
}

// SubCatchmentAreas returns a map of sub-catchment name to area in km².
func SubCatchmentAreas() map[string]float64 {
	subs := UpperCalderSubCatchments()
	areas := make(map[string]float64, len(subs))
	for _, s := range subs {
		areas[s.Name] = s.AreaKm2
	}
	return areas
}

// AssignRainfallStations assigns each rainfall station to the nearest
// sub-catchment gauge by Haversine distance.
func AssignRainfallStations(
	stations []RainfallStationMeta,
	subcatchments []SubCatchment,
) map[string][]string {
	assignment := make(map[string][]string, len(subcatchments))
	for _, s := range stations {
		bestDist := math.MaxFloat64
		bestName := subcatchments[0].Name
		for _, sc := range subcatchments {
			d := haversine(s.Lat, s.Long, sc.GaugeLat, sc.GaugeLong)
			if d < bestDist {
				bestDist = d
				bestName = sc.Name
			}
		}
		assignment[bestName] = append(assignment[bestName], s.Label)
	}
	return assignment
}

// haversine returns the distance in km between two lat/lon points.
func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKm = 6371.0
	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKm * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// SubCatchmentRainfall groups rainfall time series by sub-catchment
// assignment and computes per-group averages.
func SubCatchmentRainfall(
	allSeries []*TimeSeries,
	assignment map[string][]string,
) (map[string]*TimeSeries, error) {
	// Index series by label for lookup.
	byLabel := make(map[string]*TimeSeries, len(allSeries))
	for _, ts := range allSeries {
		byLabel[ts.Label] = ts
	}

	result := make(map[string]*TimeSeries, len(assignment))
	for scName, labels := range assignment {
		var group []*TimeSeries
		for _, label := range labels {
			if ts, ok := byLabel[label]; ok {
				group = append(group, ts)
			}
		}
		if len(group) == 0 {
			return nil, fmt.Errorf("no rainfall series found for sub-catchment %s", scName)
		}
		avg := AverageCatchmentRainfall(group)
		avg.Label = scName + "_rainfall"
		result[scName] = avg
	}
	return result, nil
}

// LoadRainfallStationMeta reads a rainfall station metadata CSV file
// with columns: label, lat, long.
func LoadRainfallStationMeta(path string) ([]RainfallStationMeta, error) {
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

	var stations []RainfallStationMeta
	for _, row := range records[1:] {
		if len(row) < 3 {
			continue
		}
		lat, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			continue
		}
		lon, err := strconv.ParseFloat(row[2], 64)
		if err != nil {
			continue
		}
		// Normalize label to lowercase to match LoadAllRainfallSeries
		// which derives labels from sanitised filenames.
		stations = append(stations, RainfallStationMeta{
			Label: strings.ToLower(row[0]),
			Lat:   lat,
			Long:  lon,
		})
	}
	return stations, nil
}
