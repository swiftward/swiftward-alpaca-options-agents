// Package harness holds the clock and the room. It decides WHEN a session works
// and says why it woke it; it never decides what to trade - that is the session's
// job, and the autonomy requirement rests on the difference.
//
// Two causes reach a session: the schedule in the declaration, and a person
// writing in the chat. A person who writes while the session is working is not
// made to wait - the message goes into the turn already running.
package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

// clockTick is how often the schedule is asked whether anything is due, and how
// often what the session reads is brought level with what is on disk. The
// declaration speaks in minutes, so asking more often would only repeat the same
// question. Harness.TickEvery overrides it, which is how a test proves that an
// edit reaches a running harness without standing there for a minute.
const clockTick = time.Minute

// turnWatch is how often the running turn is measured against its limit. The
// check reads one field under a lock, so it costs nothing to do often.
const turnWatch = time.Second

// typingRefresh is how often the typing cue is renewed; Telegram drops it after
// about five seconds.
const typingRefresh = 4 * time.Second

// Chat is the room the session works in. The agent never holds this - the
// harness posts what the session said and passes on what a person wrote.
type Chat interface {
	Listen(ctx context.Context) error
	Inbound() <-chan telegram.Message
	Send(ctx context.Context, text string) (int, error)
	Edit(ctx context.Context, messageID int, text string) error
	Delete(ctx context.Context, messageID int) error
	Typing(ctx context.Context) error
}

// Wakeups holds what the session asked to be woken for. Nil means the session
// was offered no way to ask.
type Wakeups interface {
	Due(now time.Time, price map[string]float64) []wakeup.Wakeup
	Watching() []string
}

// Prices reads the last trade of the symbols a wake-up is watching. The harness
// reads them only to know when to wake a session; it decides nothing from them.
type Prices interface {
	LastTrades(ctx context.Context, symbols []string) (map[string]float64, error)
}

// Conversation is one thread with the agent, held open across turns.
type Conversation interface {
	Open(ctx context.Context) (threadID string, err error)
	// Forget drops the remembered thread so the next Open starts a fresh one.
	// Needed because a thread can outlive the tools it was opened with: the agent
	// reads the list of tools ONCE per connection, so a server that blinks is
	// gone for the rest of that thread - and resuming it carries the gap forward
	// across restarts of this process too.
	Forget()
	Turn(ctx context.Context, text, model string) (turnID string, err error)
	Steer(ctx context.Context, turnID, text string) error
	Interrupt(ctx context.Context, turnID string) error
	Events() <-chan agent.Event
}

// Commands a person can type instead of talking to the session.
const (
	CommandStop = "/stop"
)

type Harness struct {
	Chat         Chat
	Conversation Conversation
	// Declaration names the sessions the clock wakes. Nil means the clock wakes
	// nobody and the chat is the only cause. It is the schedule the harness
	// starts with; what is in force later comes from Reread.
	Declaration *declaration.Declaration
	// Reread hands back the declaration as it stands on disk, or the one already
	// in force together with the reason the file could not replace it. Nil means
	// the schedule is read once and only a restart can change it.
	Reread func() (*declaration.Declaration, error)
	// Record is where a turn, a tool call and an intent are written down. Nil
	// means nothing is recorded, which is legal only in a test.
	Record record.Keeper
	// Wakeups are the session's own standing requests. Nil means it has none and
	// was offered no way to make them.
	Wakeups Wakeups
	// Prices answers what a price wake-up is watching for. Nil means price
	// wake-ups cannot fire, and the session is told so when it tries to set one.
	Prices Prices
	// DefaultModel is what a session runs on when the declaration names none. It
	// is recorded with the turn, so the record can answer which model traded.
	DefaultModel string
	// Now is the clock. It is a field so a test is not at the mercy of the wall
	// clock, and so the only place a time comes from is visible.
	Now func() time.Time
	// SayEvery is how often the room hears the session think out loud. A session
	// says several things per turn and thirteen of them run all day; Telegram
	// answers a chatty bot with "too many requests" and the answer that mattered
	// waits behind the chatter. What is held back is not lost: the last thing said
	// is always posted when the turn ends, and everything is in the record.
	SayEvery time.Duration
	// TurnLimit bounds how long one turn may run before it is interrupted. The
	// sessions behind it are waiting: a scan that outlives its window starves the
	// defence, and a defence that never runs is worse than a scan that never
	// finishes. Zero means a turn runs until the agent ends it.
	TurnLimit time.Duration
	// CallTimeout bounds one request to the agent. The loop that reads the chat is
	// the loop that talks to the agent, so an unbounded call takes the room down
	// with the session.
	CallTimeout time.Duration
	// TickEvery is how often the schedule is asked whether anything is due, and
	// how often the declaration and the skills are brought level with what is on
	// disk. Zero means clockTick, which is what every deployment uses; a test sets
	// it rather than standing there for a minute.
	//
	// A tick costs no network, but it is NOT free: unless RereadEvery says
	// otherwise it also re-reads the declaration and re-stamps the skill tree,
	// which reads every skill file twice and parses the mount table. Lower this
	// without lowering that and the cost follows the tick, sixty times a minute
	// at a second.
	TickEvery time.Duration
	// RereadEvery is how often the declaration and the skills are brought level
	// with the disk. Zero means every tick, which is what this did before the
	// three were told apart.
	//
	// It is separate because it is the only one of the three that touches the
	// filesystem, and its cost grows with the skill tree rather than with
	// anything the schedule needs. A minute here and a second in TickEvery is the
	// combination that buys accuracy without buying the reading.
	RereadEvery time.Duration
	// PriceEvery is how often the prices a wake-up watches are read. Zero means
	// every tick, which is what this ran on before the three were separated.
	//
	// They were separated because they cost different things and were tied
	// together for no reason but that one loop did both. Asking the schedule
	// costs nothing - it is a map in memory. Asking the market costs a request
	// out of two hundred a minute, shared with the screener, the ladder and the
	// agent itself; measured on 27 August at a budget of 180, 133 of 187 calls
	// came back refused and the defence twice reported the gateway down.
	//
	// So accuracy of the clock is now free and accuracy of the price is now
	// priced, and an operator can buy the first without paying for the second.
	PriceEvery time.Duration
	Log        *zap.Logger

	mu          sync.Mutex
	lastSaid    time.Time
	heldBack    string
	threadID    string
	turnID      string
	statusID    int
	turnFor     string
	turnStarted time.Time
	// finishedBeforePublished is a turn the agent finished before this process
	// had its id in hand. The id is known only when Conversation.Turn returns, and
	// a turn short enough to end first would otherwise have its completion
	// dropped as belonging to nobody - leaving a finished turn marked as running,
	// every session queued behind it, and the record's row never closed. Only one
	// turn can be opening at a time, so one name is enough.
	finishedBeforePublished string
	// opening is true from the moment a goroutine claims the right to start a
	// turn until that turn has an id. Without it the clock and the room both read
	// an empty turnID, both find nobody working, and both start one - two
	// sessions trading one account, the first of them orphaned the moment the
	// second overwrites turnID. The check for a running turn is in three places
	// and cannot be atomic there; claiming is atomic here.
	opening bool
	// brokerCalled records whether THIS turn reached the broker at all, and
	// brokerless counts the turns in a row that did not. A session that cannot
	// see prices or positions still wakes, still reasons and still reports - it
	// simply does nothing, and nothing is what a broken tool list looks like from
	// the outside. See noteBrokerReach.
	brokerCalled bool
	brokerless   int

	// turnActed records whether the agent said or called ANYTHING this turn. A
	// turn that ends without either did not run - the model was unreachable, the
	// thread was refused - and must be marked failed rather than counted done.
	turnActed bool
	// declared is the schedule in force. The clock is the only thing that replaces
	// it; the lock is here because it is also read from outside that goroutine.
	// Everything else that wants the schedule - the session's own read_schedule -
	// asks the watcher rather than the harness.
	declared *declaration.Declaration
	lastRun  map[string]time.Time
	// lastPricePoll is when prices were last read, and lastReread is when the
	// declaration was last brought level with the disk. Both are touched only by
	// the clock goroutine, so they need no lock - and taking one would say the
	// opposite.
	lastPricePoll time.Time
	lastReread    time.Time
	// refusedInterrupts counts how many times in a row the agent has refused to
	// give up an overrunning turn. It resets the moment one is given up.
	refusedInterrupts int
}

