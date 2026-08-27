package harness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

// chatDouble accepts what production accepts and refuses what production
// refuses: an empty message reaches Telegram from neither.
type chatDouble struct {
	mu       sync.Mutex
	inbound  chan telegram.Message
	posted   []string
	statuses []string
	deleted  []int
	typed    int
	nextID   int
	// listenFails, when set, is what Listen returns at once instead of waiting
	// for the context - a room that goes away while the process runs.
	listenFails error
}

func newChatDouble() *chatDouble {
	return &chatDouble{inbound: make(chan telegram.Message, 8)}
}

func (c *chatDouble) Listen(ctx context.Context) error {
	if c.listenFails != nil {
		return c.listenFails
	}
	<-ctx.Done()

	return nil
}

func (c *chatDouble) Inbound() <-chan telegram.Message { return c.inbound }

func (c *chatDouble) Send(_ context.Context, text string) (int, error) {
	if text == "" {
		return 0, fmt.Errorf("refusing to send an empty message")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.posted = append(c.posted, text)
	return c.nextID, nil
}

func (c *chatDouble) Typing(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.typed++

	return nil
}

func (c *chatDouble) Delete(_ context.Context, messageID int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleted = append(c.deleted, messageID)

	return nil
}

func (c *chatDouble) Edit(_ context.Context, _ int, text string) error {
	if text == "" {
		return fmt.Errorf("refusing to edit a message to nothing")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statuses = append(c.statuses, text)
	return nil
}

func (c *chatDouble) postedTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.posted...)
}

func (c *chatDouble) deletedIDs() []int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]int(nil), c.deleted...)
}

func (c *chatDouble) statusTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.statuses...)
}

type conversationSpy struct {
	// hang, when set, makes every turn block until it is closed - the shape of an
	// agent that stopped answering.
	hang       chan struct{}
	mu         sync.Mutex
	events     chan agent.Event
	turns      []string
	models     []string
	steered    []string
	interrupts []string
	turnErr    error
	steerErr   error
	// refuseInterrupt is what the agent answers to turn/interrupt. Nil accepts it.
	refuseInterrupt error
	nextTurn        int
}

func newConversationSpy() *conversationSpy {
	return &conversationSpy{events: make(chan agent.Event, 16)}
}

func (c *conversationSpy) Open(context.Context) (string, error) { return "th-1", nil }

func (c *conversationSpy) Turn(ctx context.Context, text, model string) (string, error) {
	if c.hang != nil {
		c.mu.Lock()
		c.turns = append(c.turns, text)
		c.models = append(c.models, model)
		c.mu.Unlock()
		select {
		case <-c.hang:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "", fmt.Errorf("the agent never answered")
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.turnErr != nil {
		return "", c.turnErr
	}
	c.turns = append(c.turns, text)
	c.models = append(c.models, model)
	c.nextTurn++
	return fmt.Sprintf("tu-%d", c.nextTurn), nil
}

func (c *conversationSpy) Steer(_ context.Context, turnID, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.steerErr != nil {
		return c.steerErr
	}
	c.steered = append(c.steered, turnID+": "+text)
	return nil
}

func (c *conversationSpy) interrupted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.interrupts...)
}

func (c *conversationSpy) Interrupt(_ context.Context, turnID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interrupts = append(c.interrupts, turnID)
	return c.refuseInterrupt
}

func (c *conversationSpy) Events() <-chan agent.Event { return c.events }

func (c *conversationSpy) seen() ([]string, []string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.turns...), append([]string(nil), c.steered...), append([]string(nil), c.interrupts...)
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

func start(t *testing.T) (*Harness, *chatDouble, *conversationSpy) {
	t.Helper()

	chat := newChatDouble()
	conversation := newConversationSpy()
	h := &Harness{Chat: chat, Conversation: conversation, CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t)}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = h.Run(ctx) }()

	return h, chat, conversation
}

