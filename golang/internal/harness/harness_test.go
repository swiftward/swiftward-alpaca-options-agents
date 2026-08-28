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
	// turnErrOnce is refused to the FIRST turn only, so a test can fail the cause
	// that wins a race and watch the one that lost recover.
	turnErrOnce error
	// slowTurn is how long the agent takes to answer with a turn id.
	slowTurn time.Duration
	nextTurn int
	// forgotten counts how many times the harness gave up on this conversation
	// and asked for a fresh one.
	forgotten int
}

func newConversationSpy() *conversationSpy {
	return &conversationSpy{events: make(chan agent.Event, 16)}
}

func (c *conversationSpy) Open(context.Context) (string, error) { return "th-1", nil }

func (c *conversationSpy) Forget() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forgotten++
}

func (c *conversationSpy) timesForgotten() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.forgotten
}

func (c *conversationSpy) Turn(ctx context.Context, text, model string) (string, error) {
	// slowTurn holds the answer back the way a real one is held back: the id
	// exists only after the agent answers, and everything racing to start a turn
	// races inside that window.
	if c.slowTurn > 0 {
		select {
		case <-time.After(c.slowTurn):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
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
	if c.turnErrOnce != nil {
		err := c.turnErrOnce
		c.turnErrOnce = nil

		return "", err
	}
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
	// With the record rather than without: half of what the harness does is the
	// record, and a fixture without one would check only the other half.
	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}

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
    cause: "close everything before the day ends"
    task: "Close all positions."
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
	assert.Contains(t, turns[0], "close everything before the day ends", "the session must carry why it was woken")
	assert.Contains(t, turns[0], "Close all positions")
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
    cause: "checking the defence rules"
    task: "Check the positions."
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
	assert.Contains(t, posted[len(posted)-1], "✅ joker · done", "the result belongs at the bottom, where the reader is")
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
	assert.Contains(t, chat.postedTexts()[0], "nothing is running")
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
		Cause: "look at how the position opened",
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
	assert.Contains(t, turns[0], "look at how the position opened")
}

// A price wake-up needs a reading, and the harness asks only for the symbols
// something is actually watching.
func TestAPriceWakeUpFiresOnAReading(t *testing.T) {
	now := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	standing := &wakeupsDouble{
		standing: []wakeup.Wakeup{{
			ID: "w2", Kind: wakeup.KindPrice, Symbol: "SPY", Direction: wakeup.Below, Level: 760,
			Cause: "the price came up to the sold strike",
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
	assert.Contains(t, turns[0], "the sold strike")
	assert.Equal(t, [][]string{{"SPY"}}, prices.asked)
}

// With nothing watching a price, the broker is not asked at all: a reading
// nobody needs is a request nobody should pay for.
func TestNoPriceIsReadWhenNothingWatchesOne(t *testing.T) {
	now := time.Date(2026, 8, 24, 15, 0, 0, 0, time.UTC)
	standing := &wakeupsDouble{standing: []wakeup.Wakeup{{
		ID: "w3", Kind: wakeup.KindAt, At: now.Add(time.Hour), Cause: "later",
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
	assert.Contains(t, chat.statusTexts()[0], "did not start")
	assert.Contains(t, chat.statusTexts()[0], "agent is not answering")
}

func TestHowLongReadsLikeRussian(t *testing.T) {
	cases := map[time.Duration]string{
		400 * time.Millisecond:               "a moment",
		7 * time.Second:                      "7s",
		90 * time.Second:                     "1m 30s",
		4 * time.Minute:                      "4m",
		2*time.Minute + 500*time.Millisecond: "2m 1s",
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
    cause: "close everything before the day ends"
    task: "Close all positions."
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
	assert.Contains(t, state.Turns[0].Cause, "close everything before the day ends")

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

	chat.inbound <- telegram.Message{Text: "what is on the account?", Username: "joker"}

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
	_, err = store.AddAt("check the spread after the news", at, at.Add(-time.Minute))
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

	chat.inbound <- telegram.Message{Text: "look at the positions", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	waitFor(t, func() bool { _, steered, _ := conversation.seen(); return len(steered) == 1 })

	_, steered, _ := conversation.seen()
	assert.Contains(t, steered[0], "check the spread after the news")

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
    cause: "entry window"
    task: "Sell a spread."
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
    cause: "entry window"
    task: "Sell a spread."
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
    cause: "entry window"
    task: "Sell a spread."
    at: "14:20"
    within: 45m
`), 0o600))

	declared, err := declaration.Load(path)
	require.NoError(t, err)

	at := time.Date(2026, 8, 25, 14, 40, 0, 0, time.UTC)
	kept := record.NewMemory()
	require.NoError(t, kept.TurnStarted(context.Background(), record.Turn{
		Ref: "turn-1", ThreadRef: "th-1", StartedAt: at.Add(-time.Hour),
		WokenBy: "wakeup w1", Cause: "a reason of its own",
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

// movingClock is a clock a running harness reads while the test moves it. A
// plain variable would be written by the test goroutine and read by the
// harness's, which is a data race whatever the value - and the race detector
// fails the whole package for it, so a gate that should be about behaviour
// starts failing about itself.
type movingClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock(at time.Time) *movingClock { return &movingClock{now: at} }

func (c *movingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *movingClock) Set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = at
}

// When the cause that won the race fails to start its turn, the one that lost
// does not lose its prompt.
//
// A wake-up is consumed when it fires, so a dropped one never comes back. The
// loser waits for the winner's turn to steer into - and if the winner never gets
// one, waiting out the deadline would spend the prompt on nothing. It starts one
// itself instead.
func TestWhenTheWinnerFailsToStartTheLoserStillGetsATurn(t *testing.T) {
	conversation := newConversationSpy()
	conversation.slowTurn = 100 * time.Millisecond
	conversation.turnErrOnce = fmt.Errorf("the agent refused the turn")
	chat := newChatDouble()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	// The clock's cause goes first and will fail. The room's arrives only once
	// that one holds the claim, so which cause loses is decided rather than
	// raced - otherwise the test proves whichever path it happened to take.
	go h.Tell(ctx, "the entry window is open", "entry")
	waitFor(t, func() bool { _, opening := h.turnState(); return opening })
	chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}

	waitFor(t, func() bool { return h.runningTurn() != "" })

	// Both prompts are carried, and neither is dropped for the refusal. Which of
	// the two roads each took is not the point: the winner retried its own start
	// after its first was refused, and the loser either steered into that turn or
	// started one of its own. What must never happen is a prompt that reached
	// nothing.
	waitFor(t, func() bool {
		turns, steered, _ := conversation.seen()

		return said(turns, steered, "what do we hold") && said(turns, steered, "the entry window is open")
	})
}

// said reports whether a prompt reached the agent at all - as a turn of its own
// or said into one already running.
func said(turns, steered []string, want string) bool {
	for _, text := range append(append([]string{}, turns...), steered...) {
		if strings.Contains(text, want) {
			return true
		}
	}

	return false
}

// A turn that ends before its name reaches us is still finished.
//
// The id exists only when Conversation.Turn returns. An agent quick enough to
// answer and finish inside that window sent a completion for a turn this process
// could not yet name, and the completion was dropped: the turn was then published
// as running, every session queued behind it, and the record's row stayed open
// until the interrupt came minutes later - or forever where no limit is set.
func TestATurnThatEndsBeforeItsNameArrivesIsStillFinished(t *testing.T) {
	conversation := newConversationSpy()
	conversation.slowTurn = 200 * time.Millisecond
	chat := newChatDouble()
	kept := record.NewMemory()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}
	// The agent answers and finishes while Turn is still returning: the id is
	// known to it, not yet to us.
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })
	conversation.events <- agent.Event{Kind: agent.KindText, TurnID: "tu-1", Text: "nothing is open"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool { return h.runningTurn() == "" })

	waitFor(t, func() bool {
		state, err := kept.Read(context.Background())

		return err == nil && len(state.Turns) == 1 && state.Turns[0].FinishedAt != nil
	})

	// And the next cause is not queued behind a turn that is over.
	chat.inbound <- telegram.Message{Text: "and now", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 2 })
}

// A shell command goes into the record by NAME only.
//
// The record is served, unauthenticated, on the page a judge opens. What the
// agent typed at a shell and what the machine printed back are neither: one
// `env` in a session that read a poisoned headline would put this stack's
// database password, gateway token and chat token on a public address. What the
// broker and the session's own tools were called with stays in full - that is
// what the page exists to show.
func TestAShellCommandIsRecordedWithoutItsArgumentsOrOutput(t *testing.T) {
	harness, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "look at the notes", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	// The shape a real shell call has: the agent's protocol gives it no tool name,
	// so the COMMAND LINE becomes the name (see itemEvent.call). A test that puts
	// a tidy word there instead cannot catch a credential in the command itself.
	const typed = "sh -c 'echo $GATEWAY_TOKEN'"
	conversation.events <- agent.Event{Kind: agent.KindToolStarted, TurnID: "tu-1",
		Call: agent.Call{Ref: "call-shell", Server: agent.ServerShell, Tool: typed,
			Arguments: []byte(`{"command":"sh -c 'echo $GATEWAY_TOKEN'"}`), Status: "inProgress"}}
	conversation.events <- agent.Event{Kind: agent.KindTool, TurnID: "tu-1",
		Call: agent.Call{Ref: "call-shell", Server: agent.ServerShell, Tool: typed,
			Status: "failed", Failure: "sh -c 'echo $GATEWAY_TOKEN': exit 1",
			Answer: "GATEWAY_TOKEN=hunter2 POSTGRES_PASSWORD=hunter2"}}

	conversation.events <- agent.Event{Kind: agent.KindToolStarted, TurnID: "tu-1",
		Call: agent.Call{Ref: "call-broker", Server: "broker", Tool: "get_clock",
			Arguments: []byte(`{"detail":true}`), Status: "inProgress"}}
	conversation.events <- agent.Event{Kind: agent.KindTool, TurnID: "tu-1",
		Call: agent.Call{Ref: "call-broker", Server: "broker", Tool: "get_clock",
			Status: "completed", Answer: `{"is_open":true}`}}

	waitFor(t, func() bool {
		state, err := harness.Record.Read(context.Background())

		return err == nil && len(state.Calls) == 2
	})

	state, err := harness.Record.Read(context.Background())
	require.NoError(t, err)
	byRef := map[string]record.ToolCall{}
	for _, call := range state.Calls {
		byRef[call.Ref] = call
	}

	shell := byRef["call-shell"]
	assert.Equal(t, "a shell command", shell.Tool, "that one ran is on the record; what it was is not")
	assert.Empty(t, shell.Arguments, "nor what it was called with")
	assert.Empty(t, shell.Answer, "nor what it printed")
	assert.Empty(t, shell.Failure, "nor the error, which quotes the command back")
	for _, field := range []string{shell.Tool, string(shell.Arguments), shell.Answer, shell.Failure} {
		assert.NotContains(t, field, "GATEWAY_TOKEN", "no part of it reaches the page")
	}

	broker := byRef["call-broker"]
	assert.NotEmpty(t, broker.Arguments, "a broker call is recorded in full")
	assert.Contains(t, broker.Answer, "is_open")
}

// Two causes arriving together start ONE turn, not two.
//
// The clock and the room run in different goroutines, and each used to look at
// the same empty turnID, find nobody working and start a session. Two sessions
// then traded one account on one thread, and the first was orphaned the moment
// the second overwrote the id: never closed in the record, never closed in the
// room. The prompt that lost the race is not dropped - it goes into the turn
// that won.
//
// The double start is what this proves, so the two causes are released at the
// same moment: the turn is made slow to open, and the message arrives while it
// is opening.
func TestTwoCausesArrivingTogetherStartOneTurn(t *testing.T) {
	conversation := newConversationSpy()
	conversation.slowTurn = 150 * time.Millisecond
	chat := newChatDouble()
	at := time.Date(2026, 8, 25, 14, 20, 0, 0, time.UTC)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		CallTimeout: 2 * time.Second,
		Now:         func() time.Time { return at },
		Log:         zaptest.NewLogger(t),
	}

	// The clock's cause and the room's cause, together.
	go h.Tell(ctx, "the entry window is open", "entry")
	go func() { _ = h.Run(ctx) }()
	chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}

	waitFor(t, func() bool {
		_, steered, _ := conversation.seen()

		return len(steered) == 1
	})

	turns, steered, _ := conversation.seen()
	assert.Len(t, turns, 1, "one turn, whichever cause won the race")
	assert.Len(t, steered, 1, "and the cause that lost said its piece into that turn")
}

// A turn that outlives its limit is interrupted: the sessions behind it are
// waiting, and a defence that never runs because a scan never finished is worse
// than a scan cut short.
func TestATurnThatOutlivesItsLimitIsInterrupted(t *testing.T) {
	conversation := newConversationSpy()
	chat := newChatDouble()
	kept := record.NewMemory()
	at := time.Date(2026, 8, 25, 17, 13, 0, 0, time.UTC)
	clock := newClock(at)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: clock.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at all twenty underlyings", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	// The turn is still running six minutes later.
	clock.Set(at.Add(6 * time.Minute))

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
	clock := newClock(at)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: clock.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at all twenty underlyings", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	clock.Set(at.Add(6 * time.Minute))
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
	clock := newClock(at)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: record.NewMemory(),
		TurnLimit: 5 * time.Minute, CallTimeout: 2 * time.Second,
		Now: clock.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at the positions", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	clock.Set(at.Add(4 * time.Minute))
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

	chat.inbound <- telegram.Message{Text: "look at the positions", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	for _, said := range []string{"checking the clock", "looking at the account", "reading the chain", "entered TSLA"} {
		conversation.events <- agent.Event{Kind: agent.KindText, Text: said, TurnID: "tu-1"}
	}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool {
		for _, posted := range chat.postedTexts() {
			if strings.Contains(posted, "entered TSLA") {
				return true
			}
		}
		return false
	})

	said := chat.postedTexts()
	middle := 0
	for _, posted := range said {
		if strings.Contains(posted, "looking at the account") || strings.Contains(posted, "reading the chain") {
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
	clock := newClock(at)

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
		Now: clock.Now, Log: log,
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at all twenty underlyings", Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	clock.Set(at.Add(6 * time.Minute))

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
    cause: "end of the day"
    task: "Close everything."
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
    cause: "end of the day"
    task: "Close everything."
    at: "15:50"
    within: 20m
  - name: entry
    cause: "entry window"
    task: "Sell a spread."
    at: "14:20"
    within: 45m
  - name: entry-morning
    cause: "the morning window"
    task: "Sell a spread."
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
    cause: "entry window"
    task: "Sell a spread."
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
    cause: "entry window"
    task: "Sell a spread."
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
	body := `
kind: trading-agent
name: options-alpha
timezone: UTC
sessions:
  - name: entry
    cause: "entry window"
    task: "Sell a spread."
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
		// The real tick is a minute, which is right for a schedule written in
		// minutes and wrong for standing here waiting for one.
		TickEvery: 20 * time.Millisecond,
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

// The numbers this agent runs on go in front of EVERY turn, not only a
// scheduled one. A session woken by its own price wake-up is asked to follow the
// same techniques, and the playbook tells a session that cannot see its numbers
// to open nothing - so a wake-up without them would be a turn that can only
// report a fault.
func TestEveryCauseCarriesTheNumbersTheAgentRunsOn(t *testing.T) {
	declared, err := declaration.Load(write(t, `
kind: trading-agent
name: options-alpha
timezone: UTC
parameters:
  short_leg_delta: "0.15 in absolute value"
sessions:
  - name: entry
    cause: "entry window"
    task: "Sell a spread."
    at: "14:20"
    within: 45m
`))
	require.NoError(t, err)

	chat := newChatDouble()
	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Declaration: declared,
		Record: record.NewMemory(), CallTimeout: 2 * time.Second,
		// Saturday, outside every window: nothing the clock does interferes.
		Now: func() time.Time { return time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC) },
		Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	// A wake-up the session asked itself for, in its own words.
	h.Tell(ctx, "Woken by a condition you set: SPY passed 640, check the positions", "wakeup w1")
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "short_leg_delta = 0.15 in absolute value",
		"a turn woken by the session's own request still carries the agent's numbers")
	assert.Contains(t, turns[0], "SPY passed 640", "and still says why it was woken")

	// And a person writing in the chat is the third cause, with the same answer.
	chat.inbound <- telegram.Message{Text: "run premium-harvest", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, steered, _ := conversation.seen(); return len(turns)+len(steered) == 2 })

	turns, steered, _ := conversation.seen()
	if len(turns) == 2 {
		assert.Contains(t, turns[1], "short_leg_delta = 0.15 in absolute value")
	} else {
		// It went into the turn already running, which already carries them.
		require.Len(t, steered, 1)
		assert.Contains(t, turns[0], "short_leg_delta = 0.15 in absolute value")
	}
}

// The record answers "why did this turn happen" long after the room has
// scrolled away, so the cause it files is the window, the wake-up or the message
// - never the fixed sentence the agent's numbers open with. The older guard
// missed this because its declaration carried no parameters at all.
func TestTheRecordedCauseIsWhatWokeTheSessionNotTheNumbers(t *testing.T) {
	declared, err := declaration.Load(write(t, `
kind: trading-agent
name: options-alpha
timezone: UTC
parameters:
  short_leg_delta: "0.15 in absolute value"
sessions:
  - name: flatten
    cause: "close everything before the day ends"
    task: "Close all positions."
    at: "15:50"
    within: 20m
`))
	require.NoError(t, err)

	kept := record.NewMemory()
	conversation := newConversationSpy()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Conversation: conversation, Declaration: declared, Record: kept,
		CallTimeout: 2 * time.Second, TickEvery: 20 * time.Millisecond,
		Now: func() time.Time { return time.Date(2026, 8, 24, 15, 50, 0, 0, time.UTC) },
		Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)

		return err == nil && len(state.Turns) == 1
	})

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 1)
	assert.Equal(t, "flatten", state.Turns[0].WokenBy)
	assert.Contains(t, state.Turns[0].Cause, "close everything before the day ends")
	assert.NotContains(t, state.Turns[0].Cause, "Numbers this agent runs on")

	// And the numbers did reach the session - the cause is trimmed, not the
	// prompt.
	turns, _, _ := conversation.seen()
	assert.Contains(t, turns[0], "short_leg_delta = 0.15 in absolute value")
}

// A turn where the agent produced neither a word nor a tool call did not decide
// to do nothing - it never ran. On 27 August the model gateway began refusing
// the agent's credential and eight turns in a row ended in ten seconds having
// done nothing; every one of them was recorded as finished with no failure, and
// the room showed a tick. The record has to say the opposite of "fine" here.
func TestASilentTurnIsRecordedAsAFailure(t *testing.T) {
	conversation := newConversationSpy()
	kept := record.NewMemory()
	chat := newChatDouble()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at the market"}
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1
	})

	// The agent ends the turn without a single text or tool event.
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1 && state.Turns[0].FinishedAt != nil
	})

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, record.SilentFailure, state.Turns[0].Failure,
		"a turn that produced nothing was recorded as a clean finish, so nobody will ever look at it again")
}

