// Package record keeps what a judge and the demo page need to see: why the agent
// worked, what it meant to do before it ordered anything, and where it was
// stopped.
//
// Two implementations, one meaning. Memory is what tests use; Postgres is what
// runs, because a restart must not erase the week's evidence.
package record

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

// Intent is what a session states BEFORE it sends an order: the thesis, the
// structure it intends to open and the loss it accepts. A judge reads fills;
// only this says what the session meant to do.
type Intent struct {
	At time.Time `json:"at"`
	// TurnRef is the turn that recorded it, and Session is the waker of that turn.
	// Both come from the harness, not from the session: a name the model types is
	// what it believes it is, and the record answers what it was.
	TurnRef   string `json:"turn_ref"`
	Session   string `json:"session"`
	Thesis    string `json:"thesis"`
	Structure string `json:"structure"`
	MaxLoss   string `json:"max_loss"`
	// UnderlyingPrice is what the underlying cost when the intent was stated.
	// The defence windows measure how far price has travelled since, and there is
	// nowhere else to read where it started.
	UnderlyingPrice string `json:"underlying_price"`
	// EnvelopeChecked says whether this intent was checked against an envelope
	// read in the same turn. Absent where the row predates the column; false
	// where the deployment cannot make the check - see the migration.
	EnvelopeChecked *bool `json:"envelope_checked,omitempty"`
}

// Turn is one run of the agent: when it ran, who woke it and why.
// Said is one thing the agent told the room during a turn.
type Said struct {
	TurnRef string    `json:"turn_ref"`
	At      time.Time `json:"at"`
	Text    string    `json:"text"`
}

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

// ToolCall is one thing the session did with its hands: which tool, on which
// server, with what arguments, and how it ended. The intent says what it meant
// to do; this says what it did.
type ToolCall struct {
	Ref        string          `json:"ref"`
	TurnRef    string          `json:"turn_ref"`
	Server     string          `json:"server"`
	Tool       string          `json:"tool"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	StartedAt  time.Time       `json:"started_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
	Status     string          `json:"status"`
	Failure    string          `json:"failure,omitempty"`
	// Answer is the beginning of what the tool said back. A broker refusal lives
	// here: it arrives inside the answer, not as a failure of the call.
	Answer string `json:"answer,omitempty"`
}

// ExecutionStep is one move the ladder made on an order: what the price was,
// what it became, what the book was showing and the worst price the session
// allowed. Kept because the question it answers - what did walking the price
// save us - cannot be asked of a log that a redeployment throws away.
type ExecutionStep struct {
	OrderRef string    `json:"order_ref"`
	At       time.Time `json:"at"`
	Action   string    `json:"action"`
	Was      float64   `json:"was"`
	Became   *float64  `json:"became,omitempty"`
	Showing  *float64  `json:"showing,omitempty"`
	Floor    *float64  `json:"floor,omitempty"`
	// Quantity is how many contracts a fill was for. Without it the record holds
	// a price and not money: 0.28 says nothing about whether we collected
	// twenty-eight dollars or fourteen hundred.
	Quantity *float64 `json:"quantity,omitempty"`
	// ReplacedBy is the id the broker gave the replacement, on the step that
	// moved the price. It is what joins one order's steps to the fill that
	// arrives under another id; the name is the broker's own.
	ReplacedBy *string `json:"replaced_by,omitempty"`
}

// State is everything the page shows at once. A refusal is not here: it comes
// from the gateway, the gateway is a service outside this stack with no path
// into this database, and a section that can only be empty reads as "the agent
// was never stopped" rather than as "we do not know". What an order runs into
// today is in Calls - a failed call carries the broker's own words. The limits in force are not here
// yet: they are the gateway's envelope, and the gateway is not in front of the
// broker. A field standing empty until then would read as an agent under no
// limits at all.
type State struct {
	Turns   []Turn          `json:"turns"`
	Calls   []ToolCall      `json:"calls"`
	Steps   []ExecutionStep `json:"steps"`
	Intents []Intent        `json:"intents"`
	Said    []Said          `json:"said"`
}