// The loop that reads the chat is the loop that talks to the agent, so a call
// with no bound takes the room down with a session that stopped answering.
func TestRefusesWithoutACallBound(t *testing.T) {
	h := &Harness{Chat: newChatDouble(), Conversation: newConversationSpy(), Now: time.Now, Log: zaptest.NewLogger(t)}
	err := h.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENT_CALL_TIMEOUT")
}

func TestAHungAgentDoesNotWedgeTheRoom(t *testing.T) {
	chat := newChatDouble()
	conversation := newConversationSpy()
	conversation.hang = make(chan struct{})
	t.Cleanup(func() { close(conversation.hang) })

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{Chat: chat, Conversation: conversation, CallTimeout: 300 * time.Millisecond, Now: time.Now, Log: zaptest.NewLogger(t)}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "start something", UserID: 42, Username: "joker"}
	chat.inbound <- telegram.Message{Text: "and this one after it", UserID: 42, Username: "joker"}

	// The second message is only ever handled if the first call gave up in time.
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) >= 2 })
}

func TestRefusesWithoutAnyCause(t *testing.T) {
	h := &Harness{Log: zaptest.NewLogger(t)}
	err := h.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cause to wake a session")
}

// The clock wakes a session on its own, with no chat and nobody typing: that is
// what the hackathon means by autonomous.
func TestTheClockWakesASessionWithoutAnybody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
version: v1
timezone: UTC
sessions:
  - name: flatten
    cause: "закрыть всё перед концом дня"
    task: "Закрой все позиции."
    at: "15:50"
    within: 20m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation,
		Declaration:  declared,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return time.Date(2026, 8, 24, 15, 50, 0, 0, time.UTC) },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "закрыть всё перед концом дня", "the session must carry why it was woken")
	assert.Contains(t, turns[0], "Закрой все позиции")
}

// Two sessions on one account close each other's positions, so a due session
// waits rather than starting beside a running one.
func TestADueSessionWaitsForTheRunningTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: defend
    cause: "проверка правил защиты"
    task: "Проверь позиции."
    every: 30m
    between: ["09:40", "15:55"]
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	chat := newChatDouble()
	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat:         chat,
		Conversation: conversation,
		Declaration:  declared,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC) },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	// A person writes while the scheduled session runs: that message steers the
	// running turn instead of starting a second one.
	chat.inbound <- telegram.Message{Text: "close it", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { _, steered, _ := conversation.seen(); return len(steered) == 1 })

	turns, _, _ := conversation.seen()
	assert.Len(t, turns, 1)
}

func TestFirstMessageStartsATurnAndTheRoomSeesIt(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "close everything", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "close everything")
	assert.Contains(t, turns[0], "joker", "the session must be able to say who woke it")
	require.Len(t, chat.postedTexts(), 1)
	assert.Contains(t, chat.postedTexts()[0], "joker", "the room sees one line naming who the turn runs for")

	conversation.events <- agent.Event{
		Kind: agent.KindToolStarted, TurnID: "tu-1",
		Call: agent.Call{Ref: "call-1", Server: "broker", Tool: "get_option_chain", Status: "inProgress"},
	}
	conversation.events <- agent.Event{Kind: agent.KindText, Text: "the spread is closed", TurnID: "tu-1"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool { return len(chat.statusTexts()) >= 1 && len(chat.deletedIDs()) >= 1 })
	assert.Contains(t, chat.statusTexts(), "⏳ joker · broker.get_option_chain",
		"the same line changes as the turn works")

	posted := chat.postedTexts()
	assert.Contains(t, posted[len(posted)-1], "✅ joker · готово", "the result belongs at the bottom, where the reader is")
	assert.NotEmpty(t, chat.deletedIDs(), "the line that tracked the work comes down")
	assert.Contains(t, chat.postedTexts(), "the spread is closed")
}