// The same turn, once the agent has actually spoken, must NOT be marked failed:
// a session that looked and had nothing to do says one line and that is success.
func TestATurnThatSpokeIsNotMarkedFailed(t *testing.T) {
	conversation := newConversationSpy()
	kept := record.NewMemory()
	chat := newChatDouble()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	h := &Harness{
		Chat: chat, Conversation: conversation, Record: kept,
		CallTimeout: 2 * time.Second, Now: time.Now, Log: zaptest.NewLogger(t),
	}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "look at the market"}
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1
	})

	conversation.events <- agent.Event{Kind: agent.KindText, Text: "no open positions"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}
	waitFor(t, func() bool {
		state, err := kept.Read(ctx)
		return err == nil && len(state.Turns) == 1 && state.Turns[0].FinishedAt != nil
	})

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, state.Turns[0].Failure, "a turn that said its one line is a finished turn, not a failure")
}

// Turns that speak but never reach the broker end the conversation.
//
// The agent reads its list of tools ONCE per connection, so a broker that blinks
// - a gateway restarted, a name that stopped resolving for a minute - is dropped
// from that list and does not come back. The session then wakes on schedule,
// reasons carefully and does nothing, turn after turn, and nobody logs an error
// because there are no calls to fail. Measured on 27 August: in one thread
// get_clock answered in 0.56 seconds at 12:11, and by 13:38 the session reported
// that only its own tools were left.
//
// Restarting the process does not cure it - the thread is remembered on purpose
// and a restart resumes the same one. Only a fresh conversation reconnects the
// servers.
func TestTurnsThatNeverReachTheBrokerEndTheConversation(t *testing.T) {
	_, chat, conversation := start(t)

	for turn := 1; turn <= 2; turn++ {
		chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}
		waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == turn })

		id := fmt.Sprintf("tu-%d", turn)
		conversation.events <- agent.Event{Kind: agent.KindText, TurnID: id,
			Text: "the broker's tools are not in my list this run"}
		conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: id}

		// The next message must start a NEW turn, not be steered into this one, so
		// wait until this one is actually finished.
		want := turn
		waitFor(t, func() bool { return finishedTurns(chat) == want })
	}

	waitFor(t, func() bool { return conversation.timesForgotten() == 1 })
	assert.Equal(t, 1, conversation.timesForgotten(),
		"two turns in a row that spoke and never called the broker: the conversation is not worth resuming")
}

