package hydrology

// CatchmentConfig defines the stations and parameters for a target catchment.
type CatchmentConfig struct {
	Name            string
	CentreLat       float64
	CentreLong      float64
	SearchRadiusKm  float64
	FlowStations    []StationSpec
	RainfallLat     float64
	RainfallLong    float64
	RainfallRadiusKm float64
}

// StationSpec identifies a station to ingest by reference and name.
type StationSpec struct {
	Notation  string
	Label     string
	River     string
}

// UpperCalderValley returns the catchment configuration for the Upper Calder
// Valley in Yorkshire — the primary study catchment.
func UpperCalderValley() CatchmentConfig {
	return CatchmentConfig{
		Name:           "Upper Calder Valley",
		CentreLat:      53.72,
		CentreLong:     -1.9,
		SearchRadiusKm: 15,
		FlowStations: []StationSpec{
			// River Calder main stem
			{Notation: "3e6fbf1a-f3f3-4dcf-a6fb-84a70cb6d12d", Label: "Elland", River: "River Calder"},
			{Notation: "d7896c1f-b892-4272-b78a-9bb4d5a53c5b", Label: "Dewsbury", River: "River Calder"},
			// Tributaries
			{Notation: "2ab16180-81f3-4694-8236-0ad3610bf012", Label: "Ripponden", River: "River Ryburn"},
			{Notation: "89111009-6453-44ca-a7e6-529df0d9904a", Label: "Colne Bridge", River: "River Colne"},
			{Notation: "c570853f-a563-424f-aeb2-a926b1c30223", Label: "Longroyd Bridge", River: "River Colne"},
			{Notation: "8f2b54dc-b425-4a05-a100-0ea06f4ba248", Label: "Queens Mill", River: "River Holme"},
			{Notation: "280e15ee-d656-40f0-be7a-a144770a6744", Label: "Northorpe", River: "Spen Beck"},
		},
		RainfallLat:      53.72,
		RainfallLong:     -1.9,
		RainfallRadiusKm: 15,
	}
}