// This is the whole point of holding the conversation open: a person who writes
// while the session works reaches the work in progress, not the next one.
func TestAMessageDuringATurnSteersIt(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "open the spread", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	chat.inbound <- telegram.Message{Text: "wait, half the size", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { _, steered, _ := conversation.seen(); return len(steered) == 1 })

	turns, steered, _ := conversation.seen()
	assert.Len(t, turns, 1, "the running turn takes the message; a second turn would be a different piece of work")
	assert.Contains(t, steered[0], "tu-1: ")
	assert.Contains(t, steered[0], "half the size")
}

func TestAMessageAfterTheTurnStartsANewOne(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "what is open?", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}
	waitFor(t, func() bool { return len(chat.deletedIDs()) >= 1 })

	chat.inbound <- telegram.Message{Text: "and the second leg?", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 2 })

	_, steered, _ := conversation.seen()
	assert.Empty(t, steered, "there is no running turn to steer")
}

func TestStopInterruptsTheRunningTurn(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "start something long", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	chat.inbound <- telegram.Message{Text: CommandStop, UserID: 42, Username: "joker"}
	waitFor(t, func() bool { _, _, interrupts := conversation.seen(); return len(interrupts) == 1 })

	turns, steered, interrupts := conversation.seen()
	assert.Equal(t, []string{"tu-1"}, interrupts)
	assert.Len(t, turns, 1, "stop is a command to the harness, not a task for the agent")
	assert.Empty(t, steered)
}

func TestStopWithNothingRunningSaysSo(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: CommandStop, UserID: 42, Username: "joker"}
	waitFor(t, func() bool { return len(chat.postedTexts()) >= 1 })

	_, _, interrupts := conversation.seen()
	assert.Empty(t, interrupts)
	assert.Contains(t, chat.postedTexts()[0], "ничего не выполняется")
}

// A turn that ends between the check and the steer must not swallow the message:
// it becomes the next turn instead.
func TestASteerThatArrivesTooLateBecomesANewTurn(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "open the spread", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	conversation.mu.Lock()
	conversation.steerErr = fmt.Errorf("turn already finished")
	conversation.mu.Unlock()

	chat.inbound <- telegram.Message{Text: "then tell me the loss", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 2 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[1], "tell me the loss")
}

type wakeupsDouble struct {
	mu       sync.Mutex
	standing []wakeup.Wakeup
	watching []string
}

func (w *wakeupsDouble) Due(now time.Time, price map[string]float64) []wakeup.Wakeup {
	w.mu.Lock()
	defer w.mu.Unlock()

	var due []wakeup.Wakeup
	kept := w.standing[:0]
	for _, one := range w.standing {
		if one.Kind == wakeup.KindAt && !now.Before(one.At) {
			due = append(due, one)
			continue
		}
		if one.Kind == wakeup.KindPrice {
			last, ok := price[one.Symbol]
			if ok && last <= one.Level {
				due = append(due, one)
				continue
			}
		}
		kept = append(kept, one)
	}
	w.standing = kept

	return due
}

func (w *wakeupsDouble) Watching() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]string(nil), w.watching...)
}

type pricesDouble struct {
	mu     sync.Mutex
	last   map[string]float64
	asked  [][]string
	broken error
}

func (p *pricesDouble) LastTrades(_ context.Context, symbols []string) (map[string]float64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.asked = append(p.asked, symbols)
	if p.broken != nil {
		return nil, p.broken
	}

	return p.last, nil
}

func (p *pricesDouble) timesAsked() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.asked)
}

// A session that asked to be woken must be woken, and told why it asked.
func TestTheSessionIsWokenForItsOwnReason(t *testing.T) {
	now := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	standing := &wakeupsDouble{standing: []wakeup.Wakeup{{
		ID: "w1", Kind: wakeup.KindAt, At: now.Add(-time.Minute),
		Cause: "посмотреть, как открылась позиция",
	}}}

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation,
		Wakeups:      standing,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return now },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "посмотреть, как открылась позиция")
}