// One such turn is not enough. A date guard that says "not today" and a defence
// run that finds nothing to defend both end early and honestly without asking
// the broker anything, and neither is a reason to throw the conversation away.
func TestOneTurnWithoutTheBrokerIsNotEnoughToStartOver(t *testing.T) {
	_, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "anything to do", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })
	conversation.events <- agent.Event{Kind: agent.KindText, TurnID: "tu-1", Text: "not today, finishing"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool { return len(chat.postedTexts()) >= 2 })
	assert.Zero(t, conversation.timesForgotten(), "one quiet turn is an answer, not a fault")
}

// A turn that DID reach the broker clears the count, so two brokerless turns
// with a healthy one between them are not two in a row.
func TestReachingTheBrokerClearsTheCount(t *testing.T) {
	_, chat, conversation := start(t)

	send := func(turn int, reached bool) {
		chat.inbound <- telegram.Message{Text: "go", UserID: 42, Username: "joker"}
		waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == turn })
		id := fmt.Sprintf("tu-%d", turn)
		if reached {
			conversation.events <- agent.Event{Kind: agent.KindToolStarted, TurnID: id,
				Call: agent.Call{Ref: "call-" + id, Server: "broker", Tool: "get_clock", Status: "inProgress"}}
		}
		conversation.events <- agent.Event{Kind: agent.KindText, TurnID: id, Text: "done"}
		conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: id}
		waitFor(t, func() bool { return finishedTurns(chat) == turn })
	}

	send(1, false)
	send(2, true)
	send(3, false)
	assert.Zero(t, conversation.timesForgotten(),
		"a healthy turn between two quiet ones means the tools are there")
}

