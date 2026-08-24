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

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/appserver"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// clockTick is how often the schedule is asked whether anything is due. The
// declaration speaks in minutes, so asking more often would only repeat the same
// question.
const clockTick = time.Minute

// Chat is the room the session works in. The agent never holds this - the
// harness posts what the session said and passes on what a person wrote.
type Chat interface {
	Listen(ctx context.Context) error
	Inbound() <-chan telegram.Message
	Send(ctx context.Context, text string) (int, error)
	Edit(ctx context.Context, messageID int, text string) error
}

// Conversation is one thread with the agent, held open across turns.
type Conversation interface {
	Open(ctx context.Context) (threadID string, err error)
	Turn(ctx context.Context, text string) (turnID string, err error)
	Steer(ctx context.Context, turnID, text string) error
	Interrupt(ctx context.Context, turnID string) error
	Events() <-chan appserver.Event
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
	// Now is the clock. It is a field so a test is not at the mercy of the wall
	// clock, and so the only place a time comes from is visible.
	Now func() time.Time
	// CallTimeout bounds one request to the agent. The loop that reads the chat is
	// the loop that talks to the agent, so an unbounded call takes the room down
	// with the session.
	CallTimeout time.Duration
	Log         *zap.Logger

	mu       sync.Mutex
	threadID string
	turnID   string
	statusID int
	lastRun  map[string]time.Time
}

// Run holds both causes until ctx ends. With neither a declaration nor a chat it
// refuses to start: a harness that runs while waking nobody looks exactly like a
// working one.
func (h *Harness) Run(ctx context.Context) error {
	if h.Declaration == nil && h.Chat == nil {
		return fmt.Errorf("the harness has no cause to wake a session: set DECLARATION, configure the chat, or run neither role")
	}
	if h.CallTimeout <= 0 {
		return fmt.Errorf("the harness needs a bound on one call to the agent: set AGENT_CALL_TIMEOUT")
	}
	if h.Now == nil {
		return fmt.Errorf("the harness has no clock")
	}
	h.lastRun = map[string]time.Time{}

	if h.Chat != nil {
		go h.postWhatTheSessionSays(ctx)
	}
	if h.Declaration != nil {
		go h.keepTheClock(ctx)
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

// keepTheClock wakes the sessions the declaration names. It checks once a minute
// because the declaration speaks in minutes; a finer tick would only ask the same
// question more often.
func (h *Harness) keepTheClock(ctx context.Context) {
	ticker := time.NewTicker(clockTick)
	defer ticker.Stop()

	h.fireDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.fireDue(ctx)
		}
	}
}

func (h *Harness) fireDue(ctx context.Context) {
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
		h.startTurnWith(ctx, session.Prompt(), session.Name)
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
	h.startTurnWith(ctx, promptFor(msg), msg.Username)
}

func (h *Harness) startTurnWith(ctx context.Context, prompt, who string) {
	agentCtx, done := h.boundToAgent(ctx)
	threadID, err := h.openThread(agentCtx)
	done()
	if err != nil {
		h.Log.Error("could not open a conversation with the agent", zap.Error(err))
		h.say(ctx, "не удалось начать разговор с агентом: "+err.Error())
		return
	}

	status, err := h.status(ctx, "working")
	if err != nil {
		h.Log.Error("could not open a status line in the chat", zap.Error(err))
	}

	agentCtx, done = h.boundToAgent(ctx)
	turnID, err := h.Conversation.Turn(agentCtx, prompt)
	done()
	if err != nil {
		h.Log.Error("could not start a turn", zap.Error(err))
		h.say(ctx, "агент не взял задачу: "+err.Error())
		return
	}

	h.mu.Lock()
	h.turnID = turnID
	h.statusID = status
	h.mu.Unlock()

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

// postWhatTheSessionSays is the one place the room learns what happened: whole
// messages are posted, tool calls replace one status line rather than adding a
// message, and the end of a turn closes that line.
func (h *Harness) postWhatTheSessionSays(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-h.Conversation.Events():
			if !ok {
				return
			}
			switch ev.Kind {
			case appserver.KindText:
				h.say(ctx, ev.Text)
			case appserver.KindTool:
				h.updateStatus(ctx, "working: "+ev.Tool)
			case appserver.KindTurnDone:
				h.finishTurn(ctx, ev.TurnID)
			}
		}
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
	h.statusID = 0
	h.mu.Unlock()

	if status != 0 && h.Chat != nil {
		if err := h.Chat.Edit(ctx, status, "done"); err != nil {
			h.Log.Debug("could not close the status line", zap.Error(err))
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

func textOf(msg telegram.Message) string {
	who := msg.Username
	if who == "" {
		who = fmt.Sprintf("user %d", msg.UserID)
	}

	return fmt.Sprintf("From %s: %s", who, msg.Text)
}
