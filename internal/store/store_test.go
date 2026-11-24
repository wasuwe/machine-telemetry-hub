package store

import (
	"testing"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

func shot(id string, good bool) telemetry.Event {
	return telemetry.Event{MachineID: id, Kind: telemetry.EventShot, At: time.Unix(0, 0), Good: good, CycleTime: 1500 * time.Millisecond}
}

func ids(rows []Reading) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.ID
	}
	return out
}

func equal(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAddFillsFields(t *testing.T) {
	s := New(10)
	r := s.Add(shot("M01", false))
	if r.ID != 1 || r.MachineID != "M01" || r.Kind != telemetry.EventShot || r.CycleMs != 1500 {
		t.Fatalf("unexpected row %+v", r)
	}
	if r.Good == nil || *r.Good {
		t.Fatalf("good should be a pointer to false, got %v", r.Good)
	}

	st := s.Add(telemetry.Event{MachineID: "M02", Kind: telemetry.EventState, State: telemetry.StateDown})
	if st.State != telemetry.StateDown || st.Good != nil || st.CycleMs != 0 {
		t.Fatalf("state row should only carry the state, got %+v", st)
	}
}

func TestAfter(t *testing.T) {
	s := New(5)
	if got := s.After(0, 10); len(got) != 0 {
		t.Fatalf("empty store returned %v", ids(got))
	}
	for range 8 { // IDs 1..8; rows 4..8 remain
		s.Add(shot("M01", true))
	}

	cases := []struct {
		name  string
		after int64
		limit int
		want  []int64
	}{
		{"from start skips overwritten rows", 0, 10, []int64{4, 5, 6, 7, 8}},
		{"after an id", 6, 10, []int64{7, 8}},
		{"limit keeps the newest", 0, 2, []int64{7, 8}},
		{"nothing new", 8, 10, []int64{}},
		{"zero limit", 0, 0, []int64{}},
	}
	for _, c := range cases {
		if got := ids(s.After(c.after, c.limit)); !equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	if s.Total() != 8 {
		t.Fatalf("total = %d, want 8", s.Total())
	}
}