// finishedTurns counts the turns the room has been told are done.
func finishedTurns(chat *chatDouble) int {
	done := 0
	for _, text := range chat.postedTexts() {
		if strings.Contains(text, "· done in") || strings.Contains(text, "the turn did not happen") {
			done++
		}
	}

	return done
}

// What the agent said goes into the record beside the turn it belongs to.
//
// The calls and their answers show WHAT happened. They do not show why, and that
// is what a judge opens the address for: to see the decision in words, not only
// the curve it left behind. The agent's transcript holds the same words, but in
// its own format, in files, and with no link to our turns.
func TestWhatTheAgentSaysIsWrittenDownBesideItsTurn(t *testing.T) {
	harness, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	conversation.events <- agent.Event{Kind: agent.KindText, TurnID: "tu-1",
		Text: "Not entering: a fresh quote gave 2.2 against a threshold of 3."}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool {
		state, err := harness.Record.Read(context.Background())

		return err == nil && len(state.Said) == 1
	})

	state, err := harness.Record.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, state.Said, 1)
	assert.Equal(t, "tu-1", state.Said[0].TurnRef, "a line is tied to its own turn")
	assert.Contains(t, state.Said[0].Text, "against a threshold of 3",
		"the whole decision is written, not a summary of it")
}

// An empty line does not count as a record: the agent sometimes sends whitespace
// between pieces of work, and a page full of blank lines reads worse than a short
// one.
func TestEmptyLinesAreNotWrittenDown(t *testing.T) {
	harness, chat, conversation := start(t)

	chat.inbound <- telegram.Message{Text: "what do we hold", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { turns, _, _ := conversation.seen(); return len(turns) == 1 })

	conversation.events <- agent.Event{Kind: agent.KindText, TurnID: "tu-1", Text: "   \n  "}
	conversation.events <- agent.Event{Kind: agent.KindText, TurnID: "tu-1", Text: "done"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool {
		state, err := harness.Record.Read(context.Background())

		return err == nil && len(state.Said) == 1
	})

	state, err := harness.Record.Read(context.Background())
	require.NoError(t, err)
	assert.Len(t, state.Said, 1, "one line of two is written: the empty one does not count")
}