// A price wake-up needs a reading, and the harness asks only for the symbols
// something is actually watching.
func TestAPriceWakeUpFiresOnAReading(t *testing.T) {
	now := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	standing := &wakeupsDouble{
		standing: []wakeup.Wakeup{{
			ID: "w2", Kind: wakeup.KindPrice, Symbol: "SPY", Direction: wakeup.Below, Level: 760,
			Cause: "цена подошла к проданному страйку",
		}},
		watching: []string{"SPY"},
	}
	prices := &pricesDouble{last: map[string]float64{"SPY": 759.5}}

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation,
		Wakeups:      standing,
		Prices:       prices,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return now },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "проданному страйку")
	assert.Equal(t, [][]string{{"SPY"}}, prices.asked)
}

// With nothing watching a price, the broker is not asked at all: a reading
// nobody needs is a request nobody should pay for.
func TestNoPriceIsReadWhenNothingWatchesOne(t *testing.T) {
	now := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	standing := &wakeupsDouble{standing: []wakeup.Wakeup{{
		ID: "w3", Kind: wakeup.KindAt, At: now.Add(time.Hour), Cause: "позже",
	}}}
	prices := &pricesDouble{}

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation,
		Wakeups:      standing,
		Prices:       prices,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return now },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { return true })
	assert.Zero(t, prices.timesAsked())
	turns, _, _ := conversation.seen()
	assert.Empty(t, turns)
}

// A turn that cannot start must not leave the room reading a line that says it
// is working.
func TestTheStatusLineClosesWhenATurnCannotStart(t *testing.T) {
	chat := newChatDouble()
	conversation := newConversationSpy()
	conversation.turnErr = fmt.Errorf("agent is not answering")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{Chat: chat, Conversation: conversation, CallTimeout: time.Second, Now: time.Now, Log: zaptest.NewLogger(t)}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "go", UserID: 42, Username: "joker"}

	waitFor(t, func() bool { return len(chat.statusTexts()) >= 1 })
	assert.Contains(t, chat.statusTexts()[0], "не начался")
	assert.Contains(t, chat.statusTexts()[0], "agent is not answering")
}

func TestHowLongReadsLikeRussian(t *testing.T) {
	cases := map[time.Duration]string{
		400 * time.Millisecond:               "мгновение",
		7 * time.Second:                      "7 с",
		90 * time.Second:                     "1 мин 30 с",
		4 * time.Minute:                      "4 мин",
		2*time.Minute + 500*time.Millisecond: "2 мин 1 с",
	}

	for took, want := range cases {
		assert.Equal(t, want, howLong(took), took.String())
	}
}

// The record is read long after the room, so a turn is written down and closed
// whether or not a chat is configured. Before this, the loop that closes a turn
// ran only beside a chat, and a scheduled session left an open turn forever.
func TestATurnIsRecordedAndClosedWithoutAChat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
version: v1
timezone: UTC
sessions:
  - name: flatten
    cause: "закрыть всё перед концом дня"
    task: "Закрой все позиции."
    at: "15:50"
    within: 20m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	conversation := newConversationSpy()
	kept := record.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation,
		Declaration:  declared,
		Record:       kept,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return time.Date(2026, 8, 24, 15, 50, 0, 0, time.UTC) },
		Log:          zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1
	})

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "flatten", state.Turns[0].WokenBy)
	assert.Contains(t, state.Turns[0].Cause, "закрыть всё перед концом дня")

	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1 && state.Turns[0].FinishedAt != nil
	})
}

// The record must answer which model traded. A session that names none runs on
// the conversation's, and an empty field would read as "no model".
func TestATurnRecordsTheModelItRanOn(t *testing.T) {
	conversation := newConversationSpy()
	kept := record.NewMemory()
	chat := newChatDouble()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		DefaultModel: "gpt-5.6-terra", CallTimeout: 2 * time.Second,
		Now: time.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "что на счёте?", Username: "joker"}

	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1
	})

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, "gpt-5.6-terra", state.Turns[0].Model)
}

