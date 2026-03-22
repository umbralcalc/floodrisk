package catchment

import (
	"math"

	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// NumModelParams is the number of calibration parameters when using
// the vectorized "model_params" format.
const NumModelParams = 6

// ModelParamsFromMap converts individual named params to the vectorized
// model_params format: [field_capacity, drainage_rate, et_rate,
// runoff_coefficient, recession_rate, catchment_area_km2].
func ModelParamsFromMap(m map[string][]float64) []float64 {
	return []float64{
		m["field_capacity"][0],
		m["drainage_rate"][0],
		m["et_rate"][0],
		m["runoff_coefficient"][0],
		m["recession_rate"][0],
		m["catchment_area_km2"][0],
	}
}

// RainfallRunoffIteration implements a lumped conceptual rainfall-runoff
// model for a single sub-catchment. It reads rainfall (mm/day) from an
// upstream partition and produces river flow (m³/s) through a soil
// moisture bucket with quick and slow flow pathways.
//
// State vector: [soil_moisture_mm, flow_m3s]
//
// Parameters can be provided in two ways:
//
// Named params (original):
//   - field_capacity, drainage_rate, et_rate, runoff_coefficient,
//     recession_rate, catchment_area_km2
//
// Vectorized (for SBI wiring via params_from_upstream):
//   - model_params: [field_capacity, drainage_rate, et_rate,
//     runoff_coefficient, recession_rate, catchment_area_km2]
//
// Additional:
//   - upstream_partition: partition index providing rainfall
type RainfallRunoffIteration struct {
	upstreamPartitionIndex int
}

func (r *RainfallRunoffIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {
	r.upstreamPartitionIndex = int(
		settings.Iterations[partitionIndex].Params.Map["upstream_partition"][0],
	)
}

func (r *RainfallRunoffIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	// Read parameters — vectorized or individual named params.
	var fieldCapacity, drainageRate, etRate, runoffCoeff, recessionRate, catchmentArea float64
	if mp, ok := params.GetOk("model_params"); ok {
		fieldCapacity = mp[0]
		drainageRate = mp[1]
		etRate = mp[2]
		runoffCoeff = mp[3]
		recessionRate = mp[4]
		catchmentArea = mp[5]
	} else {
		fieldCapacity = params.Map["field_capacity"][0]
		drainageRate = params.Map["drainage_rate"][0]
		etRate = params.Map["et_rate"][0]
		runoffCoeff = params.Map["runoff_coefficient"][0]
		recessionRate = params.Map["recession_rate"][0]
		catchmentArea = params.Map["catchment_area_km2"][0]
	}

	// Time step in days.
	dt := timestepsHistory.NextIncrement

	// Get rainfall from upstream partition (mm/day).
	rainfall := stateHistories[r.upstreamPartitionIndex].Values.At(0, 0)

	// Previous state.
	current := stateHistories[partitionIndex]
	soilMoisture := current.Values.At(0, 0)
	prevFlow := current.Values.At(0, 1)

	// --- Soil moisture accounting ---

	// Net rainfall after ET losses (can't go negative).
	netRainfall := math.Max(rainfall-etRate, 0.0) * dt

	// Add net rainfall to soil store.
	soilMoisture += netRainfall

	// Excess over field capacity becomes surface runoff.
	excess := math.Max(soilMoisture-fieldCapacity, 0.0)
	soilMoisture -= excess

	// Slow drainage from soil store.
	drainage := drainageRate * soilMoisture * dt
	soilMoisture -= drainage
	soilMoisture = math.Max(soilMoisture, 0.0)

	// --- Flow generation ---

	// Quick runoff from excess + baseflow from drainage (mm over timestep).
	totalRunoffMM := runoffCoeff*excess + drainage

	// Convert mm → m³/s:  (mm * km² * 1e6 m²/km² * 1e-3 m/mm) / (86400 s/day * dt)
	// Simplifies to: mm * km² * 1000 / (86400 * dt)
	flowContribution := totalRunoffMM * catchmentArea * 1000.0 / (86400.0 * dt)

	// Recession filter: blend new contribution with previous flow.
	flow := recessionRate*flowContribution + (1.0-recessionRate)*prevFlow

	return []float64{soilMoisture, flow}
}
