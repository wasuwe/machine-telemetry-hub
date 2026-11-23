package simulator

import (
	"context"
	"testing"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

func TestDefaultMachines(t *testing.T) {
	ms := DefaultMachines(5)
	if len(ms) != 5 {
		t.Fatalf("got %d machines", len(ms))
	}
	seen := map[string]bool{}
	for _, m := range ms {
		if seen[m.ID] {
			t.Fatalf("duplicate id %s", m.ID)
		}
		seen[m.ID] = true
		if m.IdealCycle <= 0 {
			t.Fatalf("%s has no ideal cycle", m.ID)
		}
	}
}

func TestRunEmitsEventsAndStopsCleanly(t *testing.T) {
	machines := []telemetry.Machine{
		{ID: "A", IdealCycle: time.Millisecond},
		{ID: "B", IdealCycle: time.Millisecond},
	}
	sim := New(machines, 42)
	sim.Config = Config{ScrapRate: 0.5, DownProb: 0.1, IdleProb: 0.1, DownMax: time.Millisecond, IdleMax: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan telemetry.Event, 16)
	go sim.Run(ctx, out)

	shots := map[string]int{}
	var started int
	deadline := time.After(5 * time.Second)
	for shots["A"] < 20 || shots["B"] < 20 {
		select {
		case e := <-out:
			switch e.Kind {
			case telemetry.EventShot:
				shots[e.MachineID]++
			case telemetry.EventState:
				if e.State == telemetry.StateRunning {
					started++
				}
			}
		case <-deadline:
			t.Fatalf("not enough events: %v", shots)
		}
	}
	if started < 2 {
		t.Fatalf("expected both machines to start, got %d", started)
	}

	cancel()
	// out must be closed once every machine goroutine has stopped.
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("output channel was not closed after cancel")
		}
	}
}
