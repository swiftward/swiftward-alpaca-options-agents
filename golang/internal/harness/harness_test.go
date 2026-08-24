package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// chatDouble accepts what production accepts and refuses what production
// refuses: an empty message reaches Telegram from neither.
type chatDouble struct {
	mu       sync.Mutex
	inbound  chan telegram.Message
	posted   []string
	statuses []string
	nextID   int
}

func newChatDouble() *chatDouble {
	return &chatDouble{inbound: make(chan telegram.Message, 8)}
}

func (c *chatDouble) Listen(ctx context.Context) error {
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
	steered    []string
	interrupts []string
	turnErr    error
	steerErr   error
	nextTurn   int
}

func newConversationSpy() *conversationSpy {
	return &conversationSpy{events: make(chan agent.Event, 16)}
}

func (c *conversationSpy) Open(context.Context) (string, error) { return "th-1", nil }

func (c *conversationSpy) Turn(ctx context.Context, text string) (string, error) {
	if c.hang != nil {
		c.mu.Lock()
		c.turns = append(c.turns, text)
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

func (c *conversationSpy) Interrupt(_ context.Context, turnID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.interrupts = append(c.interrupts, turnID)
	return nil
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
	assert.Equal(t, []string{"working"}, chat.postedTexts())

	conversation.events <- agent.Event{Kind: agent.KindTool, Tool: "get_option_chain", TurnID: "tu-1"}
	conversation.events <- agent.Event{Kind: agent.KindText, Text: "the spread is closed", TurnID: "tu-1"}
	conversation.events <- agent.Event{Kind: agent.KindTurnDone, TurnID: "tu-1"}

	waitFor(t, func() bool { return len(chat.statusTexts()) >= 2 })
	assert.Contains(t, chat.statusTexts(), "working: get_option_chain")
	assert.Contains(t, chat.statusTexts(), "done")
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
	waitFor(t, func() bool { return len(chat.statusTexts()) >= 1 })

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
