// Package store keeps what a judge and the demo page need to see: the limits in
// force, the intent each session recorded before it ordered anything, and the
// refusals the gateway returned.
//
// The in-memory implementation exists so the stack runs before Postgres does;
// it is replaced, not extended, once the schema is wired.
package store

import (
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

type Refusal struct {
	At       time.Time `json:"at"`
	Boundary string    `json:"boundary"`
	Detail   string    `json:"detail"`
}

type State struct {
	Ruleset  string    `json:"ruleset"`
	Limits   []string  `json:"limits"`
	Intents  []Intent  `json:"intents"`
	Refusals []Refusal `json:"refusals"`
}

type Memory struct {
	mu    sync.RWMutex
	state State
}

func NewMemory() *Memory {
	return &Memory{state: State{
		Ruleset:  "none",
		Limits:   []string{},
		Intents:  []Intent{},
		Refusals: []Refusal{},
	}}
}

func (m *Memory) Read() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// The copies start empty rather than nil: a reader that sees null cannot tell
	// "nothing recorded" from "this field is not implemented".
	out := State{
		Ruleset:  m.state.Ruleset,
		Limits:   make([]string, 0, len(m.state.Limits)),
		Intents:  make([]Intent, 0, len(m.state.Intents)),
		Refusals: make([]Refusal, 0, len(m.state.Refusals)),
	}
	out.Limits = append(out.Limits, m.state.Limits...)
	out.Intents = append(out.Intents, m.state.Intents...)
	out.Refusals = append(out.Refusals, m.state.Refusals...)

	return out
}

func (m *Memory) AppendIntent(i Intent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Intents = append(m.state.Intents, i)
}

func (m *Memory) AppendRefusal(r Refusal) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Refusals = append(m.state.Refusals, r)
}
