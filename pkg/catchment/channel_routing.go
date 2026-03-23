package catchment

import (
	"github.com/umbralcalc/stochadex/pkg/simulator"
)

// ChannelRoutingIteration aggregates flow from multiple upstream
// sub-catchment partitions using linear reservoir routing.
//
// For each upstream partition i, the routed flow is:
//
//	routed_i(t) = K_i * upstream_i(t) + (1 - K_i) * routed_i(t-1)
//
// where K_i is the routing coefficient (0–1). K=1 means instant
// pass-through (no lag); lower K produces more attenuation/delay.
//
// State vector: [total_routed_flow, routed_0, routed_1, ..., routed_N-1]
// State width = 1 + N where N = number of upstream partitions.
//
// Parameters:
//   - upstream_partitions: []float64 of partition indices to read total_flow from
//   - routing_coefficients: []float64 of K values, one per upstream (optional; defaults to 1.0)
type ChannelRoutingIteration struct {
	upstreamIndices []int
}

func (c *ChannelRoutingIteration) Configure(
	partitionIndex int,
	settings *simulator.Settings,
) {
	ups := settings.Iterations[partitionIndex].Params.Map["upstream_partitions"]
	c.upstreamIndices = make([]int, len(ups))
	for i, v := range ups {
		c.upstreamIndices[i] = int(v)
	}
}

func (c *ChannelRoutingIteration) Iterate(
	params *simulator.Params,
	partitionIndex int,
	stateHistories []*simulator.StateHistory,
	timestepsHistory *simulator.CumulativeTimestepsHistory,
) []float64 {
	n := len(c.upstreamIndices)
	coeffs, hasCoeffs := params.GetOk("routing_coefficients")

	current := stateHistories[partitionIndex]
	state := make([]float64, 1+n)
	totalFlow := 0.0

	for i, idx := range c.upstreamIndices {
		// Read total_flow (index 1) from upstream rainfall-runoff partition.
		upstreamFlow := stateHistories[idx].Values.At(0, 1)

		k := 1.0 // default: pass-through
		if hasCoeffs && i < len(coeffs) {
			k = coeffs[i]
		}

		prevRouted := current.Values.At(0, 1+i)
		routed := k*upstreamFlow + (1.0-k)*prevRouted

		state[1+i] = routed
		totalFlow += routed
	}
	state[0] = totalFlow
	return state
}