// Run holds both causes until ctx ends. With neither a declaration nor a chat it
// refuses to start: a harness that runs while waking nobody looks exactly like a
// working one.
func (h *Harness) Run(ctx context.Context) error {
	// Three things can wake a session: the schedule, a person in the chat, and the
	// session's own standing request from an earlier run. With none of them the
	// harness would run while waking nobody, which looks exactly like working.
	if h.Declaration == nil && h.Chat == nil && h.Wakeups == nil {
		return fmt.Errorf("the harness has no cause to wake a session: set DECLARATION, configure the chat, allow wake-ups, or run neither role")
	}
	if h.CallTimeout <= 0 {
		return fmt.Errorf("the harness needs a bound on one call to the agent: set AGENT_CALL_TIMEOUT")
	}
	if h.Now == nil {
		return fmt.Errorf("the harness has no clock")
	}
	h.mu.Lock()
	h.declared = h.Declaration
	h.mu.Unlock()
	h.lastRun = h.whatAlreadyRanToday(ctx, h.Declaration)

	// Followed whether or not anybody is watching the chat: this is the loop that
	// closes a turn in the record, and the record is read long after the room.
	go h.followTheSession(ctx)
	if h.Declaration != nil || h.Wakeups != nil {
		go h.keepTheClock(ctx)
	}
	// The running turn is watched on its own, much faster clock: the schedule
	// ticks once a minute, and a turn that has overrun should not wait most of a
	// minute to be noticed while other sessions queue behind it.
	if h.TurnLimit > 0 {
		go h.watchTheRunningTurn(ctx)
	}
	if h.Declaration != nil {
		h.Log.Info("clock held",
			zap.String("declaration", h.Declaration.Name),
			zap.Int("sessions", len(h.Declaration.Sessions)),
			zap.String("timezone", h.Declaration.Location().String()))
	}

	if h.Chat == nil {
		<-ctx.Done()
		return nil
	}

	return h.serveChat(ctx)
}

// whatAlreadyRanToday reads which sessions have already run since midnight, so a
// restart inside a session's window does not run that session a second time. A
// record that cannot be read is not fatal: the harness says so and starts with
// nothing, which is the state it had before this existed.
func (h *Harness) whatAlreadyRanToday(ctx context.Context, declared *declaration.Declaration) map[string]time.Time {
	if h.Record == nil || declared == nil {
		return map[string]time.Time{}
	}

	now := h.Now().In(declared.Location())
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	last, err := h.Record.LastRuns(ctx, midnight)
	if err != nil {
		h.Log.Error("could not read which sessions already ran today; a session in its window may run again",
			zap.Error(err))
		return map[string]time.Time{}
	}
	// A turn is also woken by a person and by a wake-up, and those names are not
	// session names. Keeping them would let a chat name that happens to match a
	// session silence that session for the day.
	ran := map[string]time.Time{}
	for i := range declared.Sessions {
		name := declared.Sessions[i].Name
		if at, found := last[name]; found {
			ran[name] = at
		}
	}
	if len(ran) > 0 {
		h.Log.Info("sessions that already ran today", zap.Int("sessions", len(ran)))
	}

	return ran
}

// keepTheClock wakes the sessions the declaration names. It checks once a minute
// because the declaration speaks in minutes; a finer tick would only ask the same
// question more often.
func (h *Harness) keepTheClock(ctx context.Context) {
	ticker := time.NewTicker(h.tickCadence())
	defer ticker.Stop()

	h.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.tick(ctx)
		}
	}
}

func (h *Harness) tick(ctx context.Context) {
	if h.rereadDue() {
		h.rereadTheDeclaration(ctx)
	}
	h.fireWakeups(ctx)
	h.fireDue(ctx)
}

