package oee

import (
	"math"
	"testing"
	"time"
)

func TestCalculate(t *testing.T) {
	tests := []struct {
		name string
		in   Counters
		want Result
	}{
		{
			name: "no data",
			in:   Counters{},
			want: Result{},
		},
		{
			name: "perfect machine",
			in:   Counters{Planned: 100 * time.Second, Running: 100 * time.Second, IdealCycle: time.Second, Total: 100, Good: 100},
			want: Result{Availability: 1, Performance: 1, Quality: 1, OEE: 1},
		},
		{
			name: "typical shift",
			// ran 90% of the time, at 80% speed, 75% good parts
			in:   Counters{Planned: 100 * time.Second, Running: 90 * time.Second, IdealCycle: time.Second, Total: 72, Good: 54},
			want: Result{Availability: 0.9, Performance: 0.8, Quality: 0.75, OEE: 0.54},
		},
		{
			name: "performance is capped at 100%",
			in:   Counters{Planned: 10 * time.Second, Running: 10 * time.Second, IdealCycle: time.Second, Total: 11, Good: 11},
			want: Result{Availability: 1, Performance: 1, Quality: 1, OEE: 1},
		},
		{
			name: "never ran",
			in:   Counters{Planned: 60 * time.Second, IdealCycle: time.Second},
			want: Result{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Calculate(tt.in)
			if !near(got.Availability, tt.want.Availability) ||
				!near(got.Performance, tt.want.Performance) ||
				!near(got.Quality, tt.want.Quality) ||
				!near(got.OEE, tt.want.OEE) {
				t.Errorf("Calculate(%+v) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
