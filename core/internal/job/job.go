// Package job owns the queue and the job state machine.
package job

import (
	"encoding/json"
	"time"
)

type State string

// queued → running → done | failed | blocked. blocked is first-class:
// an output that failed compliance, never an error (docs/compliance.md).
const (
	StateQueued  State = "queued"
	StateRunning State = "running"
	StateDone    State = "done"
	StateFailed  State = "failed"
	StateBlocked State = "blocked"
)

var transitions = map[State][]State{
	StateQueued:  {StateRunning, StateFailed},
	StateRunning: {StateDone, StateFailed, StateBlocked},
}

func CanTransition(from, to State) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

func (s State) Terminal() bool {
	return s == StateDone || s == StateFailed || s == StateBlocked
}

type Job struct {
	ID         string
	Kind       string
	InputPath  string // blob id of the uploaded input (image-input kinds)
	State      State
	Params     json.RawMessage
	Workflow   json.RawMessage
	Error      string
	ImagePath  string
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}