// rereadTheDeclaration puts a changed schedule in force between one tick and the
// next.
//
// What it has to be careful about is the memory of what already ran. That memory
// is kept by session NAME and starts from the record, so replacing the
// declaration without touching it would be wrong twice over: every name would
// keep a time this process happens to hold, and every name the new declaration
// added would carry none at all - which reads as "never ran".
//
// So the record is asked again, and what it says fills in the names that are
// new, and only those. A name this process has already woken keeps the time this
// process saw: that is later than anything the record can offer, because the
// record is asked only about today and this process knows what it did minutes
// ago. A name that has gone from the declaration keeps its entry too - it costs
// a map entry, and dropping it would let a session removed and put back within
// the same day run twice.
//
// What this CANNOT do is tell a renamed session from an added one. A name the
// record has never seen has never run under that name, and both a session added
// at midday and a session renamed at midday look exactly like that. The added
// one should run, so it does. Renaming a session inside its own window still
// runs it twice, and the way to avoid that is to rename outside the window -
// there is nothing on disk that would tell the two apart.
func (h *Harness) rereadTheDeclaration(ctx context.Context) {
	if h.Reread == nil {
		return
	}

	fresh, err := h.Reread()
	if err != nil {
		// The error names what actually stopped it, which is not always the
		// declaration: putting one in force lays out the skills it names, so a
		// half-written SKILL.md refuses the re-read too. Saying "the declaration is
		// broken" would point whoever reads this at the wrong file.
		h.Log.Error("the declaration on disk could not be put in force; the schedule in force is unchanged",
			zap.Error(err))
		return
	}
	if fresh == h.inForce() {
		return
	}

	h.mu.Lock()
	h.declared = fresh
	h.mu.Unlock()
	for name, at := range h.whatAlreadyRanToday(ctx, fresh) {
		if _, known := h.lastRun[name]; !known {
			h.lastRun[name] = at
		}
	}
	h.Log.Info("the declaration changed and the new one is in force",
		zap.String("declaration", fresh.Name),
		zap.Int("sessions", len(fresh.Sessions)))
}

// watchTheRunningTurn ends a turn that has outlived its limit.
func (h *Harness) watchTheRunningTurn(ctx context.Context) {
	ticker := time.NewTicker(turnWatch)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.endAnOverrunningTurn(ctx)
		}
	}
}

// endAnOverrunningTurn interrupts a turn that has outlived its limit. What it
// was doing is not lost: whatever it already sent to the broker stands, and the
// record says the turn was cut and why.
func (h *Harness) endAnOverrunningTurn(ctx context.Context) {
	if h.TurnLimit <= 0 {
		return
	}

	h.mu.Lock()
	turnID, started := h.turnID, h.turnStarted
	h.mu.Unlock()
	if turnID == "" || started.IsZero() {
		return
	}

	ran := h.Now().Sub(started)
	if ran <= h.TurnLimit {
		return
	}

	h.Log.Warn("a turn outlived its limit and is being interrupted",
		zap.String("turn_id", turnID), zap.Duration("ran", ran), zap.Duration("limit", h.TurnLimit))

	agentCtx, done := h.boundToAgent(ctx)
	err := h.Conversation.Interrupt(agentCtx, turnID)
	done()
	// "There is no turn to interrupt" is an answer, not a fault: the turn ended
	// between the measurement and the call. Treating it as a fault is what wedges
	// the harness - it keeps believing a turn runs, so every session queues behind
	// a turn that finished, and the watcher asks again every second forever.
	if err != nil && !errors.Is(err, agent.ErrNoActiveTurn) {
		h.mu.Lock()
		h.refusedInterrupts++
		refused := h.refusedInterrupts
		h.mu.Unlock()

		h.Log.Error("could not interrupt the overrunning turn",
			zap.Error(err), zap.Int("refused", refused), zap.Int("of", interruptAttempts))

		// Politeness has a limit, and past it the harness is the thing standing in
		// the way. A turn that will not be interrupted holds every session behind
		// it: on 26 August one held them for two hours and nineteen minutes, and
		// the only reason it ended was a person restarting the container.
		//
		// So the last resort is to stop being the one who waits. Dying costs the
		// conversation and about ten seconds; not dying costs the rest of the day.
		if refused >= interruptAttempts {
			h.Log.Fatal("the agent will not give up the turn: ending the process so it can be started again",
				zap.String("turn_id", turnID), zap.Duration("ran", ran))
		}
		return
	}

	h.mu.Lock()
	h.refusedInterrupts = 0
	h.mu.Unlock()
	if err != nil {
		h.Log.Info("the overrunning turn had already ended", zap.String("turn_id", turnID))
	}

	if h.Record != nil {
		if err := h.Record.TurnFinished(ctx, turnID, h.Now(), overranFailure); err != nil {
			h.Log.Error("could not close the interrupted turn in the record", zap.Error(err))
		}
	}
	h.say(ctx, "the turn ran longer than it was given and was stopped: other sessions are waiting behind it")
	h.clearTurn(ctx)
}

// overranFailure is what the record says about a turn the harness cut short.
const overranFailure = "the turn outlived its limit and was interrupted so the sessions behind it could run"

// fireWakeups wakes the session for what it asked itself. Its own requests come
// first: a session that asked to be woken when price crossed a level asked
// because something needs doing before the next scheduled hour.
func (h *Harness) fireWakeups(ctx context.Context) {
	if h.Wakeups == nil {
		return
	}

	var prices map[string]float64
	// A tick that skips the read still fires the wake-ups that wait for a TIME:
	// an empty price map is not a failure, it is simply a tick that asked the
	// market nothing. A wake-up waiting for a price waits one more tick.
	//
	// The order of this condition is load-bearing; priceDue says why.
	if watching := h.Wakeups.Watching(); len(watching) > 0 && h.Prices != nil && h.priceDue() {
		read, done := h.boundToAgent(ctx)
		last, err := h.Prices.LastTrades(read, watching)
		done()
		if err != nil {
			h.Log.Error("could not read the prices a wake-up is watching",
				zap.Strings("symbols", watching), zap.Error(err))
		}
		prices = last

		// The cadence goes into the line that reports the read, so that a day
		// later the question "was the finer tick worth it" is answered by the
		// record instead of by memory. It is written at Info on purpose: Debug is
		// off on every deployment we have, and a line nobody keeps answers
		// nothing. At the default cadence this is one line a minute.
		if err == nil {
			h.Log.Info("read the prices a wake-up watches",
				zap.Strings("symbols", watching),
				zap.Duration("price_every", h.priceCadence()),
				zap.Int("read", len(last)))
		}
	}

	for _, due := range h.Wakeups.Due(h.Now(), prices) {
		h.Log.Info("waking the session for its own reason",
			zap.String("wakeup", due.ID), zap.String("cause", string(due.Cause)))
		h.Tell(ctx, due.Prompt(), "wakeup "+due.ID)
	}
}

