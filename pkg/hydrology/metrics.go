package hydrology

import "math"

// NashSutcliffe computes the Nash-Sutcliffe efficiency coefficient.
// NSE = 1 means perfect match, 0 means the model is as good as predicting
// the mean, and negative values indicate worse-than-mean performance.
func NashSutcliffe(observed, simulated []float64) float64 {
	n := len(observed)
	mean := 0.0
	for _, v := range observed {
		mean += v
	}
	mean /= float64(n)

	var ssRes, ssTot float64
	for i := 0; i < n; i++ {
		diff := observed[i] - simulated[i]
		ssRes += diff * diff
		devObs := observed[i] - mean
		ssTot += devObs * devObs
	}
	if ssTot == 0 {
		return math.NaN()
	}
	return 1.0 - ssRes/ssTot
}

// RMSE computes the root mean square error between observed and simulated.
func RMSE(observed, simulated []float64) float64 {
	n := len(observed)
	var sum float64
	for i := 0; i < n; i++ {
		diff := observed[i] - simulated[i]
		sum += diff * diff
	}
	return math.Sqrt(sum / float64(n))
}

// PeakFlowBias computes the relative bias in peak flow:
// (max(sim) - max(obs)) / max(obs). Positive means over-prediction.
func PeakFlowBias(observed, simulated []float64) float64 {
	maxObs := observed[0]
	for _, v := range observed[1:] {
		if v > maxObs {
			maxObs = v
		}
	}
	maxSim := simulated[0]
	for _, v := range simulated[1:] {
		if v > maxSim {
			maxSim = v
		}
	}
	if maxObs == 0 {
		return math.NaN()
	}
	return (maxSim - maxObs) / maxObs
}

// VolumeError computes the relative total volume error:
// (sum(sim) - sum(obs)) / sum(obs). Positive means the model
// generates too much total flow.
func VolumeError(observed, simulated []float64) float64 {
	var sumObs, sumSim float64
	for i := range observed {
		sumObs += observed[i]
		sumSim += simulated[i]
	}
	if sumObs == 0 {
		return math.NaN()
	}
	return (sumSim - sumObs) / sumObs
}
