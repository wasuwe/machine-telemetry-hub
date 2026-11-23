// Package hub collects machine events, keeps live OEE counters per machine,
// and fans out snapshots to any number of subscribers (e.g. browser tabs).
package hub

import (
	"context"
	"sync"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/oee"
	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

// MachineSnapshot is the live view of one machine.
type MachineSnapshot struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	State        telemetry.State `json:"state"`
	StateSince   time.Time       `json:"stateSince"`
	Total        int             `json:"total"`
	Good         int             `json:"good"`
	LastCycleMs  int64           `json:"lastCycleMs"`
	IdealCycleMs int64           `json:"idealCycleMs"`
	oee.Result
}

// Summary is the plant-wide view.
type Summary struct {
	Running int     `json:"running"`
	Idle    int     `json:"idle"`
	Down    int     `json:"down"`
	Total   int     `json:"total"`
	Good    int     `json:"good"`
	AvgOEE  float64 `json:"avgOee"`
}

// Snapshot is everything the dashboard needs at one point in time.
type Snapshot struct {
	At       time.Time         `json:"at"`
	Summary  Summary           `json:"summary"`
	Machines []MachineSnapshot `json:"machines"`
}

type machineState struct {
	machine   telemetry.Machine
	state     telemetry.State
	since     time.Time     // when the current state started
	start     time.Time     // when tracking started (planned time begins)
	running   time.Duration // running time from finished RUNNING periods
	total     int
	good      int
	lastCycle time.Duration
}

// Hub is safe for concurrent use.
type Hub struct {
	now func() time.Time

	mu       sync.RWMutex
	order    []string
	machines map[string]*machineState

	subsMu sync.Mutex
	subs   map[chan Snapshot]struct{}
}

// New creates a hub tracking the given machines. now is injectable for tests.
func New(machines []telemetry.Machine, now func() time.Time) *Hub {
	h := &Hub{
		now:      now,
		machines: make(map[string]*machineState, len(machines)),
		subs:     make(map[chan Snapshot]struct{}),
	}
	start := now()
	for _, m := range machines {
		h.order = append(h.order, m.ID)
		h.machines[m.ID] = &machineState{machine: m, state: telemetry.StateIdle, since: start, start: start}
	}
	return h
}

// Apply records one event. Events for unknown machines are ignored.
func (h *Hub) Apply(e telemetry.Event) {
	h.mu.Lock()
	defer h.mu.Unlock()

	m, ok := h.machines[e.MachineID]
	if !ok {
		return
	}
	switch e.Kind {
	case telemetry.EventState:
		if m.state == e.State {
			return
		}
		if m.state == telemetry.StateRunning {
			m.running += e.At.Sub(m.since)
		}
		m.state = e.State
		m.since = e.At
	case telemetry.EventShot:
		m.total++
		if e.Good {
			m.good++
		}
		m.lastCycle = e.CycleTime
	}
}

// Snapshot returns the current state of every machine, in a stable order.
func (h *Hub) Snapshot() Snapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	now := h.now()
	snap := Snapshot{At: now, Machines: make([]MachineSnapshot, 0, len(h.order))}
	var oeeSum float64

	for _, id := range h.order {
		m := h.machines[id]
		running := m.running
		if m.state == telemetry.StateRunning {
			running += now.Sub(m.since) // include the period still in progress
		}
		res := oee.Calculate(oee.Counters{
			Planned:    now.Sub(m.start),
			Running:    running,
			IdealCycle: m.machine.IdealCycle,
			Total:      m.total,
			Good:       m.good,
		})
		snap.Machines = append(snap.Machines, MachineSnapshot{
			ID:           m.machine.ID,
			Name:         m.machine.Name,
			State:        m.state,
			StateSince:   m.since,
			Total:        m.total,
			Good:         m.good,
			LastCycleMs:  m.lastCycle.Milliseconds(),
			IdealCycleMs: m.machine.IdealCycle.Milliseconds(),
			Result:       res,
		})

		switch m.state {
		case telemetry.StateRunning:
			snap.Summary.Running++
		case telemetry.StateIdle:
			snap.Summary.Idle++
		case telemetry.StateDown:
			snap.Summary.Down++
		}
		snap.Summary.Total += m.total
		snap.Summary.Good += m.good
		oeeSum += res.OEE
	}
	if n := len(h.order); n > 0 {
		snap.Summary.AvgOEE = oeeSum / float64(n)
	}
	return snap
}

// Subscribe returns a channel that receives snapshots, and a function to stop.
// A slow subscriber never blocks the hub: if it has not read the previous
// snapshot yet, the new one is skipped for that subscriber.
func (h *Hub) Subscribe() (<-chan Snapshot, func()) {
	ch := make(chan Snapshot, 1)
	h.subsMu.Lock()
	h.subs[ch] = struct{}{}
	h.subsMu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.subsMu.Lock()
			delete(h.subs, ch)
			close(ch)
			h.subsMu.Unlock()
		})
	}
	return ch, cancel
}

// Subscribers returns how many subscribers are connected.
func (h *Hub) Subscribers() int {
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	return len(h.subs)
}

// Broadcast sends the current snapshot to every subscriber.
func (h *Hub) Broadcast() {
	snap := h.Snapshot()
	h.subsMu.Lock()
	defer h.subsMu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- snap:
		default: // subscriber is busy; it will get the next one
		}
	}
}

// Run applies events from the channel and broadcasts a snapshot every
// interval until ctx is cancelled.
func (h *Hub) Run(ctx context.Context, events <-chan telemetry.Event, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				events = nil // producer finished; keep broadcasting
				continue
			}
			h.Apply(e)
		case <-ticker.C:
			h.Broadcast()
		}
	}
}