// Tell puts something in front of the session now. A turn already running takes
// it as another instruction; otherwise it becomes a turn of its own.
//
// Starting a second turn beside a running one is what this exists to prevent: it
// would leave the first orphaned - never closed in the record, never closed in
// the chat - with two turns on one conversation.
func (h *Harness) Tell(ctx context.Context, prompt, who string) {
	stale := ""
	if turnID := h.runningTurn(); turnID != "" {
		agentCtx, done := h.boundToAgent(ctx)
		err := h.Conversation.Steer(agentCtx, turnID, prompt)
		done()
		if err == nil {
			h.Log.Info("said into the running turn",
				zap.String("who", who), zap.String("turn_id", turnID))
			return
		}
		// The turn ended between the check and the call: nothing is lost, this
		// becomes its own turn below.
		h.Log.Info("could not reach the running turn, starting a new one",
			zap.String("who", who), zap.Error(err))
		stale = turnID
	}
	if h.startTurnReplacing(ctx, prompt, who, "", stale) {
		return
	}
	// Another cause was opening a turn at that moment. A wake-up is consumed when
	// it fires, so dropping this would lose it for good: wait for that turn's id
	// and put the prompt in front of the same session instead.
	if h.steerWhenReady(ctx, prompt, who) {
		return
	}
	// The cause that won the claim never started a turn, so there is nothing to
	// say this into and nothing holding the place. One more attempt, and this
	// time the claim is free.
	if h.startTurnWith(ctx, prompt, who, "") {
		return
	}
	h.Log.Error("nothing carried this: no turn started and none took it",
		zap.String("who", who), zap.String("prompt", firstLine(prompt)))
}

// steerWhenReady waits for the turn another cause is opening and says this into
// it. It reports whether the prompt was delivered.
//
// The wait is bounded by the same limit one call to the agent gets: past that,
// whatever is opening the turn is not going to finish opening it, and a caller
// waiting longer would hold the clock's goroutine.
func (h *Harness) steerWhenReady(ctx context.Context, prompt, who string) bool {
	deadline := time.Now().Add(h.CallTimeout)
	for time.Now().Before(deadline) {
		// Nobody opening and nobody running means the cause that won the claim
		// never got a turn - the agent refused it, or the conversation would not
		// open. Waiting out the deadline for a turn that will not arrive costs the
		// caller its prompt; the caller is told at once and starts one itself.
		if turnID, opening := h.turnState(); turnID == "" && !opening {
			return false
		} else if turnID != "" {
			agentCtx, done := h.boundToAgent(ctx)
			err := h.Conversation.Steer(agentCtx, turnID, prompt)
			done()
			if err == nil {
				h.Log.Info("said into the turn another cause had just started",
					zap.String("who", who), zap.String("turn_id", turnID))

				return true
			}
			h.Log.Info("could not reach the turn another cause started",
				zap.String("who", who), zap.Error(err))

			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(50 * time.Millisecond):
		}
	}

	return false
}

// inForce is the declaration the clock is waking by right now.
//
// Before Run has taken it up it falls back to the one the harness was built
// with, because a turn can be started before then and from outside the clock:
// the ladder wakes a session when an order fills, and that turn needs the same
// numbers in front of it as any other.
//
// Read under the lock: the clock is the only writer, but the readers are the
// chat's goroutine, the ladder's and a test's.
func (h *Harness) inForce() *declaration.Declaration {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.declared != nil {
		return h.declared
	}

	return h.Declaration
}

// priceDue says whether this tick should read prices, and remembers that it did.
//
// It HAS a side effect, and the place it is called from matters: it must be
// reached only on ticks where a price would actually be read. Moving it ahead of
// the "is anyone watching" test would advance the cadence through a quiet
// afternoon, and the first price wake-up set after that would then wait a whole
// cadence before anybody looked.
func (h *Harness) priceDue() bool {
	return elapsed(&h.lastPricePoll, h.Now(), h.PriceEvery)
}

// rereadDue says whether this tick should bring the declaration and the skills
// level with the disk.
func (h *Harness) rereadDue() bool {
	return elapsed(&h.lastReread, h.Now(), h.RereadEvery)
}

// elapsed reports whether every has passed since last, and records now when it
// has. Zero means "every time", which is how an unset interval keeps the
// behaviour this had before the intervals existed.
//
// A clock that steps BACKWARDS reads as due rather than as not-yet. The other
// way round is a trap: the difference goes negative, the caller is told to wait,
// and the stamp is never moved - so a jump back of an hour would stop the
// reading for an hour. Nothing does that today, because Now is time.Now and its
// monotonic reading survives NTP; virtual clocks are exactly where it would.
func elapsed(last *time.Time, now time.Time, every time.Duration) bool {
	if every <= 0 {
		return true
	}
	// The first call is due: a harness that has just started knows nothing at
	// all, and waiting out a cadence before the first read would leave a standing
	// price wake-up blind for that long after every restart.
	if !last.IsZero() && !now.Before(*last) && now.Sub(*last) < every-slack(every) {
		return false
	}
	*last = now

	return true
}

// slack is how early a tick may be and still count as the cadence having
// passed.
//
// Without it a cadence that is a MULTIPLE of the tick lands on the boundary and
// loses: measured live on 29 August with a five-second tick and a ten-second
// cadence, the tick arriving ten seconds later arrived 9.995 of them later, was
// told to wait, and the read happened on the tick after - so a cadence written
// as ten seconds ran at fifteen, and did it every other time.
//
// The tolerance is a fraction of the cadence rather than a fixed number so that
// it stays sane at both ends: a hundredth of a millisecond of it at ten seconds,
// and still only a second at a hundred. A tick early by more than that is not
// jitter, it is a different tick.
func slack(every time.Duration) time.Duration {
	return every / 64
}

// priceCadence is how often prices are read, said in a way that is true in a log
// line: an unset PriceEvery means every tick, and the tick has a length.
func (h *Harness) priceCadence() time.Duration {
	if h.PriceEvery > 0 {
		return h.PriceEvery
	}

	return h.tickCadence()
}

// tickCadence is TickEvery, or the minute this program used before it was a
// parameter.
func (h *Harness) tickCadence() time.Duration {
	if h.TickEvery > 0 {
		return h.TickEvery
	}

	return clockTick
}

