package hub

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

// fakeClock lets tests control "now".
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func newClock() *fakeClock               { return &fakeClock{t: time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)} }
func near(a, b float64) bool             { return math.Abs(a-b) < 1e-9 }
func machine(id string) telemetry.Machine {
	return telemetry.Machine{ID: id, Name: id, IdealCycle: time.Second}
}

func TestSnapshotComputesOEE(t *testing.T) {
	clk := newClock()
	t0 := clk.Now()
	h := New([]telemetry.Machine{machine("M01")}, clk.Now)

	h.Apply(telemetry.Event{MachineID: "M01", Kind: telemetry.EventState, State: telemetry.StateRunning, At: t0})
	for i := range 8 {
		h.Apply(telemetry.Event{MachineID: "M01", Kind: telemetry.EventShot, Good: i < 6, CycleTime: 1250 * time.Millisecond, At: t0})
	}
	h.Apply(telemetry.Event{MachineID: "M01", Kind: telemetry.EventState, State: telemetry.StateDown, At: t0.Add(10 * time.Second)})
	clk.Add(20 * time.Second)

	m := h.Snapshot().Machines[0]

	// Ran 10s of 20s planned; 8 parts × 1s ideal in 10s; 6 of 8 good.
	if !near(m.Availability, 0.5) || !near(m.Performance, 0.8) || !near(m.Quality, 0.75) || !near(m.OEE, 0.3) {
		t.Fatalf("got A=%v P=%v Q=%v OEE=%v", m.Availability, m.Performance, m.Quality, m.OEE)
	}
	if m.State != telemetry.StateDown || m.Total != 8 || m.Good != 6 || m.LastCycleMs != 1250 {
		t.Fatalf("unexpected snapshot: %+v", m)
	}
}

func TestSnapshotCountsRunningPeriodInProgress(t *testing.T) {
	clk := newClock()
	h := New([]telemetry.Machine{machine("M01")}, clk.Now)

	clk.Add(5 * time.Second) // idle for 5s
	h.Apply(telemetry.Event{MachineID: "M01", Kind: telemetry.EventState, State: telemetry.StateRunning, At: clk.Now()})
	clk.Add(15 * time.Second) // still running

	if a := h.Snapshot().Machines[0].Availability; !near(a, 0.75) {
		t.Fatalf("availability = %v, want 0.75", a)
	}
}

func TestSummary(t *testing.T) {
	clk := newClock()
	h := New([]telemetry.Machine{machine("A"), machine("B"), machine("C")}, clk.Now)
	h.Apply(telemetry.Event{MachineID: "A", Kind: telemetry.EventState, State: telemetry.StateRunning, At: clk.Now()})
	h.Apply(telemetry.Event{MachineID: "B", Kind: telemetry.EventState, State: telemetry.StateDown, At: clk.Now()})
	h.Apply(telemetry.Event{MachineID: "A", Kind: telemetry.EventShot, Good: true, At: clk.Now()})
	h.Apply(telemetry.Event{MachineID: "unknown", Kind: telemetry.EventShot, Good: true, At: clk.Now()})

	s := h.Snapshot().Summary
	if s.Running != 1 || s.Down != 1 || s.Idle != 1 || s.Total != 1 || s.Good != 1 {
		t.Fatalf("summary = %+v", s)
	}
}

func TestSubscribeBroadcastAndCancel(t *testing.T) {
	h := New([]telemetry.Machine{machine("M01")}, newClock().Now)

	ch, cancel := h.Subscribe()
	h.Broadcast()
	h.Broadcast() // buffer is full: must not block

	if snap := <-ch; len(snap.Machines) != 1 {
		t.Fatalf("got %d machines", len(snap.Machines))
	}

	cancel()
	cancel() // safe to call twice
	if _, ok := <-ch; ok {
		t.Fatal("channel should be closed after cancel")
	}
	if n := h.Subscribers(); n != 0 {
		t.Fatalf("subscribers = %d, want 0", n)
	}
}

func TestRunAppliesEventsAndBroadcasts(t *testing.T) {
	h := New([]telemetry.Machine{machine("M01")}, time.Now)
	ch, cancel := h.Subscribe()
	defer cancel()

	ctx, stop := context.WithCancel(context.Background())
	events := make(chan telemetry.Event)
	done := make(chan struct{})
	go func() { h.Run(ctx, events, 10*time.Millisecond); close(done) }()

	events <- telemetry.Event{MachineID: "M01", Kind: telemetry.EventShot, Good: true, At: time.Now()}

	deadline := time.After(2 * time.Second)
	for {
		select {
		case snap := <-ch:
			if snap.Summary.Total == 1 {
				stop()
				<-done
				return
			}
		case <-deadline:
			t.Fatal("did not receive a snapshot with the shot")
		}
	}
}