// A wake-up that comes due while a turn runs goes into that turn. Starting a
// second one leaves the first orphaned: never closed in the record, never closed
// in the chat, and two turns pushing tools into one conversation.
func TestAWakeUpDuringATurnGoesIntoIt(t *testing.T) {
	store, err := wakeup.Open(filepath.Join(t.TempDir(), "wakeups.json"))
	require.NoError(t, err)

	at := time.Date(2026, 9, 3, 18, 0, 0, 0, time.UTC)
	_, err = store.AddAt("проверить спред после новости", at, at.Add(-time.Minute))
	require.NoError(t, err)

	conversation := newConversationSpy()
	chat := newChatDouble()
	kept := record.NewMemory()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept, Wakeups: store,
		CallTimeout: 2 * time.Second, Now: func() time.Time { return at },
		Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри позиции", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	waitFor(t, func() bool { _, steered, _ := conversation.seen(); return len(steered) == 1 })

	_, steered, _ := conversation.seen()
	assert.Contains(t, steered[0], "проверить спред после новости")

	turns, _, _ := conversation.seen()
	assert.Len(t, turns, 1, "the wake-up must not open a second turn")

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, state.Turns, 1)
}

// A restart inside a session's window must not run that session again: on this
// declaration that would mean a second spread on the same underlying.
func TestARestartInsideTheWindowDoesNotRunTheSessionTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	at := time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC)
	kept := record.NewMemory()
	require.NoError(t, kept.TurnStarted(context.Background(), record.Turn{
		Ref: "turn-1", ThreadRef: "th-1", StartedAt: at.Add(-18 * time.Minute),
		WokenBy: "entry", Cause: "declaration: entry",
	}))

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation, Declaration: declared, Record: kept,
		CallTimeout: 2 * time.Second, Now: func() time.Time { return at },
		Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	// Long enough for the first tick, which happens immediately.
	time.Sleep(200 * time.Millisecond)

	turns, _, _ := conversation.seen()
	assert.Empty(t, turns, "the session already ran inside this window")
}

// The same window, with nothing recorded, does run the session: the guard above
// must not be a harness that never wakes anybody.
func TestAWindowWithNothingRecordedStillRuns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation, Declaration: declared, Record: record.NewMemory(),
		CallTimeout: 2 * time.Second,
		Now:         func() time.Time { return time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC) },
		Log:         zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })
}

// A person in the chat is not a session. Reading their name as one would let a
// chat name that matches a session silence that session for the day.
func TestOnlyDeclaredSessionsCountAsHavingRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	at := time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC)
	kept := record.NewMemory()
	require.NoError(t, kept.TurnStarted(context.Background(), record.Turn{
		Ref: "turn-1", ThreadRef: "th-1", StartedAt: at.Add(-time.Hour),
		WokenBy: "wakeup w1", Cause: "своя причина",
	}))

	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation, Declaration: declared, Record: kept,
		CallTimeout: 2 * time.Second, Now: func() time.Time { return at },
		Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })
}

// A turn that outlives its limit is interrupted: the sessions behind it are
// waiting, and a defence that never runs because a scan never finished is worse
// than a scan cut short.
func TestATurnThatOutlivesItsLimitIsInterrupted(t *testing.T) {
	conversation := newConversationSpy()
	chat := newChatDouble()
	kept := record.NewMemory()
	at := time.Date(2026, 8, 25, 17, 13, 0, 0, time.UTC)
	now := at

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри все двадцать бумаг", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	// The turn is still running six minutes later.
	now = at.Add(6 * time.Minute)

	waitFor(t, func() bool { return len(conversation.interrupted()) == 1 })

	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1 && state.Turns[0].Failure != ""
	})
	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Contains(t, state.Turns[0].Failure, "interrupted")
	assert.NotNil(t, state.Turns[0].FinishedAt, "an interrupted turn is closed, not left open")
}

