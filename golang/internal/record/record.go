// Package record keeps what a judge and the demo page need to see: why the agent
// worked, what it meant to do before it ordered anything, and where it was
// stopped.
//
// Two implementations, one meaning. Memory is what tests use; Postgres is what
// runs, because a restart must not erase the week's evidence.
package record

import (
	"context"
	"sync"
	"time"
)

// Intent is what a session states BEFORE it sends an order: the thesis, the
// structure it intends to open and the loss it accepts. A judge reads fills;
// only this says what the session meant to do.
type Intent struct {
	At        time.Time `json:"at"`
	Session   string    `json:"session"`
	Thesis    string    `json:"thesis"`
	Structure string    `json:"structure"`
	MaxLoss   string    `json:"max_loss"`
}

// Refusal is a boundary that stopped an order, and what it said.
type Refusal struct {
	At       time.Time `json:"at"`
	Boundary string    `json:"boundary"`
	Detail   string    `json:"detail"`
}

// Turn is one run of the agent: when it ran, who woke it and why.
type Turn struct {
	Ref        string     `json:"ref"`
	ThreadRef  string     `json:"thread_ref"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	WokenBy    string     `json:"woken_by"`
	Cause      string     `json:"cause"`
	Model      string     `json:"model,omitempty"`
	Failure    string     `json:"failure,omitempty"`
}

// State is everything the page shows at once. The limits in force are not here
// yet: they are the gateway's envelope, and the gateway is not in front of the
// broker. A field standing empty until then would read as an agent under no
// limits at all.
type State struct {
	Turns    []Turn    `json:"turns"`
	Intents  []Intent  `json:"intents"`
	Refusals []Refusal `json:"refusals"`
}

// Keeper is what the rest of the program writes to and reads from. The tools a
// session calls, the harness and the page all speak through this one door.
type Keeper interface {
	Read(ctx context.Context) (State, error)
	AppendIntent(ctx context.Context, intent Intent) error
	AppendRefusal(ctx context.Context, refusal Refusal) error
	TurnStarted(ctx context.Context, turn Turn) error
	TurnFinished(ctx context.Context, ref string, finishedAt time.Time, failure string) error
	// CloseTurnsLeftOpen closes what a previous process was in the middle of when
	// it died, and answers how many there were. Called once at startup: a turn
	// that stays open forever reads as work still running.
	CloseTurnsLeftOpen(ctx context.Context, at time.Time) (int, error)
}

// RestartedFailure is what a turn carries when the process running it died. The
// record says what happened rather than leaving the turn open.
const RestartedFailure = "the process restarted while this turn was running"

// Memory keeps the record for the length of one run. Tests use it; nothing else
// should, because the week's evidence has to outlive a restart.
type Memory struct {
	mu    sync.RWMutex
	state State
}

func NewMemory() *Memory {
	return &Memory{}
}

func (m *Memory) Read(context.Context) (State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// The copies start empty rather than nil: a reader that sees null cannot tell
	// "nothing recorded" from "this field is not implemented".
	out := State{
		Turns:    append(make([]Turn, 0, len(m.state.Turns)), m.state.Turns...),
		Intents:  append(make([]Intent, 0, len(m.state.Intents)), m.state.Intents...),
		Refusals: append(make([]Refusal, 0, len(m.state.Refusals)), m.state.Refusals...),
	}

	return out, nil
}

func (m *Memory) AppendIntent(_ context.Context, intent Intent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Intents = append(m.state.Intents, intent)

	return nil
}

func (m *Memory) AppendRefusal(_ context.Context, refusal Refusal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Refusals = append(m.state.Refusals, refusal)

	return nil
}

func (m *Memory) TurnStarted(_ context.Context, turn Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Turns = append(m.state.Turns, turn)

	return nil
}

func (m *Memory) CloseTurnsLeftOpen(_ context.Context, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	closed := 0
	for i := range m.state.Turns {
		if m.state.Turns[i].FinishedAt != nil {
			continue
		}
		finished := at
		m.state.Turns[i].FinishedAt = &finished
		m.state.Turns[i].Failure = RestartedFailure
		closed++
	}

	return closed, nil
}

func (m *Memory) TurnFinished(_ context.Context, ref string, finishedAt time.Time, failure string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.state.Turns {
		if m.state.Turns[i].Ref == ref {
			m.state.Turns[i].FinishedAt = &finishedAt
			m.state.Turns[i].Failure = failure
			return nil
		}
	}

	return nil
}
