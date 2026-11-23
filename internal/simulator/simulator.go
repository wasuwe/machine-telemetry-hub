// Package simulator fakes a shop floor: every machine runs in its own
// goroutine and sends shot / state events, like a real PLC gateway would.
package simulator

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/wasuwe/machine-telemetry-hub/internal/telemetry"
)

// Config controls how machines behave.
type Config struct {
	ScrapRate float64 // chance a part is bad
	SlowDown  float64 // max extra cycle time, e.g. 0.25 = up to 25% slower than ideal
	DownProb  float64 // chance of a breakdown after each part
	IdleProb  float64 // chance of waiting (no material) after each part

	DownMin, DownMax time.Duration
	IdleMin, IdleMax time.Duration

	StartJitter time.Duration // machines start at random times within this window
}

// DefaultConfig gives a realistic-looking dashboard (OEE around 55–80%).
func DefaultConfig() Config {
	return Config{
		ScrapRate: 0.04,
		SlowDown:  0.25,
		DownProb:  0.015,
		IdleProb:  0.025,
		DownMin:   6 * time.Second,
		DownMax:   20 * time.Second,
		IdleMin:   2 * time.Second,
		IdleMax:   8 * time.Second,

		StartJitter: 3 * time.Second,
	}
}

// DefaultMachines builds n press machines with a mix of ideal cycle times.
func DefaultMachines(n int) []telemetry.Machine {
	tonnage := []int{80, 150, 250, 400}
	machines := make([]telemetry.Machine, n)
	for i := range machines {
		machines[i] = telemetry.Machine{
			ID:         fmt.Sprintf("M%02d", i+1),
			Name:       fmt.Sprintf("Press %02d · %dT", i+1, tonnage[i%len(tonnage)]),
			IdealCycle: time.Duration(1200+400*(i%len(tonnage))) * time.Millisecond,
		}
	}
	return machines
}

// Simulator drives a set of machines.
type Simulator struct {
	Machines []telemetry.Machine
	Config   Config
	Seed     uint64
	Now      func() time.Time
}

// New creates a simulator with the default config.
func New(machines []telemetry.Machine, seed uint64) *Simulator {
	return &Simulator{Machines: machines, Config: DefaultConfig(), Seed: seed, Now: time.Now}
}

// Run starts one goroutine per machine and sends their events to out.
// It blocks until ctx is cancelled and all machines stop, then closes out.
func (s *Simulator) Run(ctx context.Context, out chan<- telemetry.Event) {
	var wg sync.WaitGroup
	for i, m := range s.Machines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(s.Seed, uint64(i)))
			s.runMachine(ctx, m, rng, out)
		}()
	}
	wg.Wait()
	close(out)
}

func (s *Simulator) runMachine(ctx context.Context, m telemetry.Machine, rng *rand.Rand, out chan<- telemetry.Event) {
	cfg := s.Config

	emit := func(e telemetry.Event) bool {
		e.MachineID = m.ID
		e.At = s.Now()
		select {
		case out <- e:
			return true
		case <-ctx.Done():
			return false
		}
	}
	setState := func(st telemetry.State) bool {
		return emit(telemetry.Event{Kind: telemetry.EventState, State: st})
	}

	// Machines start at slightly different times, like a real shift start.
	if !sleep(ctx, between(rng, 0, cfg.StartJitter)) || !setState(telemetry.StateRunning) {
		return
	}

	for {
		cycle := time.Duration(float64(m.IdealCycle) * (1 + rng.Float64()*cfg.SlowDown))
		if !sleep(ctx, cycle) {
			return
		}
		if !emit(telemetry.Event{Kind: telemetry.EventShot, Good: rng.Float64() >= cfg.ScrapRate, CycleTime: cycle}) {
			return
		}

		var stop telemetry.State
		var wait time.Duration
		switch r := rng.Float64(); {
		case r < cfg.DownProb:
			stop, wait = telemetry.StateDown, between(rng, cfg.DownMin, cfg.DownMax)
		case r < cfg.DownProb+cfg.IdleProb:
			stop, wait = telemetry.StateIdle, between(rng, cfg.IdleMin, cfg.IdleMax)
		default:
			continue
		}
		if !setState(stop) || !sleep(ctx, wait) || !setState(telemetry.StateRunning) {
			return
		}
	}
}

// sleep waits for d or until ctx is cancelled. It reports whether it slept fully.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func between(rng *rand.Rand, lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(rng.Int64N(int64(hi-lo)))
}
