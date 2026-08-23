// Package store keeps what the demo page shows: the limits in force, the
// refusals the gateway returned and the positions currently open.
//
// The in-memory implementation exists so the stack runs before Postgres does;
// it is replaced, not extended, once the schema is wired.
package store

import "sync"

type Refusal struct {
	At       string `json:"at"`
	Boundary string `json:"boundary"`
	Detail   string `json:"detail"`
}

type State struct {
	Ruleset  string    `json:"ruleset"`
	Limits   []string  `json:"limits"`
	Refusals []Refusal `json:"refusals"`
}

type Memory struct {
	mu    sync.RWMutex
	state State
}

func NewMemory() *Memory {
	return &Memory{state: State{Ruleset: "none", Limits: []string{}, Refusals: []Refusal{}}}
}

func (m *Memory) Read() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

func (m *Memory) Write(s State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s
}
