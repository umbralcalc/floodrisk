package hydrology

import (
	"fmt"
	"sort"
	"time"
)

// truncateToDay strips the time component from a time.Time, keeping only the date.
func truncateToDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// AverageCatchmentRainfall computes a daily catchment-average rainfall
// time series from multiple station series. For each day, it averages
// all stations that have a value for that day.
func AverageCatchmentRainfall(series []*TimeSeries) *TimeSeries {
	// Accumulate sums and counts per day.
	type accum struct {
		sum   float64
		count int
	}
	daily := make(map[time.Time]*accum)
	for _, ts := range series {
		for i, t := range ts.Times {
			day := truncateToDay(t)
			a, ok := daily[day]
			if !ok {
				a = &accum{}
				daily[day] = a
			}
			a.sum += ts.Values[i]
			a.count++
		}
	}

	// Sort days and build output.
	days := make([]time.Time, 0, len(daily))
	for d := range daily {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	out := &TimeSeries{Label: "catchment_average"}
	out.Times = make([]time.Time, len(days))
	out.Values = make([]float64, len(days))
	for i, d := range days {
		a := daily[d]
		out.Times[i] = d
		out.Values[i] = a.sum / float64(a.count)
	}
	return out
}

// AlignDaily aligns two daily time series to a common date range,
// returning parallel slices of equal length. Missing rainfall values
// are treated as 0; missing flow values are linearly interpolated
// from neighbours, or dropped if at the boundaries.
func AlignDaily(rainfall, flow *TimeSeries) (rainOut, flowOut []float64, start time.Time, nDays int, err error) {
	// Build day-indexed maps.
	rainMap := make(map[time.Time]float64, len(rainfall.Times))
	for i, t := range rainfall.Times {
		rainMap[truncateToDay(t)] = rainfall.Values[i]
	}
	flowMap := make(map[time.Time]float64, len(flow.Times))
	for i, t := range flow.Times {
		flowMap[truncateToDay(t)] = flow.Values[i]
	}

	// Find common date range.
	rainStart := truncateToDay(rainfall.Times[0])
	rainEnd := truncateToDay(rainfall.Times[len(rainfall.Times)-1])
	flowStart := truncateToDay(flow.Times[0])
	flowEnd := truncateToDay(flow.Times[len(flow.Times)-1])

	if rainStart.After(flowStart) {
		start = rainStart
	} else {
		start = flowStart
	}
	end := rainEnd
	if flowEnd.Before(end) {
		end = flowEnd
	}
	if !start.Before(end) {
		return nil, nil, time.Time{}, 0, fmt.Errorf("no overlapping date range")
	}

	// Iterate over common range day by day.
	oneDay := 24 * time.Hour
	for d := start; !d.After(end); d = d.Add(oneDay) {
		r := rainMap[d] // zero value if missing (0.0 rainfall)
		f, ok := flowMap[d]
		if !ok {
			// Linear interpolation: find nearest before and after.
			f, ok = interpolateFlow(flowMap, d, start, end, oneDay)
			if !ok {
				continue // skip if at boundary with no neighbours
			}
		}
		rainOut = append(rainOut, r)
		flowOut = append(flowOut, f)
	}
	nDays = len(rainOut)
	if nDays == 0 {
		return nil, nil, time.Time{}, 0, fmt.Errorf("no aligned data points")
	}
	return rainOut, flowOut, start, nDays, nil
}

func interpolateFlow(flowMap map[time.Time]float64, day, start, end time.Time, step time.Duration) (float64, bool) {
	// Search backward for nearest value.
	var beforeVal float64
	var beforeDist int
	foundBefore := false
	for d, dist := day.Add(-step), 1; !d.Before(start); d, dist = d.Add(-step), dist+1 {
		if v, ok := flowMap[d]; ok {
			beforeVal = v
			beforeDist = dist
			foundBefore = true
			break
		}
	}
	// Search forward for nearest value.
	var afterVal float64
	var afterDist int
	foundAfter := false
	for d, dist := day.Add(step), 1; !d.After(end); d, dist = d.Add(step), dist+1 {
		if v, ok := flowMap[d]; ok {
			afterVal = v
			afterDist = dist
			foundAfter = true
			break
		}
	}
	if foundBefore && foundAfter {
		total := float64(beforeDist + afterDist)
		return beforeVal*float64(afterDist)/total + afterVal*float64(beforeDist)/total, true
	}
	if foundBefore {
		return beforeVal, true
	}
	if foundAfter {
		return afterVal, true
	}
	return 0, false
}

// AlignedCatchmentData holds aligned daily data for multiple sub-catchments.
type AlignedCatchmentData struct {
	Rainfall map[string][]float64 // sub-catchment name → daily rainfall
	Flow     map[string][]float64 // gauge label → daily flow
	NDays    int
	Start    time.Time
}

// AlignMultiCatchment aligns multiple rainfall and flow time series to a
// common daily date range. Missing rainfall values are treated as 0; missing
// flow values are linearly interpolated.
func AlignMultiCatchment(
	rainfall map[string]*TimeSeries,
	flow map[string]*TimeSeries,
) (*AlignedCatchmentData, error) {
	// Find the global common date range across all series.
	var globalStart, globalEnd time.Time
	first := true
	for _, ts := range rainfall {
		s := truncateToDay(ts.Times[0])
		e := truncateToDay(ts.Times[len(ts.Times)-1])
		if first {
			globalStart, globalEnd = s, e
			first = false
		} else {
			if s.After(globalStart) {
				globalStart = s
			}
			if e.Before(globalEnd) {
				globalEnd = e
			}
		}
	}
	for _, ts := range flow {
		s := truncateToDay(ts.Times[0])
		e := truncateToDay(ts.Times[len(ts.Times)-1])
		if first {
			globalStart, globalEnd = s, e
			first = false
		} else {
			if s.After(globalStart) {
				globalStart = s
			}
			if e.Before(globalEnd) {
				globalEnd = e
			}
		}
	}
	if first || !globalStart.Before(globalEnd) {
		return nil, fmt.Errorf("no overlapping date range across all series")
	}

	oneDay := 24 * time.Hour

	// Build day-indexed maps for each series.
	rainMaps := make(map[string]map[time.Time]float64, len(rainfall))
	for name, ts := range rainfall {
		m := make(map[time.Time]float64, len(ts.Times))
		for i, t := range ts.Times {
			m[truncateToDay(t)] = ts.Values[i]
		}
		rainMaps[name] = m
	}
	flowMaps := make(map[string]map[time.Time]float64, len(flow))
	for name, ts := range flow {
		m := make(map[time.Time]float64, len(ts.Times))
		for i, t := range ts.Times {
			m[truncateToDay(t)] = ts.Values[i]
		}
		flowMaps[name] = m
	}

	// Iterate over common date range.
	result := &AlignedCatchmentData{
		Rainfall: make(map[string][]float64, len(rainfall)),
		Flow:     make(map[string][]float64, len(flow)),
		Start:    globalStart,
	}
	for d := globalStart; !d.After(globalEnd); d = d.Add(oneDay) {
		// Check all flow series have data (or can be interpolated).
		skip := false
		flowVals := make(map[string]float64, len(flow))
		for name, fm := range flowMaps {
			if v, ok := fm[d]; ok {
				flowVals[name] = v
			} else {
				v, ok := interpolateFlow(fm, d, globalStart, globalEnd, oneDay)
				if !ok {
					skip = true
					break
				}
				flowVals[name] = v
			}
		}
		if skip {
			continue
		}

		for name, rm := range rainMaps {
			result.Rainfall[name] = append(result.Rainfall[name], rm[d])
		}
		for name, v := range flowVals {
			result.Flow[name] = append(result.Flow[name], v)
		}
	}

	result.NDays = len(result.Flow[firstKey(result.Flow)])
	if result.NDays == 0 {
		return nil, fmt.Errorf("no aligned data points")
	}
	return result, nil
}

func firstKey(m map[string][]float64) string {
	for k := range m {
		return k
	}
	return ""
}

// ToStorageData converts a flat slice of values into the [][]float64
// format required by general.FromStorageIteration.Data. Each value
// is wrapped in a single-element slice.
func ToStorageData(values []float64) [][]float64 {
	data := make([][]float64, len(values))
	for i, v := range values {
		data[i] = []float64{v}
	}
	return data
}

// SplitTimeSeries splits a TimeSeries at the given date. All observations
// strictly before splitDate go into "before", all others into "after".
func SplitTimeSeries(ts *TimeSeries, splitDate time.Time) (before, after *TimeSeries) {
	before = &TimeSeries{Label: ts.Label}
	after = &TimeSeries{Label: ts.Label}
	split := truncateToDay(splitDate)
	for i, t := range ts.Times {
		day := truncateToDay(t)
		if day.Before(split) {
			before.Times = append(before.Times, ts.Times[i])
			before.Values = append(before.Values, ts.Values[i])
		} else {
			after.Times = append(after.Times, ts.Times[i])
			after.Values = append(after.Values, ts.Values[i])
		}
	}
	return before, after
}

// SplitAligned splits parallel aligned slices (rainfall, flow) at a given
// day index, returning train and test portions for both.
func SplitAligned(rain, flow []float64, splitIdx int) (rainTrain, flowTrain, rainTest, flowTest []float64) {
	rainTrain = rain[:splitIdx]
	flowTrain = flow[:splitIdx]
	rainTest = rain[splitIdx:]
	flowTest = flow[splitIdx:]
	return
}

// DayIndex returns the index into an aligned daily series corresponding
// to the given target date, where day 0 is alignStart.
func DayIndex(alignStart, target time.Time) int {
	s := truncateToDay(alignStart)
	t := truncateToDay(target)
	return int(t.Sub(s).Hours() / 24)
}
