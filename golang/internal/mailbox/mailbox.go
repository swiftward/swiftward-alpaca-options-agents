// Package mailbox lets a harness drive an agent it does not start.
//
// The harness holds the clock, the wake-ups, the room and the record, and hands
// all of it to one thing it does start: a codex process on the other end of a
// pipe. That pipe is the only part of this program that names a vendor, and it
// is also the part that decides which agents may ever run here. An agent that
// cannot be started as a child process - a session a person is sitting in, a
// harness on another machine, a model behind someone else's subscription -
// cannot be woken by this clock at all.
//
// A mailbox is the same Conversation with the pipe taken out. A turn is not
// written to a process; it is parked, and whoever holds the mailbox token comes
// and takes it. What the agent says comes back the same way, by a request rather
// than by a stream. Nothing else in the harness changes: the schedule still
// decides when, the wake-ups still fire, the room still shows the work, and the
// record still says what happened and why.
//
// The point is not indirection. It is that WHO the agent is stops being decided
// at build time. The same schedule can wake a codex here and a Claude Code
// session on a laptop, and the two are then comparable, because everything
// around them is the same code reading the same declaration.
//
// # The shape of the wait
//
// Polling is what a client does when it cannot be called back, and every harness
// worth running one in can do it: run a command, read its output, wake when it
// says something. So the mailbox is served as a long poll rather than as a
// stream or a socket. A poll is held open until there is something to say or
// until the hold expires, which costs one idle connection and no tokens at all -
// the difference between a client that waits and a client that asks again every
// minute, which is a model round per minute for nothing.
//
// One mailbox is one identity. The token in the path IS the identity: it names
// which agent this is, and there is deliberately no way to ask for someone
// else's turns. A deployment that wants ten agents runs ten mailboxes with ten
// tokens, and their records stay apart because nothing joins them.
package mailbox

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
)

// Kinds of delivery a client can be handed. They are the Conversation's own verbs
// - a turn to take, a word said into a turn already running, a request to stop -
// and nothing else: a client that understands these three understands the whole
// protocol.
const (
	KindTurn      = "turn"
	KindSteer     = "steer"
	KindInterrupt = "interrupt"
)

// Delivery is one thing the harness wants the agent to know. It is what a poll
// answers with, as one JSON object on one line, so that a client written in
// shell can read it without a parser.
type Delivery struct {
	// Kind is turn, steer or interrupt.
	Kind string `json:"event"`
	// Thread is the conversation these turns belong to. A client is free to
	// ignore it; it is here so a record on the client's side can be joined to
	// ours.
	Thread string `json:"thread,omitempty"`
	// Turn is what to answer against. Every reply carries it back.
	Turn string `json:"turn"`
	// Text is the prompt for a turn, or what was said into a running one. Empty
	// on an interrupt.
	Text string `json:"text,omitempty"`
	// Model is what the declaration asked this session to run on. A client that
	// cannot choose its model ignores it; one that can is expected to honour it,
	// because the record says which model traded and the arena compares them.
	Model string `json:"model,omitempty"`
	// At is when the harness parked this, not when the poll returned. The gap
	// between the two is how far behind the client is running, and it is worth
	// being able to see.
	At time.Time `json:"at"`
}

// Mailbox is a Conversation whose other end is a client that polls.
//
// It is safe for concurrent use: the harness writes to it from the clock and
// from the room, and the HTTP handler reads from whatever goroutine net/http
// gives it.
type Mailbox struct {
	// Token is the one credential, carried in the path. Empty refuses every
	// request rather than serving an open mailbox: a mailbox with no token is a
	// mistake in the deployment, and the safe reading of a missing secret is
	// never "then anyone".
	Token string
	// Hold is the longest one poll is kept open before it answers "nothing".
	// Long is cheap and short is wasteful, but not unbounded: proxies and
	// gateways close a silent connection, and a client that cannot tell a closed
	// connection from an expired hold retries in a loop. Zero means DefaultHold.
	Hold time.Duration
	// Stale is how long a parked turn may go unclaimed before it is given up on.
	// A turn nobody took is not harmless: the harness believes a session is
	// working, the room shows it working, and the next window is refused because
	// one is already running. Zero means DefaultStale.
	Stale time.Duration
	// Now is the clock, a field so a test is not at the mercy of the wall clock.
	Now func() time.Time
	// Log is where the mailbox says what it dropped and why. Nil is legal and
	// silent.
	Log *zap.Logger

	mu       sync.Mutex
	threadID string
	queue    []Delivery
	// running is the turns the harness believes are in flight, by turn id. A
	// reply against anything else is answered, not acted on: an agent that talks
	// about a turn that ended is a client that fell behind, and treating that as
	// a fresh turn would put words in the record under the wrong cause.
	running map[string]bool
	// arrived is closed and replaced whenever something is queued. A poll waits
	// on the current one, which is how a hold ends the instant there is something
	// to say rather than on the next tick of a timer.
	arrived chan struct{}
	counter int

	// events carries what the client says back to the harness. Buffered because
	// the harness reads it from one goroutine that also does other work, and a
	// reply that blocks an HTTP handler until the harness catches up would make
	// the client look slow when it is not.
	events chan agent.Event
}