// The agent answering "there is no turn to interrupt" closes the turn instead of
// leaving it open. Treated as a fault it wedges the harness: it goes on believing
// a turn runs, every session queues behind a turn that finished, and the watcher
// asks again every second. This happened live on 25 August and stopped the
// schedule for ten minutes.
func TestATurnThatEndedBeforeTheInterruptIsClosedAnyway(t *testing.T) {
	conversation := newConversationSpy()
	conversation.refuseInterrupt = fmt.Errorf("turn/interrupt: %w: no active turn to interrupt", agent.ErrNoActiveTurn)
	chat := newChatDouble()
	kept := record.NewMemory()
	at := time.Date(2026, 8, 25, 17, 13, 0, 0, time.UTC)
	now := at

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри все двадцать бумаг", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	now = at.Add(6 * time.Minute)
	waitFor(t, func() bool { return len(conversation.interrupted()) >= 1 })

	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1 && state.Turns[0].FinishedAt != nil
	})

	// The harness no longer holds a turn, so the next session can run.
	assert.Empty(t, h.runningTurn(), "a turn the agent says is over is not still held")
}

// A turn inside its limit is left alone, or the harness would cut every session
// short and call it safety.
func TestATurnInsideItsLimitIsLeftAlone(t *testing.T) {
	conversation := newConversationSpy()
	chat := newChatDouble()
	at := time.Date(2026, 8, 25, 17, 13, 0, 0, time.UTC)
	now := at

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри позиции", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	now = at.Add(4 * time.Minute)
	time.Sleep(300 * time.Millisecond)

	assert.Empty(t, conversation.interrupted())
}

// Thirteen sessions saying four things each is a rate limit, and the answer that
// matters waits behind the chatter. What a session says in quick succession is
// thinned - but the last thing it said is always posted, because that is the
// answer.
func TestChatterIsThinnedAndTheLastWordSurvives(t *testing.T) {
	conversation := newConversationSpy()
	chat := newChatDouble()
	at := time.Date(2026, 8, 25, 17, 49, 0, 0, time.UTC)
	now := at

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		SayEvery: 20 * time.Second, CallTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри позиции", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	for _, said := range []string{"проверяю часы", "смотрю счёт", "читаю цепочку", "вошёл в TSLA"} {
		conversation.events <- agent.Event{Kind: agent.KindText, Text: said, TurnID: "tu-1"}
	}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool {
		for _, posted := range chat.postedTexts() {
			if strings.Contains(posted, "вошёл в TSLA") {
				return true
			}
		}
		return false
	})

	said := chat.postedTexts()
	middle := 0
	for _, posted := range said {
		if strings.Contains(posted, "смотрю счёт") || strings.Contains(posted, "читаю цепочку") {
			middle++
		}
	}
	assert.Zero(t, middle, "the chatter between the first word and the answer is held back")
}

