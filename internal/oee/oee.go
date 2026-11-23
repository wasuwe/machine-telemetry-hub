// Package oee calculates Overall Equipment Effectiveness.
//
//	OEE = Availability × Performance × Quality
//
//	Availability = running time / planned time
//	Performance  = (ideal cycle time × total parts) / running time
//	Quality      = good parts / total parts
package oee

import "time"

// Counters are the raw numbers OEE is computed from.
type Counters struct {
	Planned    time.Duration
	Running    time.Duration
	IdealCycle time.Duration
	Total      int
	Good       int
}

// Result holds each OEE factor as a ratio between 0 and 1.
type Result struct {
	Availability float64 `json:"availability"`
	Performance  float64 `json:"performance"`
	Quality      float64 `json:"quality"`
	OEE          float64 `json:"oee"`
}

// Calculate returns the OEE factors for c. Factors with no data are 0.
func Calculate(c Counters) Result {
	var r Result
	if c.Planned > 0 {
		r.Availability = clamp(float64(c.Running) / float64(c.Planned))
	}
	if c.Running > 0 {
		r.Performance = clamp(float64(c.IdealCycle) * float64(c.Total) / float64(c.Running))
	}
	if c.Total > 0 {
		r.Quality = clamp(float64(c.Good) / float64(c.Total))
	}
	r.OEE = r.Availability * r.Performance * r.Quality
	return r
}

// clamp keeps v in [0, 1]. Performance can briefly exceed 1 because a part
// is counted only when its cycle ends, so we cap it.
func clamp(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