func (h *Harness) fireDue(ctx context.Context) {
	declared := h.inForce()
	if declared == nil {
		return
	}
	now := h.Now().In(declared.Location())

	for i := range declared.Sessions {
		session := &declared.Sessions[i]
		if !session.Due(now, h.lastRun[session.Name]) {
			continue
		}
		if turnID := h.runningTurn(); turnID != "" {
			// Two sessions on one account close each other's positions. The due
			// session is not lost: it stays due until the running turn ends.
			h.Log.Info("session is due but the agent is working",
				zap.String("session", session.Name),
				zap.String("turn_id", turnID))
			return
		}

		h.Log.Info("waking a session", zap.String("session", session.Name), zap.String("cause", session.Cause))
		if !h.startTurnWith(ctx, session.Prompt(), session.Name, session.Model) {
			// Marked as run only when it ran. A session recorded against a turn
			// that never started would be skipped for the rest of the day.
			h.Log.Info("session is due but another cause was starting a turn; it stays due",
				zap.String("session", session.Name))

			return
		}
		h.lastRun[session.Name] = now
	}
}

// serveChat carries the room, and losing the room does not stop the work.
//
// The chat is where the agent reports; the market is where it acts. Returning
// the listener's error from here ended the whole process, so a long poll that
// timed out took the ladder, the screener and the schedule down with it - and
// the restart landed in the middle of orders the ladder was still walking. Seen
// on 26 August: "context deadline exceeded" out of the listener, and every role
// died for it.
//
// So the error is said once, loudly, and everything else keeps running. An
// operator who wants the room back restarts the process deliberately, which is
// a much smaller thing than having it restarted for them mid-order.
func (h *Harness) serveChat(ctx context.Context) error {
	listening := make(chan error, 1)
	go func() { listening <- h.Chat.Listen(ctx) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-listening:
			if err != nil && ctx.Err() == nil {
				h.Log.Error("the room is gone; trading continues without it", zap.Error(err))
			}
			<-ctx.Done()

			return nil
		case msg, ok := <-h.Chat.Inbound():
			if !ok {
				return nil
			}
			h.handle(ctx, msg)
		}
	}
}

func (h *Harness) handle(ctx context.Context, msg telegram.Message) {
	if strings.TrimSpace(msg.Text) == CommandStop {
		h.stop(ctx)
		return
	}

	stale := ""
	if turnID := h.runningTurn(); turnID != "" {
		agentCtx, done := h.boundToAgent(ctx)
		err := h.Conversation.Steer(agentCtx, turnID, textOf(msg))
		done()
		if err != nil {
			// The turn ended between the check and the call. Nothing is lost: the
			// message becomes the next turn instead of vanishing into a finished one.
			h.Log.Info("could not reach the running turn, starting a new one", zap.Error(err))
			stale = turnID
		} else {
			h.Log.Info("message went into the running turn", zap.String("turn_id", turnID))
			return
		}
	}

	h.startTurn(ctx, msg, stale)
}

func (h *Harness) startTurn(ctx context.Context, msg telegram.Message, stale string) {
	prompt := promptFor(msg)
	if h.startTurnReplacing(ctx, prompt, msg.Username, "", stale) {
		return
	}
	// A person's message is never dropped for a race: the clock was starting a
	// turn at that same moment, so this goes into that one.
	if h.steerWhenReady(ctx, prompt, msg.Username) {
		return
	}
	// That turn never started either, so the place is free now.
	if h.startTurnWith(ctx, prompt, msg.Username, "") {
		return
	}
	h.say(ctx, "a session was starting just as you wrote, and this did not reach it - say it again")
}

// numbersInFront puts the agent's own numbers ahead of whatever woke it.
//
// Every turn gets them, not only a scheduled one: a session woken by its own
// price wake-up or by a person in the chat is asked to follow the same
// techniques, and a skill that finds its numbers missing is told to open
// nothing. Put here rather than in the declaration's own prompt so that all
// three causes go through one place and none of them can be forgotten.
func (h *Harness) numbersInFront(prompt string) string {
	declared := h.inForce()
	if declared == nil {
		return prompt
	}
	numbers := declared.Numbers()
	if numbers == "" {
		return prompt
	}

	return numbers + "\n" + prompt
}

// startTurnWith runs one turn. model names the model this particular turn is
// worth: a session that only reads the news does not need the one that trades.
// turnState is the running turn and whether one is being opened right now.
func (h *Harness) turnState() (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.turnID, h.opening
}

// claim takes the right to start a turn, or reports that somebody else holds it.
// The right is given up either by release or by the turn getting an id.
//
// stale is the turn the caller has just PROVEN is over - the agent answered
// "turn already finished" to a steer. That one may be taken over; any other
// running turn may not, because starting beside a live turn is what this exists
// to prevent.
func (h *Harness) claim(stale string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.opening {
		return false
	}
	if h.turnID != "" && h.turnID != stale {
		return false
	}
	h.opening = true

	return true
}

func (h *Harness) release() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.opening = false
}

// startTurnWith runs one turn and reports whether it started one. A caller that
// is told false has NOT had its prompt delivered - it was neither started nor
// steered into a running turn - and must decide what to do with it.
func (h *Harness) startTurnWith(ctx context.Context, prompt, who, model string) bool {
	return h.startTurnReplacing(ctx, prompt, who, model, "")
}