const (
	// DefaultHold is a minute and a half: long enough that an idle day costs a
	// handful of connections, short enough that anything in front of us has not
	// yet decided the connection is dead.
	DefaultHold = 90 * time.Second
	// DefaultStale is ten minutes. A trading window is measured in minutes, and
	// a turn taken up ten minutes late would act on a market that has moved -
	// better to say plainly that nobody came than to trade on stale reasoning.
	DefaultStale = 10 * time.Minute

	// eventBuffer is how many replies may be in flight towards the harness.
	// Generous: a chatty turn says a dozen things, and dropping one would lose it
	// from the room and from the record both.
	eventBuffer = 64
)

// New returns a mailbox ready to be given to a harness and served over HTTP.
func New(token string, hold, stale time.Duration, log *zap.Logger) *Mailbox {
	if hold <= 0 {
		hold = DefaultHold
	}
	if stale <= 0 {
		stale = DefaultStale
	}

	return &Mailbox{
		Token:   token,
		Hold:    hold,
		Stale:   stale,
		Now:     time.Now,
		Log:     log,
		running: map[string]bool{},
		arrived: make(chan struct{}),
		events:  make(chan agent.Event, eventBuffer),
	}
}

// Open returns the thread these turns belong to.
//
// There is nothing to open: no process is started and no connection is made, so
// this cannot fail and must not wait. That is deliberate rather than incidental.
// The harness opens the conversation before the room, and a mailbox that waited
// here for a client to show up would hold the whole process closed until someone
// happened to poll - the client is allowed to be absent, that is the point of
// parking work for it.
func (m *Mailbox) Open(_ context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.threadID == "" {
		m.threadID = fmt.Sprintf("mb-%d", m.now().UnixNano())
	}

	return m.threadID, nil
}

// Forget drops the thread so the next Open starts a fresh one.
func (m *Mailbox) Forget() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.threadID = ""
}

// Turn parks a prompt for whoever holds the token and returns at once.
//
// It does not wait for a client to take it. A harness that waited would turn
// every missing client into a stuck clock, and the failure worth having here is
// the visible one: the turn sits, goes stale, and the room is told nobody came.
func (m *Mailbox) Turn(_ context.Context, text, model string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.counter++
	turnID := fmt.Sprintf("%s-t%d", m.threadOrNew(), m.counter)
	m.running[turnID] = true
	m.park(Delivery{
		Kind:   KindTurn,
		Thread: m.threadID,
		Turn:   turnID,
		Text:   text,
		Model:  model,
		At:     m.now(),
	})

	return turnID, nil
}

// Steer says something into a turn already running.
//
// A steer for a turn that is not running is refused rather than promoted to a
// turn of its own. The harness reads that refusal as "start a fresh turn
// instead", which is the behaviour a person expects when they write to a session
// that has already finished: they get an answer, not silence.
func (m *Mailbox) Steer(_ context.Context, turnID, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running[turnID] {
		return fmt.Errorf("no turn %s is running in this mailbox", turnID)
	}

	m.park(Delivery{
		Kind:   KindSteer,
		Thread: m.threadID,
		Turn:   turnID,
		Text:   text,
		At:     m.now(),
	})

	return nil
}

// Interrupt asks the client to stop, and ends the turn here without waiting to
// be told that it did.
//
// Waiting would be wrong in exactly the case interrupt exists for. The harness
// interrupts a turn that has outrun its limit - which is most often a client
// that has died, hung, or gone home - and a mailbox that kept the turn open
// until that client answered would keep the clock blocked on the thing it was
// trying to unblock. So the interrupt is parked for a client that may still be
// listening, and the turn is closed regardless. A reply that arrives afterwards
// is not lost: it is recorded against the turn it names, which by then is a turn
// that ended, and that is the truth of what happened.
func (m *Mailbox) Interrupt(_ context.Context, turnID string) error {
	m.mu.Lock()
	running := m.running[turnID]
	if running {
		m.park(Delivery{
			Kind:   KindInterrupt,
			Thread: m.threadID,
			Turn:   turnID,
			At:     m.now(),
		})
		delete(m.running, turnID)
	}
	m.mu.Unlock()

	if !running {
		return nil
	}

	m.emit(agent.Event{Kind: agent.KindTurnDone, TurnID: turnID})

	return nil
}

