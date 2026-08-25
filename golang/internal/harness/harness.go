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

// clockTick is how often the schedule is asked whether anything is due. The
// declaration speaks in minutes, so asking more often would only repeat the same
// question.
const clockTick = time.Minute

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
	// nobody and the chat is the only cause.
	Declaration *declaration.Declaration
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
	// CallTimeout bounds one request to the agent. The loop that reads the chat is
	// the loop that talks to the agent, so an unbounded call takes the room down
	// with the session.
	CallTimeout time.Duration
	Log         *zap.Logger

	mu          sync.Mutex
	threadID    string
	turnID      string
	statusID    int
	turnFor     string
	turnStarted time.Time
	lastRun     map[string]time.Time
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
	h.lastRun = h.whatAlreadyRanToday(ctx)

	// Followed whether or not anybody is watching the chat: this is the loop that
	// closes a turn in the record, and the record is read long after the room.
	go h.followTheSession(ctx)
	if h.Declaration != nil || h.Wakeups != nil {
		go h.keepTheClock(ctx)
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
func (h *Harness) whatAlreadyRanToday(ctx context.Context) map[string]time.Time {
	if h.Record == nil || h.Declaration == nil {
		return map[string]time.Time{}
	}

	now := h.Now().In(h.Declaration.Location())
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
	for i := range h.Declaration.Sessions {
		name := h.Declaration.Sessions[i].Name
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
	ticker := time.NewTicker(clockTick)
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
	h.fireWakeups(ctx)
	h.fireDue(ctx)
}

// fireWakeups wakes the session for what it asked itself. Its own requests come
// first: a session that asked to be woken when price crossed a level asked
// because something needs doing before the next scheduled hour.
func (h *Harness) fireWakeups(ctx context.Context) {
	if h.Wakeups == nil {
		return
	}

	var prices map[string]float64
	if watching := h.Wakeups.Watching(); len(watching) > 0 && h.Prices != nil {
		read, done := h.boundToAgent(ctx)
		last, err := h.Prices.LastTrades(read, watching)
		done()
		if err != nil {
			h.Log.Error("could not read the prices a wake-up is watching",
				zap.Strings("symbols", watching), zap.Error(err))
		}
		prices = last
	}

	for _, due := range h.Wakeups.Due(h.Now(), prices) {
		// A wake-up that comes due mid-turn goes INTO that turn. Starting a second
		// one would leave the first orphaned - never closed in the record, never
		// closed in the chat - and would put two turns on one conversation.
		if turnID := h.runningTurn(); turnID != "" {
			agentCtx, done := h.boundToAgent(ctx)
			err := h.Conversation.Steer(agentCtx, turnID, due.Prompt())
			done()
			if err == nil {
				h.Log.Info("a wake-up came due mid-turn and went into it",
					zap.String("wakeup", due.ID), zap.String("turn_id", turnID))
				continue
			}
			// The turn ended between the check and the call: nothing is lost, the
			// wake-up becomes its own turn below.
			h.Log.Info("could not reach the running turn with a wake-up, starting a new one",
				zap.String("wakeup", due.ID), zap.Error(err))
		}
		h.Log.Info("waking the session for its own reason",
			zap.String("wakeup", due.ID), zap.String("cause", string(due.Cause)))
		h.startTurnWith(ctx, due.Prompt(), "wakeup "+due.ID, "")
	}
}

func (h *Harness) fireDue(ctx context.Context) {
	if h.Declaration == nil {
		return
	}
	now := h.Now().In(h.Declaration.Location())

	for i := range h.Declaration.Sessions {
		session := &h.Declaration.Sessions[i]
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

		h.lastRun[session.Name] = now
		h.Log.Info("waking a session", zap.String("session", session.Name), zap.String("cause", session.Cause))
		h.startTurnWith(ctx, session.Prompt(), session.Name, session.Model)
	}
}

func (h *Harness) serveChat(ctx context.Context) error {
	listening := make(chan error, 1)
	go func() { listening <- h.Chat.Listen(ctx) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-listening:
			return err
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

	if turnID := h.runningTurn(); turnID != "" {
		agentCtx, done := h.boundToAgent(ctx)
		err := h.Conversation.Steer(agentCtx, turnID, textOf(msg))
		done()
		if err != nil {
			// The turn ended between the check and the call. Nothing is lost: the
			// message becomes the next turn instead of vanishing into a finished one.
			h.Log.Info("could not reach the running turn, starting a new one", zap.Error(err))
		} else {
			h.Log.Info("message went into the running turn", zap.String("turn_id", turnID))
			return
		}
	}

	h.startTurn(ctx, msg)
}

func (h *Harness) startTurn(ctx context.Context, msg telegram.Message) {
	h.startTurnWith(ctx, promptFor(msg), msg.Username, "")
}

// startTurnWith runs one turn. model names the model this particular turn is
// worth: a session that only reads the news does not need the one that trades.
func (h *Harness) startTurnWith(ctx context.Context, prompt, who, model string) {
	agentCtx, done := h.boundToAgent(ctx)
	threadID, err := h.openThread(agentCtx)
	done()
	if err != nil {
		h.Log.Error("could not open a conversation with the agent", zap.Error(err))
		h.say(ctx, "не удалось начать разговор с агентом: "+err.Error())
		return
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
		return
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
			Cause:     firstLine(prompt),
			Model:     h.modelOf(model),
		}); err != nil {
			h.Log.Error("could not record the turn", zap.Error(err))
		}
	}

	go h.showTyping(ctx, turnID)

	h.Log.Info("turn started",
		zap.String("thread_id", threadID),
		zap.String("turn_id", turnID),
		zap.String("from", who))
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
		h.say(ctx, "сейчас ничего не выполняется")
		return
	}
	agentCtx, done := h.boundToAgent(ctx)
	err := h.Conversation.Interrupt(agentCtx, turnID)
	done()
	if err != nil {
		h.Log.Error("could not interrupt the turn", zap.Error(err))
		h.say(ctx, "остановить не удалось: "+err.Error())
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
				h.say(ctx, ev.Text)
			case agent.KindToolStarted:
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
func (h *Harness) callStarted(ctx context.Context, ev agent.Event) {
	if h.Record == nil || ev.Call.Ref == "" {
		return
	}

	err := h.Record.CallStarted(ctx, record.ToolCall{
		Ref:       ev.Call.Ref,
		TurnRef:   ev.TurnID,
		Server:    ev.Call.Server,
		Tool:      ev.Call.Tool,
		Arguments: ev.Call.Arguments,
		StartedAt: h.Now(),
		Status:    ev.Call.Status,
	})
	if err != nil {
		h.Log.Error("could not record the call", zap.String("tool", ev.Call.Named()), zap.Error(err))
	}
}

func (h *Harness) callFinished(ctx context.Context, ev agent.Event) {
	if h.Record == nil || ev.Call.Ref == "" {
		return
	}

	err := h.Record.CallFinished(ctx, ev.Call.Ref, h.Now(), ev.Call.Status, ev.Call.Failure)
	if err != nil {
		h.Log.Error("could not close the call", zap.String("tool", ev.Call.Named()), zap.Error(err))
	}
}

func (h *Harness) finishTurn(ctx context.Context, turnID string) {
	h.mu.Lock()
	if turnID != "" && turnID != h.turnID {
		h.mu.Unlock()
		return
	}
	h.turnID = ""
	status := h.statusID
	who := h.turnFor
	started := h.turnStarted
	h.statusID = 0
	h.mu.Unlock()

	// The line that tracked the work comes down, and the result goes to the
	// bottom: by the end the session may have written a page, and a mark left at
	// the top of it says nothing to anyone reading downward.
	if status != 0 && h.Chat != nil {
		if err := h.Chat.Delete(ctx, status); err != nil {
			h.Log.Debug("could not take down the status line", zap.Error(err))
		}
	}
	h.say(ctx, finished(who, h.Now().Sub(started)))
	if h.Record != nil && turnID != "" {
		if err := h.Record.TurnFinished(ctx, turnID, h.Now(), ""); err != nil {
			h.Log.Error("could not close the turn in the record", zap.Error(err))
		}
	}
	h.Log.Info("turn finished", zap.String("turn_id", turnID))
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

	return head + " · думает"
}

func finished(who string, took time.Duration) string {
	return fmt.Sprintf("✅ %s · готово за %s", who, howLong(took))
}

// howLong writes a duration the way the room reads it, in Russian and rounded:
// nobody in the chat needs the milliseconds, and "7s" reads as a machine.
func howLong(took time.Duration) string {
	switch seconds := int(took.Round(time.Second).Seconds()); {
	case seconds < 1:
		return "мгновение"
	case seconds < 60:
		return fmt.Sprintf("%d с", seconds)
	default:
		minutes := seconds / 60
		rest := seconds % 60
		if rest == 0 {
			return fmt.Sprintf("%d мин", minutes)
		}
		return fmt.Sprintf("%d мин %d с", minutes, rest)
	}
}

func refused(who string, err error) string {
	return fmt.Sprintf("⚠️ %s · не начался: %s", who, err)
}

func (h *Harness) runningTurn() string {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.turnID
}

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