// startTurnReplacing is startTurnWith for a caller the agent has just told that
// the turn the harness still holds is over: that one, and only that one, may be
// taken over.
func (h *Harness) startTurnReplacing(ctx context.Context, prompt, who, model, stale string) bool {
	// The cause is read off what WOKE the session, before the agent's numbers go
	// in front of it. They open with a fixed sentence, so taking the first line
	// afterwards would file every turn in the record under that sentence instead
	// of under the window, the wake-up or the message that caused it - and the
	// record is what answers "why did this happen" long after the room has
	// scrolled away.
	if !h.claim(stale) {
		h.Log.Info("another cause is already starting a turn; this one is not started",
			zap.String("who", who))

		return false
	}
	// Given up here in every case except the one that matters: once the turn has
	// an id, turnID holds the place instead and this only clears a flag.
	defer h.release()

	woke := prompt
	prompt = h.numbersInFront(prompt)

	agentCtx, done := h.boundToAgent(ctx)
	threadID, err := h.openThread(agentCtx)
	done()
	if err != nil {
		h.Log.Error("could not open a conversation with the agent", zap.Error(err))
		h.say(ctx, "could not start a conversation with the agent: "+err.Error())

		return false
	}

	status, err := h.status(ctx, working(who, ""))
	if err != nil {
		h.Log.Error("could not open a status line in the chat", zap.Error(err))
	}

	agentCtx, done = h.boundToAgent(ctx)
	turnID, err := h.Conversation.Turn(agentCtx, prompt, model)
	done()
	if err != nil {
		h.Log.Error("could not start a turn", zap.Error(err))
		// The line that said the turn was starting must not keep saying it.
		if status != 0 && h.Chat != nil {
			if editErr := h.Chat.Edit(ctx, status, refused(who, err)); editErr != nil {
				h.Log.Debug("could not close the status line", zap.Error(editErr))
			}
		}

		return false
	}

	h.mu.Lock()
	h.turnID = turnID
	h.statusID = status
	h.turnFor = who
	h.turnStarted = h.Now()
	h.mu.Unlock()

	if h.Record != nil {
		if err := h.Record.TurnStarted(ctx, record.Turn{
			Ref:       turnID,
			ThreadRef: threadID,
			StartedAt: h.Now(),
			WokenBy:   who,
			Cause:     firstLine(woke),
			Model:     h.modelOf(model),
		}); err != nil {
			h.Log.Error("could not record the turn", zap.Error(err))
		}
	}

	h.mu.Lock()
	h.turnActed = false
	h.brokerCalled = false
	h.mu.Unlock()

	// A turn that finished before its name reached us finishes now. Without this
	// the completion was dropped, the turn stood as running, and the sessions
	// behind it waited for the interrupt that comes minutes later.
	h.mu.Lock()
	endedAlready := h.finishedBeforePublished == turnID
	if endedAlready {
		h.finishedBeforePublished = ""
	}
	h.mu.Unlock()
	if endedAlready {
		h.Log.Info("the turn had already finished when its name arrived", zap.String("turn_id", turnID))
		defer h.finishTurn(ctx, turnID)
	}

	go h.showTyping(ctx, turnID)

	h.Log.Info("turn started",
		zap.String("thread_id", threadID),
		zap.String("turn_id", turnID),
		zap.String("from", who))

	return true
}

// status opens the line the tool calls will rewrite. With no chat there is no
// line, and that is a legal state: the clock can wake sessions in silence.
func (h *Harness) status(ctx context.Context, text string) (int, error) {
	if h.Chat == nil {
		return 0, nil
	}

	return h.Chat.Send(ctx, text)
}

func (h *Harness) stop(ctx context.Context) {
	turnID := h.runningTurn()
	if turnID == "" {
		h.say(ctx, "nothing is running right now")
		return
	}
	agentCtx, done := h.boundToAgent(ctx)
	err := h.Conversation.Interrupt(agentCtx, turnID)
	done()
	if err != nil {
		h.Log.Error("could not interrupt the turn", zap.Error(err))
		h.say(ctx, "could not stop it: "+err.Error())
		return
	}
	h.Log.Info("turn interrupted by a person", zap.String("turn_id", turnID))
}

// followTheSession reads the agent's stream: whole messages are posted, tool
// calls replace one status line rather than adding a message, and the end of a
// turn closes both that line and the turn in the record.
func (h *Harness) followTheSession(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-h.Conversation.Events():
			if !ok {
				return
			}
			switch ev.Kind {
			case agent.KindText:
				h.markActed()
				h.writeDown(ctx, ev)
				h.sayInTurn(ctx, ev.Text)
			case agent.KindToolStarted:
				h.markActed()
				h.callStarted(ctx, ev)
				h.updateStatus(ctx, working(h.runningFor(), ev.Call.Named()))
			case agent.KindTool:
				h.callFinished(ctx, ev)
			case agent.KindTurnDone:
				h.finishTurn(ctx, ev.TurnID)
			}
		}
	}
}

// callStarted and callFinished write down what the session did with its hands.
// The pair is kept rather than one row at the end, so a call that was in flight
// when the process died is visible as unknown rather than as never made.
// noteBrokerReach marks that this turn got as far as the broker. The server's
// name is the agent's own for it, written in its config by the entrypoint.
func (h *Harness) noteBrokerReach(server string) {
	if server != brokerServer {
		return
	}

	h.mu.Lock()
	h.brokerCalled = true
	h.mu.Unlock()
}

// brokerServer is what the broker's MCP server is called in the agent's config.
const brokerServer = "broker"

func (h *Harness) callStarted(ctx context.Context, ev agent.Event) {
	h.noteBrokerReach(ev.Call.Server)

	if h.Record == nil || ev.Call.Ref == "" {
		return
	}

	// A SHELL call reaches the record as the FACT that one ran, and nothing else.
	//
	// The record is served, unauthenticated, on the page a judge opens. A shell
	// call carries the command line as its name, its arguments as typed and the
	// first of its output as its answer - and one `env` in a session that read a
	// poisoned headline would put this stack's database password, gateway token
	// and chat token on a public address. The command line is as dangerous as the
	// output: `echo $GATEWAY_TOKEN` says it in the name alone.
	//
	// The room still sees the command in full - it is private, and a person
	// watching needs to know what ran. The tools that matter to a reader - the
	// broker's and the session's own - are recorded whole, because showing them
	// is what the page is for.
	call := record.ToolCall{
		Ref:       ev.Call.Ref,
		TurnRef:   ev.TurnID,
		Server:    ev.Call.Server,
		Tool:      ev.Call.Tool,
		Arguments: ev.Call.Arguments,
		StartedAt: h.Now(),
		Status:    ev.Call.Status,
	}
	if ev.Call.Server == agent.ServerShell {
		call.Tool = shellCommandRan
		call.Arguments = nil
	}

	err := h.Record.CallStarted(ctx, call)
	if err != nil {
		h.Log.Error("could not record the call", zap.String("tool", ev.Call.Named()), zap.Error(err))
	}
}

func (h *Harness) callFinished(ctx context.Context, ev agent.Event) {
	if h.Record == nil || ev.Call.Ref == "" {
		return
	}

	answer, failure := ev.Call.Answer, ev.Call.Failure
	if ev.Call.Server == agent.ServerShell {
		// See callStarted. The failure goes too: a shell error quotes the command
		// it failed on, so the credential a name carried arrives by that road
		// instead.
		answer, failure = "", ""
	}

	err := h.Record.CallFinished(ctx, ev.Call.Ref, h.Now(), ev.Call.Status, failure, answer)
	if err != nil {
		h.Log.Error("could not close the call", zap.String("tool", ev.Call.Named()), zap.Error(err))
	}
}

// shellCommandRan is what the record calls a shell command. A fixed name, so
// that no part of what was typed reaches the page.
const shellCommandRan = "a shell command"

