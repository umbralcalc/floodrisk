package catchment

import (
	"math/rand/v2"

	"github.com/umbralcalc/floodrisk/pkg/hydrology"
)

// HoldoutResult holds metrics for both training and holdout periods.
type HoldoutResult struct {
	TrainResult CalibrationResult
	TestNSE     float64
	TestRMSE    float64
	TestPeakBias    float64
	TestVolumeError float64
}

// ValidateHoldout calibrates the model on the training portion of the
// data (rain/flow[:splitIdx]) and evaluates on the holdout portion
// (rain/flow[splitIdx:]). It returns metrics for both periods.
func ValidateHoldout(
	rainAligned, flowAligned []float64,
	splitIdx int,
	bounds ParamBounds,
	nTrials int,
	spinUp int,
	rng *rand.Rand,
) HoldoutResult {
	// Split data into train and test.
	rainTrain, flowTrain, rainTest, flowTest := hydrology.SplitAligned(
		rainAligned, flowAligned, splitIdx)

	// Calibrate on training period.
	trainData := hydrology.ToStorageData(rainTrain)
	trainResult := Calibrate(trainData, flowTrain, bounds, nTrials, spinUp, rng)

	// Evaluate best params on holdout period.
	testData := hydrology.ToStorageData(rainTest)
	nTestSteps := len(testData) - 1
	simFlow := RunModel(testData, trainResult.Params, nTestSteps)

	// Align: simFlow starts at step 1, flowTest starts at step 0.
	// Use a shorter spin-up for test (model needs to warm up from
	// fresh initial conditions on the test segment).
	testSpinUp := spinUp
	obs := flowTest[1+testSpinUp:]
	sim := simFlow[testSpinUp:]
	n := len(obs)
	if len(sim) < n {
		n = len(sim)
	}
	obs = obs[:n]
	sim = sim[:n]

	return HoldoutResult{
		TrainResult:     trainResult,
		TestNSE:         hydrology.NashSutcliffe(obs, sim),
		TestRMSE:        hydrology.RMSE(obs, sim),
		TestPeakBias:    hydrology.PeakFlowBias(obs, sim),
		TestVolumeError: hydrology.VolumeError(obs, sim),
	}
}

// EvaluateFloodEvents runs the model with given params over the full
// aligned data and compares simulated vs observed peaks for detected
// flood events. Returns per-event peak flow comparisons.
type FloodEventComparison struct {
	StartDay    int     // day index of event start
	EndDay      int     // day index of event end
	ObsPeak     float64 // observed peak flow (m³/s)
	SimPeak     float64 // simulated peak flow (m³/s)
	PeakBias    float64 // (sim - obs) / obs
}

// EvaluateFloodEvents detects flood events in the observed flow and
// compares the simulated peak within each event window.
func EvaluateFloodEvents(
	obsFlow, simFlow []float64,
	threshold float64,
	spinUp int,
) []FloodEventComparison {
	// Detect events from observed flow (after spin-up).
	var events []FloodEventComparison
	inEvent := false
	var startDay, endDay int
	var obsPeak, simPeak float64

	for i := spinUp; i < len(obsFlow) && i < len(simFlow); i++ {
		if obsFlow[i] >= threshold {
			if !inEvent {
				inEvent = true
				startDay = i
				obsPeak = obsFlow[i]
				simPeak = simFlow[i]
			}
			if obsFlow[i] > obsPeak {
				obsPeak = obsFlow[i]
			}
			if simFlow[i] > simPeak {
				simPeak = simFlow[i]
			}
			endDay = i
		} else if inEvent {
			bias := 0.0
			if obsPeak > 0 {
				bias = (simPeak - obsPeak) / obsPeak
			}
			events = append(events, FloodEventComparison{
				StartDay: startDay,
				EndDay:   endDay,
				ObsPeak:  obsPeak,
				SimPeak:  simPeak,
				PeakBias: bias,
			})
			inEvent = false
		}
	}
	// Close final event if still open.
	if inEvent {
		bias := 0.0
		if obsPeak > 0 {
			bias = (simPeak - obsPeak) / obsPeak
		}
		events = append(events, FloodEventComparison{
			StartDay: startDay,
			EndDay:   endDay,
			ObsPeak:  obsPeak,
			SimPeak:  simPeak,
			PeakBias: bias,
		})
	}
	return events
}