// A room that goes away must not take the work with it.
//
// Returning the listener's error from Run ended the whole process, and every
// other role - the ladder walking live orders, the screener, the schedule - died
// with it. Seen on 26 August: the listener returned "context deadline exceeded"
// and the restart landed in the middle of orders that were still being walked.
//
// The room is where the agent reports. The market is where it acts.
func TestALostRoomDoesNotStopTheWork(t *testing.T) {
	chat := newChatDouble()
	chat.listenFails = errors.New("context deadline exceeded")

	h := &Harness{
		Chat: chat, Conversation: newConversationSpy(),
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- h.Run(ctx) }()

	// Run must still be running: it is not allowed to return while the context
	// stands, because returning is what killed everything else.
	select {
	case err := <-stopped:
		t.Fatalf("the harness gave up when the room did: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	// And when the process really is asked to stop, it stops cleanly.
	cancel()
	select {
	case err := <-stopped:
		assert.NoError(t, err, "a lost room is not a reason to fail the process")
	case <-time.After(2 * time.Second):
		t.Fatal("the harness did not return after the context was cancelled")
	}
}

// A turn the agent will not give up holds every session behind it. On 26 August
// one held them for two hours and nineteen minutes: the limit fired every three
// minutes, the interrupt was refused every time, and the harness answered each
// due session with "the agent is working" until a person restarted the container.
//
// So refusals are counted, and past the count the harness stops being the thing
// that waits. Dying costs the conversation and seconds; waiting cost a day.
func TestARefusedInterruptEndsTheProcessRatherThanWaitingForever(t *testing.T) {
	was := interruptAttempts
	interruptAttempts = 2
	t.Cleanup(func() { interruptAttempts = was })

	conversation := newConversationSpy()
	conversation.refuseInterrupt = errors.New("context deadline exceeded")
	chat := newChatDouble()
	at := time.Date(2026, 8, 26, 14, 20, 0, 0, time.UTC)
	now := at

	// Fatal ends the process, which a test cannot survive. The hook makes it end
	// only the goroutine that called it, and the entry itself is what the test
	// waits on - so the escalation is proven, not described.
	gaveUp := make(chan struct{})
	var once sync.Once
	log := zaptest.NewLogger(t, zaptest.WrapOptions(
		zap.WithFatalHook(zapcore.WriteThenGoexit),
		zap.Hooks(func(e zapcore.Entry) error {
			if e.Level == zapcore.FatalLevel {
				once.Do(func() { close(gaveUp) })
			}
			return nil
		}),
	))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: func() time.Time { return now }, Log: log,
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "посмотри все двадцать бумаг", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	now = at.Add(6 * time.Minute)

	select {
	case <-gaveUp:
	case <-time.After(5 * time.Second):
		t.Fatal("the harness kept asking politely instead of ending the process")
	}
	assert.GreaterOrEqual(t, len(conversation.interrupted()), interruptAttempts,
		"it should have asked the agreed number of times before giving up")
}

// The memory of what already ran is kept by session NAME, and a name the
// previous declaration did not have has no entry - which reads as "never ran".
// Left alone, a name the record has already seen today would open a second
// position in a window that already had one, and one with `every:` would fire
// the same second it appeared. The record is asked about the names that are new.
//
// The other half of this test is the limit of the guard: a name the record has
// never seen does run, because a session added at midday and a session renamed
// at midday are the same thing on disk and the added one should run.
func TestANameTheRecordAlreadySawDoesNotRunASecondTimeToday(t *testing.T) {
	now := time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC)

	before, err := declaration.Load(write(t, `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: flatten
    cause: "конец дня"
    task: "Закрой всё."
    at: "15:50"
    within: 20m
`))
	require.NoError(t, err)

	after, err := declaration.Load(write(t, `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: flatten
    cause: "конец дня"
    task: "Закрой всё."
    at: "15:50"
    within: 20m
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
  - name: entry-morning
    cause: "утреннее окно"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`))
	require.NoError(t, err)

	// The turn the window already had, under a name the declaration is about to
	// grow. A session moved between declarations - or a process that had this name
	// before the edit - is exactly what produces this.
	kept := record.NewMemory()
	require.NoError(t, kept.TurnStarted(context.Background(), record.Turn{
		Ref: "turn-1", ThreadRef: "th-1", StartedAt: now.Add(-18 * time.Minute),
		WokenBy: "entry", Cause: "declaration: entry",
	}))

	ctx := context.Background()
	h := &Harness{
		Declaration: before, Record: kept, CallTimeout: time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
		Reread: func() (*declaration.Declaration, error) { return after, nil },
	}
	h.declared = before
	h.lastRun = h.whatAlreadyRanToday(ctx, before)
	require.Empty(t, h.lastRun)

	h.rereadTheDeclaration(ctx)

	assert.Same(t, after, h.declared, "the new declaration is the one in force")
	assert.Equal(t, now.Add(-18*time.Minute), h.lastRun["entry"],
		"what the record says about this name is what the clock must go by")
	assert.False(t, after.Sessions[1].Due(now, h.lastRun["entry"]), "this window already had its session")

	// And the guard must not become a harness that never wakes anybody: a name the
	// record has never seen has never run under that name, and runs. This is also
	// the guard's limit - a session RENAMED inside its own window looks like this
	// and does run twice, which is why renaming happens outside the window.
	assert.True(t, after.Sessions[2].Due(now, h.lastRun["entry-morning"]))
}

// A name this process has already woken keeps the time this process saw. The
// record's time is older - it is written when the turn starts and the process
// knows about turns the record has not been asked about since - and taking it
// would let a session run twice inside one window.
func TestARereadDoesNotForgetWhatThisProcessAlreadyWoke(t *testing.T) {
	now := time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC)
	body := `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`
	before, err := declaration.Load(write(t, body))
	require.NoError(t, err)
	after, err := declaration.Load(write(t, body+"    days: [mon, tue]\n"))
	require.NoError(t, err)

	kept := record.NewMemory()
	require.NoError(t, kept.TurnStarted(context.Background(), record.Turn{
		Ref: "turn-1", ThreadRef: "th-1", StartedAt: now.Add(-30 * time.Minute),
		WokenBy: "entry", Cause: "declaration: entry",
	}))

	ctx := context.Background()
	h := &Harness{
		Declaration: before, Record: kept, CallTimeout: time.Second,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
		Reread: func() (*declaration.Declaration, error) { return after, nil },
	}
	h.declared = before
	h.lastRun = map[string]time.Time{"entry": now.Add(-2 * time.Minute)}

	h.rereadTheDeclaration(ctx)

	assert.Equal(t, now.Add(-2*time.Minute), h.lastRun["entry"])
}