// Events is what the client said, in the harness's own vocabulary.
func (m *Mailbox) Events() <-chan agent.Event { return m.events }

// Run gives up on turns nobody came for.
//
// It is a goroutine of its own rather than work done inside a poll, because the
// case that matters is the one where no poll ever arrives. A turn that goes
// stale is announced in the room and then closed: the harness must not be left
// believing a session is at work, and a person watching must not be left reading
// a status line that will never change.
func (m *Mailbox) Run(ctx context.Context) error {
	// A quarter of the staleness, so a turn is given up within a fraction of the
	// bound rather than up to a whole bound late.
	every := m.Stale / 4
	if every < time.Second {
		every = time.Second
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			m.expire()
		}
	}
}

// expire drops what has waited too long and closes the turns it belonged to.
func (m *Mailbox) expire() {
	cutoff := m.now().Add(-m.Stale)

	m.mu.Lock()
	kept := m.queue[:0]
	var dropped []Delivery
	for _, d := range m.queue {
		if d.At.Before(cutoff) {
			dropped = append(dropped, d)
			continue
		}
		kept = append(kept, d)
	}
	m.queue = kept

	var ended []Delivery
	for _, d := range dropped {
		if d.Kind == KindTurn && m.running[d.Turn] {
			delete(m.running, d.Turn)
			ended = append(ended, d)
		}
	}
	m.mu.Unlock()

	for _, d := range ended {
		if m.Log != nil {
			m.Log.Warn("nobody took this turn in time",
				zap.String("turn_id", d.Turn),
				zap.Duration("waited", m.now().Sub(d.At)))
		}
		m.emit(agent.Event{
			Kind:   agent.KindText,
			TurnID: d.Turn,
			Text: fmt.Sprintf("ход никто не забрал за %s — поллер не пришёл, ход закрыт",
				m.Stale.Round(time.Second)),
		})
		m.emit(agent.Event{Kind: agent.KindTurnDone, TurnID: d.Turn})
	}
}

// Pending is how much is waiting to be taken. It exists for the state route and
// for tests, and it is the number an operator looks at first when a client says
// it is connected and the room says nothing is happening.
func (m *Mailbox) Pending() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.queue)
}

// next takes one delivery, waiting up to the hold for one to arrive.
func (m *Mailbox) next(ctx context.Context, hold time.Duration) (Delivery, bool) {
	deadline := time.NewTimer(hold)
	defer deadline.Stop()

	for {
		m.mu.Lock()
		if len(m.queue) > 0 {
			d := m.queue[0]
			m.queue = m.queue[1:]
			m.mu.Unlock()

			return d, true
		}
		waiting := m.arrived
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			return Delivery{}, false
		case <-deadline.C:
			return Delivery{}, false
		case <-waiting:
		}
	}
}

// park adds a delivery and wakes every poll waiting. Called under the lock.
func (m *Mailbox) park(d Delivery) {
	m.queue = append(m.queue, d)
	close(m.arrived)
	m.arrived = make(chan struct{})
}

// emit hands the harness one event, dropping it rather than blocking if the
// harness has stopped reading. A blocked HTTP handler would look to the client
// like a mailbox that refuses replies, which is a worse failure than a lost line
// in a room nobody is watching any more.
func (m *Mailbox) emit(ev agent.Event) {
	select {
	case m.events <- ev:
	default:
		if m.Log != nil {
			m.Log.Warn("reply dropped: the harness is behind", zap.String("kind", string(ev.Kind)))
		}
	}
}

func (m *Mailbox) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}

	return m.Now()
}

// threadOrNew names the thread, opening one if the caller never did. Called
// under the lock.
func (m *Mailbox) threadOrNew() string {
	if m.threadID == "" {
		m.threadID = fmt.Sprintf("mb-%d", m.now().UnixNano())
	}

	return m.threadID
}

// said is what a client posts back when the agent has spoken.
type said struct {
	Turn string `json:"turn"`
	Text string `json:"text"`
}

// finished is what a client posts back when the turn is over. Failure is for a
// turn that ended badly: it is said in the room rather than swallowed, because a
// session that failed silently is indistinguishable from one that decided to do
// nothing, and those two need different answers from a person.
type finished struct {
	Turn    string `json:"turn"`
	Failure string `json:"failure,omitempty"`
}