// clearTurn forgets the running turn and takes down its status line.
func (h *Harness) clearTurn(ctx context.Context) {
	h.mu.Lock()
	status := h.statusID
	h.turnID = ""
	h.statusID = 0
	h.mu.Unlock()

	if status != 0 && h.Chat != nil {
		if err := h.Chat.Delete(ctx, status); err != nil {
			h.Log.Debug("could not take down the status line", zap.Error(err))
		}
	}
}

func (h *Harness) finishTurn(ctx context.Context, turnID string) {
	h.mu.Lock()
	if turnID != "" && turnID != h.turnID {
		// It may be the turn being started right now, finishing before this
		// process learnt its name. Remembered, and finished the moment the name
		// arrives; anything else is a turn that ended long ago and is ignored as
		// before.
		if h.opening {
			h.finishedBeforePublished = turnID
		}
		h.mu.Unlock()

		return
	}
	h.turnID = ""
	status := h.statusID
	who := h.turnFor
	started := h.turnStarted
	acted := h.turnActed
	reached := h.brokerCalled
	h.statusID = 0
	h.mu.Unlock()

	// Whatever the pause swallowed is said now: the last thing a turn said is the
	// answer, and it must not be the thing that got held back.
	h.sayWhatWasHeldBack(ctx)

	// The line that tracked the work comes down, and the result goes to the
	// bottom: by the end the session may have written a page, and a mark left at
	// the top of it says nothing to anyone reading downward.
	if status != 0 && h.Chat != nil {
		if err := h.Chat.Delete(ctx, status); err != nil {
			h.Log.Debug("could not take down the status line", zap.Error(err))
		}
	}
	failure := ""
	if !acted {
		failure = record.SilentFailure
		h.Log.Error("the agent ended a turn without saying or calling anything",
			zap.String("turn_id", turnID), zap.String("session", who))
	}

	if acted {
		h.say(ctx, finished(who, h.Now().Sub(started)))
	} else {
		h.say(ctx, didNothing(who))
	}
	if h.Record != nil && turnID != "" {
		if err := h.Record.TurnFinished(ctx, turnID, h.Now(), failure); err != nil {
			h.Log.Error("could not close the turn in the record", zap.Error(err))
		}
	}
	h.Log.Info("turn finished", zap.String("turn_id", turnID), zap.Bool("acted", acted))

	h.checkBrokerReach(ctx, acted, reached, who)
}

// brokerlessBeforeFresh is how many turns in a row may finish without reaching
// the broker before this process stops trusting the conversation it is in.
//
// Two, not one: a turn can legitimately end early without asking the broker
// anything - a date guard that says "not today", a defence run that finds no
// positions in its own record. Two in a row is not that.
var brokerlessBeforeFresh = 2

// checkBrokerReach ends a conversation whose tools have gone quiet.
//
// The agent reads the list of tools ONCE per connection. A broker that blinks -
// a gateway restarted, a name that stopped resolving for a minute - is dropped
// from that list and does not come back, and the session then wakes on schedule,
// reasons carefully and does nothing, turn after turn. Measured on 27 August:
// in one thread get_clock answered in 0.56 seconds at 12:11, and by 13:38 the
// session reported that only its own tools were left. No error was logged by
// anyone, because there were no calls to fail.
//
// Restarting this process does NOT cure it: the thread is remembered on purpose,
// so a restart resumes the same conversation and inherits the same gap. Only a
// fresh thread reconnects the servers. So that is what happens here.
// ReportsToolCalls is implemented by a conversation that streams the agent's
// tool calls back as events. A driver that does not implement it is assumed to,
// which keeps the behaviour of the one that always did.
//
// It exists because the check below judges a turn by whether it reached the
// broker, and that can only be seen where tool calls travel through us. When the
// agent is a session we do not run - it takes its turn from the mailbox and calls
// its own servers - the calls never pass this way, every turn looks brokerless,
// and the watchdog fires on a healthy agent: measured 28 August, three scan turns
// in a row, each ending "a turn finished without reaching the broker" while the
// session was in fact reading chains and quotes the whole time.
type ReportsToolCalls interface {
	ReportsToolCalls() bool
}

// watchesBrokerReach says whether the broker watchdog can see anything at all.
func (h *Harness) watchesBrokerReach() bool {
	if reporter, ok := h.Conversation.(ReportsToolCalls); ok {
		return reporter.ReportsToolCalls()
	}

	return true
}

func (h *Harness) checkBrokerReach(ctx context.Context, acted, reached bool, who string) {
	// Silence about what cannot be observed. The alternative is worse than no
	// check: a warning on every turn, and every few turns a conversation thrown
	// away for a fault that never happened.
	if !h.watchesBrokerReach() {
		return
	}

	// A turn that did nothing at all is already reported as a silent failure, and
	// counting it here would blame the tools for the agent's own silence.
	if !acted {
		return
	}

	h.mu.Lock()
	if reached {
		h.brokerless = 0
		h.mu.Unlock()

		return
	}
	h.brokerless++
	count := h.brokerless
	h.mu.Unlock()

	if count < brokerlessBeforeFresh {
		h.Log.Warn("a turn finished without reaching the broker",
			zap.String("session", who), zap.Int("in_a_row", count))

		return
	}

	h.Log.Error("no turn has reached the broker for several turns: starting a fresh conversation",
		zap.Int("in_a_row", count),
		zap.String("why", "the agent reads its tools once per connection, so a server that blinked is gone for this thread"))

	h.Conversation.Forget()

	h.mu.Lock()
	h.threadID = ""
	h.brokerless = 0
	h.mu.Unlock()

	h.say(ctx, "The broker tools have vanished from the list. Starting the conversation again - that is the only thing that brings them back.")
}

// writeDown keeps what the agent said, so the reasoning behind a decision
// survives somewhere a reader can reach.
//
// The calls and their answers already say WHAT happened. They do not say why,
// and "why" is what the page is for: a judge opening the address should see the
// decision in words - what the entry was justified by, why a session refused,
// what it found in the chain - and not only the curve it produced.
//
// The agent's own transcript keeps this too, but in its format, in files, with
// no link to our turns. This is the same words beside the turn they belong to.
func (h *Harness) writeDown(ctx context.Context, ev agent.Event) {
	if h.Record == nil || ev.TurnID == "" || strings.TrimSpace(ev.Text) == "" {
		return
	}

	if err := h.Record.AppendSaid(ctx, record.Said{
		TurnRef: ev.TurnID, At: h.Now(), Text: ev.Text,
	}); err != nil {
		// Losing a line of reasoning must not cost the turn it belongs to: the
		// trade is the point, the record of it is second.
		h.Log.Error("could not write down what the agent said", zap.Error(err))
	}
}