// The clock and the market are asked at different rates, because they cost
// different things: the schedule lives in memory, a price costs one of the two
// hundred requests a minute the whole stand shares.
//
// The tick here is every call; PriceEvery is thirty seconds. So a market read
// belongs to the first call and to every call thirty seconds after the last
// read, and not to the four calls in between.
func TestThePriceIsReadOnItsOwnCadenceNotOnEveryTick(t *testing.T) {
	start := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	clock := start

	standing := &wakeupsDouble{
		standing: []wakeup.Wakeup{{
			ID: "w1", Kind: wakeup.KindPrice, Symbol: "SPY", Direction: wakeup.Below, Level: 1,
			Cause: "уровень, до которого рынок не дойдёт",
		}},
		watching: []string{"SPY"},
	}
	prices := &pricesDouble{last: map[string]float64{"SPY": 760}}

	h := &Harness{
		Conversation: newConversationSpy(),
		Wakeups:      standing,
		Prices:       prices,
		PriceEvery:   30 * time.Second,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return clock },
		Log:          zaptest.NewLogger(t),
	}

	// Семь тактов по десять секунд: 0, 10, 20, 30, 40, 50, 60.
	for i := 0; i < 7; i++ {
		h.fireWakeups(t.Context())
		clock = clock.Add(10 * time.Second)
	}

	assert.Equal(t, 3, prices.timesAsked(),
		"читать цену надо на нулевой, тридцатой и шестидесятой секунде, а не семь раз")
}