// A file that cannot be used leaves the clock exactly as it was: an agent whose
// schedule vanished halfway through a save wakes nobody for the rest of the day.
func TestARereadThatFailsLeavesTheClockAlone(t *testing.T) {
	declared, err := declaration.Load(write(t, `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`))
	require.NoError(t, err)

	h := &Harness{
		Declaration: declared, CallTimeout: time.Second,
		Now: func() time.Time { return time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC) },
		Log: zaptest.NewLogger(t),
		Reread: func() (*declaration.Declaration, error) {
			return declared, errors.New("the file on disk is half-saved")
		},
	}
	h.declared = declared
	h.lastRun = map[string]time.Time{}

	h.rereadTheDeclaration(context.Background())

	assert.Same(t, declared, h.declared)
}

// write puts a declaration in a file of its own, so a test can hold two versions
// of the same schedule at once.
func write(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

// The clock is what carries an edit to a running session. Nothing here restarts:
// the harness is started once, the thing behind Reread changes underneath it,
// and the next tick has to pick it up.
//
// This is the half of "an edit reaches a session already running" that lives in
// the harness. The other half - that the edit is a skill's text and that the
// directory the agent reads is rebuilt from it - is proven in internal/skills.
func TestARunningHarnessPicksUpAChangeWithoutARestart(t *testing.T) {
	// The real tick is a minute, which is right for a schedule written in minutes
	// and wrong for standing here waiting for one.
	clockTick = 20 * time.Millisecond
	t.Cleanup(func() { clockTick = time.Minute })

	body := `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`
	before, err := declaration.Load(write(t, body))
	require.NoError(t, err)
	after, err := declaration.Load(write(t, body+"    days: [mon, tue]\n"))
	require.NoError(t, err)

	var mu sync.Mutex
	current, asked := before, 0
	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation, Declaration: before, Record: record.NewMemory(),
		CallTimeout: 2 * time.Second,
		// Saturday: nothing is due, so this test measures the re-reading and
		// nothing else.
		Now: func() time.Time { return time.Date(2026, 8, 29, 14, 40, 0, 0, time.UTC) },
		Log: zaptest.NewLogger(t),
		Reread: func() (*declaration.Declaration, error) {
			mu.Lock()
			defer mu.Unlock()
			asked++

			return current, nil
		},
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return asked > 0 })

	// What is on disk changes while the process runs. Nothing is restarted.
	mu.Lock()
	current = after
	mu.Unlock()

	waitFor(t, func() bool { return h.inForce() == after })
	assert.Equal(t, []string{"mon", "tue"}, h.inForce().Sessions[0].Days,
		"the schedule in force is the edited one")
}