// markActed notes that the agent produced something this turn.
func (h *Harness) markActed() {
	h.mu.Lock()
	h.turnActed = true
	h.mu.Unlock()
}

func (h *Harness) openThread(ctx context.Context) (string, error) {
	h.mu.Lock()
	existing := h.threadID
	h.mu.Unlock()
	if existing != "" {
		return existing, nil
	}

	threadID, err := h.Conversation.Open(ctx)
	if err != nil {
		return "", err
	}

	h.mu.Lock()
	h.threadID = threadID
	h.mu.Unlock()

	return threadID, nil
}

// boundToAgent bounds ONE request to the agent. Only the agent's own calls carry
// this bound: posting to the chat is a different service with a different reason
// to be slow, and cutting it here would silence the room over the agent's fault.
func (h *Harness) boundToAgent(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, h.CallTimeout)
}

// runningFor is the name the current turn runs under: a session's name, or the
// person who wrote.
func (h *Harness) runningFor() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.turnFor
}

// showTyping holds the chat's own "typing" cue while this turn runs. Telegram
// clears it after a few seconds, so it is repeated; it stops as soon as the turn
// this was started for is no longer the one running.
func (h *Harness) showTyping(ctx context.Context, turnID string) {
	if h.Chat == nil {
		return
	}

	ticker := time.NewTicker(typingRefresh)
	defer ticker.Stop()

	for h.runningTurn() == turnID {
		if err := h.Chat.Typing(ctx); err != nil {
			h.Log.Debug("could not show the typing cue", zap.Error(err))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// working and finished are the two lines the room sees for one turn. The status
// line is edited in place rather than re-posted: a turn is one thing happening,
// and it should read as one line changing, not as a column of fragments.
func working(who, tool string) string {
	head := "⏳ " + who
	if tool != "" {
		return head + " · " + tool
	}

	return head + " · thinking"
}

func finished(who string, took time.Duration) string {
	return fmt.Sprintf("✅ %s · done in %s", who, howLong(took))
}

// didNothing is said instead of a tick when the agent produced nothing at all.
// The room must not read "done" for a session that never ran.
func didNothing(who string) string {
	return fmt.Sprintf("⚠️ %s · the turn did not happen: the agent said nothing and called nothing", who)
}

// howLong writes a duration the way the room reads it, rounded: nobody in the
// chat needs the milliseconds.
func howLong(took time.Duration) string {
	switch seconds := int(took.Round(time.Second).Seconds()); {
	case seconds < 1:
		return "a moment"
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	default:
		minutes := seconds / 60
		rest := seconds % 60
		if rest == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm %ds", minutes, rest)
	}
}

func refused(who string, err error) string {
	return fmt.Sprintf("⚠️ %s · did not start: %s", who, err)
}

func (h *Harness) runningTurn() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.turnID
}

// RunningTurn says which turn is in flight and who woke it. The session's own
// tools ask, so an intent is filed under the turn that produced it rather than
// under a name the model typed.
func (h *Harness) RunningTurn() (ref string, wokenBy string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.turnID, h.turnFor
}

// interruptAttempts is how many refusals in a row are tolerated before the
// process ends itself. Three, at one attempt per watch tick, is long enough for a
// turn that is merely slow to finish and short enough that a wedged one does not
// cost a trading session. A variable so a test can prove the escalation.
var interruptAttempts = 3

// sayInTurn posts what the session said, unless it said something moments ago -
// then it is held until either the pause has passed or the turn ends.
func (h *Harness) sayInTurn(ctx context.Context, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if h.SayEvery <= 0 {
		h.say(ctx, text)
		return
	}

	h.mu.Lock()
	quiet := h.Now().Sub(h.lastSaid) >= h.SayEvery
	if quiet {
		h.lastSaid = h.Now()
		h.heldBack = ""
	} else {
		h.heldBack = text
	}
	h.mu.Unlock()

	if quiet {
		h.say(ctx, text)
	}
}

// sayWhatWasHeldBack posts the last thing a turn said if the pause swallowed it.
// The final word of a turn is the one the room actually needs.
func (h *Harness) sayWhatWasHeldBack(ctx context.Context) {
	h.mu.Lock()
	text := h.heldBack
	h.heldBack = ""
	if text != "" {
		h.lastSaid = h.Now()
	}
	h.mu.Unlock()

	h.say(ctx, text)
}

// Post puts one line in the room from something that is not a session - a fill
// the ladder saw, and nothing else so far. It carries no throttle: what is
// throttled is a session thinking out loud, and a fill is the opposite of that.
func (h *Harness) Post(ctx context.Context, text string) { h.say(ctx, text) }

func (h *Harness) say(ctx context.Context, text string) {
	if h.Chat == nil || strings.TrimSpace(text) == "" {
		return
	}
	if _, err := h.Chat.Send(ctx, text); err != nil {
		h.Log.Error("could not post what the session said", zap.Error(err))
	}
}

func (h *Harness) updateStatus(ctx context.Context, text string) {
	h.mu.Lock()
	status := h.statusID
	h.mu.Unlock()
	if status == 0 || h.Chat == nil {
		return
	}
	if err := h.Chat.Edit(ctx, status, text); err != nil {
		h.Log.Debug("could not update the status line", zap.Error(err))
	}
}

// promptFor names the cause. A session that cannot say why it ran cannot be
// judged on whether it should have.
func promptFor(msg telegram.Message) string {
	return "Woken by a person in the chat.\n" + textOf(msg)
}

// modelOf names the model this turn actually ran on. An empty one means the
// session did not override the conversation's, not that none was used.
func (h *Harness) modelOf(model string) string {
	if model == "" {
		return h.DefaultModel
	}

	return model
}

// firstLine is the cause: every prompt opens with why the session was woken.
func firstLine(prompt string) string {
	if cut := strings.IndexByte(prompt, '\n'); cut > 0 {
		return prompt[:cut]
	}

	return prompt
}

func textOf(msg telegram.Message) string {
	who := msg.Username
	if who == "" {
		who = fmt.Sprintf("user %d", msg.UserID)
	}

	return fmt.Sprintf("From %s: %s", who, msg.Text)
}