// Keeper is what the rest of the program writes to and reads from. The tools a
// session calls, the harness and the page all speak through this one door.
type Keeper interface {
	Read(ctx context.Context) (State, error)
	AppendIntent(ctx context.Context, intent Intent) error
	// AppendSaid writes down what the agent SAID inside a turn. The calls and
	// their answers already show what happened; this is the only place the
	// reasoning behind it survives in a form anything can read - the transcripts
	// keep it too, but in the agent's own format and with no link to our turns.
	AppendSaid(ctx context.Context, said Said) error
	TurnStarted(ctx context.Context, turn Turn) error
	AppendExecutionStep(ctx context.Context, step ExecutionStep) error
	// NoteFill writes a fill down once and answers whether this call was the one
	// that wrote it. The ladder meets the same filled order on every pass and
	// forgets everything it held in memory when the process restarts, so what
	// makes a fill new is the record, not the ladder.
	NoteFill(ctx context.Context, step ExecutionStep) (bool, error)
	CallStarted(ctx context.Context, call ToolCall) error
	CallFinished(ctx context.Context, ref string, finishedAt time.Time, status, failure, answer string) error
	TurnFinished(ctx context.Context, ref string, finishedAt time.Time, failure string) error
	// CloseCallsLeftOpen closes tool calls a dead process left running. A call
	// that was in flight cannot be said to have happened or not: for an order that
	// distinction is the whole point, so it is recorded as unknown, never as done.
	CloseCallsLeftOpen(ctx context.Context, at time.Time) (int, error)
	// CloseTurnsLeftOpen closes what a previous process was in the middle of when
	// it died, and answers how many there were. Called once at startup: a turn
	// that stays open forever reads as work still running.
	CloseTurnsLeftOpen(ctx context.Context, at time.Time) (int, error)
	// LastRuns says when each named waker last started a turn, no earlier than
	// since. A harness that restarts inside a session's window reads this instead
	// of assuming the session has not run: assuming would open the position twice.
	LastRuns(ctx context.Context, since time.Time) (map[string]time.Time, error)
}

// RestartedFailure is what a turn carries when the process running it died. The
// record says what happened rather than leaving the turn open.
const RestartedFailure = "the process restarted while this turn was running"

// SilentFailure is what a turn carries when the agent produced neither a word
// nor a tool call. It did not decide to do nothing - it never ran: the model was
// unreachable, or the thread was refused. Recorded as a failure because a turn
// counted done is a turn nobody looks at again.
const SilentFailure = "the agent ended the turn without saying or calling anything"

// StatusUnknown is what a tool call carries when the process died while it was
// in flight. An order in that state may or may not have reached the broker, and
// saying either would be a guess.
const StatusUnknown = "unknown"

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
		Calls:   append(make([]ToolCall, 0, len(m.state.Calls)), m.state.Calls...),
		Steps:   append(make([]ExecutionStep, 0, len(m.state.Steps)), m.state.Steps...),
		Turns:   append(make([]Turn, 0, len(m.state.Turns)), m.state.Turns...),
		Intents: append(make([]Intent, 0, len(m.state.Intents)), m.state.Intents...),
		Said:    append(make([]Said, 0, len(m.state.Said)), m.state.Said...),
	}

	return out, nil
}

func (m *Memory) AppendIntent(_ context.Context, intent Intent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Intents = append(m.state.Intents, intent)

	return nil
}

func (m *Memory) AppendSaid(_ context.Context, said Said) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Said = append(m.state.Said, said)

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

func (m *Memory) AppendExecutionStep(_ context.Context, step ExecutionStep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Steps = append(m.state.Steps, step)

	return nil
}

// NoteFill refuses a second fill for the same order, exactly as the database
// does. A double that accepted what the real one refuses would let the ladder
// announce a trade twice and the tier would still report ok.
func (m *Memory) NoteFill(_ context.Context, step ExecutionStep) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, kept := range m.state.Steps {
		if kept.Action == "filled" && kept.OrderRef == step.OrderRef {
			return false, nil
		}
	}
	step.Action = "filled"
	m.state.Steps = append(m.state.Steps, step)

	return true, nil
}

func (m *Memory) CallStarted(_ context.Context, call ToolCall) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state.Calls = append(m.state.Calls, call)

	return nil
}

func (m *Memory) CallFinished(_ context.Context, ref string, finishedAt time.Time, status, failure, answer string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.state.Calls {
		if m.state.Calls[i].Ref == ref {
			m.state.Calls[i].FinishedAt = &finishedAt
			m.state.Calls[i].Status = status
			m.state.Calls[i].Failure = failure
			m.state.Calls[i].Answer = answer
			return nil
		}
	}

	return nil
}

func (m *Memory) CloseCallsLeftOpen(_ context.Context, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	closed := 0
	for i := range m.state.Calls {
		if m.state.Calls[i].FinishedAt != nil {
			continue
		}
		finished := at
		m.state.Calls[i].FinishedAt = &finished
		m.state.Calls[i].Status = StatusUnknown
		m.state.Calls[i].Failure = RestartedFailure
		closed++
	}

	return closed, nil
}

func (m *Memory) LastRuns(_ context.Context, since time.Time) (map[string]time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	last := map[string]time.Time{}
	for _, turn := range m.state.Turns {
		if turn.StartedAt.Before(since) {
			continue
		}
		if was, seen := last[turn.WokenBy]; !seen || turn.StartedAt.After(was) {
			last[turn.WokenBy] = turn.StartedAt
		}
	}

	return last, nil
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
