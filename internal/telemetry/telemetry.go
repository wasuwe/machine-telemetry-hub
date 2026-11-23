// Package telemetry defines the data that flows from machines into the hub.
package telemetry

import "time"

// State is the operating state of a machine.
type State string

const (
	StateRunning State = "RUNNING" // producing parts
	StateIdle    State = "IDLE"    // ready but waiting (no material, no operator)
	StateDown    State = "DOWN"    // breakdown / unplanned stop
)

// EventKind tells what an Event carries.
type EventKind string

const (
	EventShot  EventKind = "shot"  // one machine cycle finished (one part)
	EventState EventKind = "state" // machine changed state
)

// Event is a single message sent by a machine.
type Event struct {
	MachineID string
	Kind      EventKind
	At        time.Time

	// For EventState.
	State State

	// For EventShot.
	Good      bool
	CycleTime time.Duration
}

// Machine is the static description of a machine on the shop floor.
type Machine struct {
	ID         string
	Name       string
	IdealCycle time.Duration // fastest possible time to make one part
}