// A tick that skips the market still fires a wake-up waiting for a TIME. The
// price cadence must not slow the clock down: a session that asked to be woken
// at a moment is woken at that moment, whatever the market is being asked.
func TestATimeWakeupFiresOnATickThatReadsNoPrice(t *testing.T) {
	start := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	clock := start

	standing := &wakeupsDouble{
		standing: []wakeup.Wakeup{
			{ID: "w1", Kind: wakeup.KindPrice, Symbol: "SPY", Direction: wakeup.Below, Level: 1, Cause: "не сработает"},
			{ID: "w2", Kind: wakeup.KindAt, At: start.Add(10 * time.Second), Cause: "разбуди меня через десять секунд"},
		},
		watching: []string{"SPY"},
	}
	prices := &pricesDouble{last: map[string]float64{"SPY": 760}}
	conversation := newConversationSpy()

	h := &Harness{
		Conversation: conversation,
		Wakeups:      standing,
		Prices:       prices,
		PriceEvery:   time.Hour,
		CallTimeout:  2 * time.Second,
		Now:          func() time.Time { return clock },
		Log:          zaptest.NewLogger(t),
	}

	h.fireWakeups(t.Context())
	clock = clock.Add(10 * time.Second)
	h.fireWakeups(t.Context())

	turns, _, _ := conversation.seen()
	assert.Len(t, turns, 1, "пробуждение по времени обязано сработать на такте, который рынок не спрашивал")
	assert.Contains(t, turns[0], "через десять секунд")
	assert.Equal(t, 1, prices.timesAsked(), "рынок спрашивается один раз за час, а не на каждом такте")
}