// Handler serves one mailbox under base, which must begin and end with a slash.
//
//	GET  {base}{token}/poll?wait=90  one delivery, or 204 when the hold expires
//	POST {base}{token}/say           {"turn":"...","text":"..."}
//	POST {base}{token}/done          {"turn":"...","failure":"..."}
//	GET  {base}{token}/state         what is waiting, for an operator
//
// The token travels in the path rather than in a header on purpose: the client
// that most needs this is a shell script holding a URL, and a URL that works
// with a bare curl is a URL that works everywhere. It is a credential all the
// same - it is not logged, and a wrong one is answered exactly the way an
// address that does not exist is answered, so that guessing tells the guesser
// nothing.
func (m *Mailbox) Handler(base string) http.Handler {
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}

	mux := http.NewServeMux()
	mux.HandleFunc(base, func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, base)
		token, action, ok := strings.Cut(rest, "/")
		if !ok || !m.authentic(token) {
			http.NotFound(w, r)

			return
		}

		switch {
		case action == "poll" && r.Method == http.MethodGet:
			m.servePoll(w, r)
		case action == "say" && r.Method == http.MethodPost:
			m.serveSay(w, r)
		case action == "done" && r.Method == http.MethodPost:
			m.serveDone(w, r)
		case action == "state" && r.Method == http.MethodGet:
			m.serveState(w)
		default:
			http.Error(w, "unknown request for this mailbox", http.StatusNotFound)
		}
	})

	return mux
}

// authentic compares in constant time and refuses an empty token outright.
func (m *Mailbox) authentic(given string) bool {
	if m.Token == "" || given == "" {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(given), []byte(m.Token)) == 1
}

func (m *Mailbox) servePoll(w http.ResponseWriter, r *http.Request) {
	hold := m.Hold
	if raw := r.URL.Query().Get("wait"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 0 {
			http.Error(w, "wait is a whole number of seconds", http.StatusBadRequest)

			return
		}
		// Asked-for shorter is honoured; asked-for longer is not. The bound
		// belongs to the side holding the connection open.
		if asked := time.Duration(seconds) * time.Second; asked < hold {
			hold = asked
		}
	}

	d, ok := m.next(r.Context(), hold)
	if !ok {
		// Nothing to say. Not an error and not an empty object: a client must be
		// able to tell "the hold expired" from "here is a turn with no text"
		// without parsing anything.
		w.WriteHeader(http.StatusNoContent)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	// One object, one line. A shell client reads a line; a program reads JSON.
	if err := json.NewEncoder(w).Encode(d); err != nil && m.Log != nil {
		m.Log.Warn("could not hand over a delivery", zap.Error(err))
	}
}

func (m *Mailbox) serveSay(w http.ResponseWriter, r *http.Request) {
	var body said
	if err := decode(r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	if body.Turn == "" || strings.TrimSpace(body.Text) == "" {
		http.Error(w, "say needs a turn and a text", http.StatusBadRequest)

		return
	}

	m.emit(agent.Event{Kind: agent.KindText, TurnID: body.Turn, Text: body.Text})
	w.WriteHeader(http.StatusAccepted)
}

func (m *Mailbox) serveDone(w http.ResponseWriter, r *http.Request) {
	var body finished
	if err := decode(r, &body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}
	if body.Turn == "" {
		http.Error(w, "done needs a turn", http.StatusBadRequest)

		return
	}

	m.mu.Lock()
	was := m.running[body.Turn]
	delete(m.running, body.Turn)
	m.mu.Unlock()

	// Ending a turn that already ended is not an error. A client that lost its
	// connection and repeated itself is doing the right thing, and answering it
	// with a failure would teach it to stop repeating.
	if !was {
		w.WriteHeader(http.StatusAccepted)

		return
	}

	if body.Failure != "" {
		m.emit(agent.Event{
			Kind:   agent.KindText,
			TurnID: body.Turn,
			Text:   "ход закончился ошибкой: " + body.Failure,
		})
	}
	m.emit(agent.Event{Kind: agent.KindTurnDone, TurnID: body.Turn})
	w.WriteHeader(http.StatusAccepted)
}

func (m *Mailbox) serveState(w http.ResponseWriter) {
	m.mu.Lock()
	state := struct {
		Thread  string   `json:"thread"`
		Pending int      `json:"pending"`
		Running []string `json:"running"`
		Hold    string   `json:"hold"`
		Stale   string   `json:"stale"`
	}{
		Thread:  m.threadID,
		Pending: len(m.queue),
		Running: make([]string, 0, len(m.running)),
		Hold:    m.Hold.String(),
		Stale:   m.Stale.String(),
	}
	for turnID := range m.running {
		state.Running = append(state.Running, turnID)
	}
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(state)
}

// bodyLimit bounds what a client may post. What comes back from a session is a
// sentence or two for the room; a megabyte of it is a mistake somewhere, and a
// mistake that fills memory is worse than one that is refused.
const bodyLimit = 1 << 20

func decode(r *http.Request, into any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, bodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		if errors.Is(err, http.ErrBodyReadAfterClose) {
			return errors.New("the body ended early")
		}

		return fmt.Errorf("read the body: %w", err)
	}

	return nil
}
