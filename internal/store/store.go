// Package store keeps the latest machine readings in memory, like a small
// time-series table. It stands in for a real database in the demo: every
// event is written here first, and the dashboard reads the rows back.
package store

import (
	"sync"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

// Reading is one stored row.
type Reading struct {
	ID        int64               `json:"id"`
	At        time.Time           `json:"at"`
	MachineID string              `json:"machine"`
	Kind      telemetry.EventKind `json:"kind"`
	State     telemetry.State     `json:"state,omitempty"`   // state rows only
	Good      *bool               `json:"good,omitempty"`    // shot rows only
	CycleMs   int64               `json:"cycleMs,omitempty"` // shot rows only
}

// Store is a fixed-size ring of readings. Once it is full, the oldest row is
// overwritten. It is safe for concurrent use.
type Store struct {
	mu   sync.RWMutex
	rows []Reading // ring buffer, len == capacity once full
	next int       // index the next row is written to
	last int64     // ID of the newest row (0 = empty)
}

// New creates a store that keeps the last capacity rows.
func New(capacity int) *Store {
	if capacity < 1 {
		capacity = 1
	}
	return &Store{rows: make([]Reading, 0, capacity)}
}

// Add writes one event as a new row and returns it.
func (s *Store) Add(e telemetry.Event) Reading {
	r := Reading{At: e.At, MachineID: e.MachineID, Kind: e.Kind}
	switch e.Kind {
	case telemetry.EventState:
		r.State = e.State
	case telemetry.EventShot:
		good := e.Good
		r.Good = &good
		r.CycleMs = e.CycleTime.Milliseconds()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.last++
	r.ID = s.last
	if len(s.rows) < cap(s.rows) {
		s.rows = append(s.rows, r)
	} else {
		s.rows[s.next] = r
	}
	s.next = (s.next + 1) % cap(s.rows)
	return r
}

// Total is the number of rows ever written, including overwritten ones.
func (s *Store) Total() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.last
}

// After returns rows with ID > afterID, oldest first, at most limit rows.
// If more than limit rows match, it returns the newest limit of them, so a
// client that falls behind catches up instead of replaying old rows.
func (s *Store) After(afterID int64, limit int) []Reading {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n := len(s.rows)
	if n == 0 || limit < 1 || afterID >= s.last {
		return []Reading{}
	}
	oldest := s.last - int64(n) + 1
	from := max(afterID+1, oldest, s.last-int64(limit)+1)

	out := make([]Reading, 0, s.last-from+1)
	start := 0 // ring index of the oldest row
	if n == cap(s.rows) {
		start = s.next
	}
	for id := from; id <= s.last; id++ {
		out = append(out, s.rows[(start+int(id-oldest))%n])
	}
	return out
}