// Reading the disk is the one thing a tick does that is not free, so it has its
// own cadence. A finer tick has to buy accuracy in the schedule without buying
// the reading, or lowering it stops being cheap and the whole separation is a
// lie the operator pays for.
func TestTheDeclarationIsRereadOnItsOwnCadenceNotOnEveryTick(t *testing.T) {
	start := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	clock := start

	var reads int
	h := &Harness{
		Conversation: newConversationSpy(),
		Reread: func() (*declaration.Declaration, error) {
			reads++

			return nil, nil
		},
		RereadEvery: 30 * time.Second,
		CallTimeout: 2 * time.Second,
		Now:         func() time.Time { return clock },
		Log:         zaptest.NewLogger(t),
	}

	// Семь тактов по десять секунд: 0, 10, 20, 30, 40, 50, 60.
	for i := 0; i < 7; i++ {
		h.tick(t.Context())
		clock = clock.Add(10 * time.Second)
	}

	assert.Equal(t, 3, reads,
		"диск читается на нулевой, тридцатой и шестидесятой секунде, а не семь раз")
}

// A clock that steps backwards has to read as due. The other way round is a
// trap: the difference goes negative, the caller waits, the stamp never moves,
// and a jump back of an hour stops the reading for an hour.
func TestAClockThatStepsBackDoesNotStopTheReading(t *testing.T) {
	start := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	last := start

	assert.False(t, elapsed(&last, start.Add(time.Second), time.Minute),
		"секунда из минуты ещё не прошла")
	assert.True(t, elapsed(&last, start.Add(-time.Hour), time.Minute),
		"часы шагнули назад - читаем, а не залипаем")
	assert.Equal(t, start.Add(-time.Hour), last,
		"после чтения отметка обязана переехать на новое время, иначе следующее чтение снова ждёт")
}
