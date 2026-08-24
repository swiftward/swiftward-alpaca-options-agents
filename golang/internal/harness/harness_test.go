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

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/session"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// chatDouble stands in for the room. It accepts what production accepts and
// refuses what production refuses - an empty message reaches Telegram from
// neither.
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

func (c *chatDouble) Edit(_ context.Context, messageID int, text string) error {
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

type sessionSpy struct {
	mu       sync.Mutex
	requests []session.Request
	result   session.Result
	err      error
	text     []string
	tools    []string
}

func (s *sessionSpy) Run(_ context.Context, req session.Request, on session.Handlers) (session.Result, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	for _, name := range s.tools {
		if on.Tool != nil {
			on.Tool(name)
		}
	}
	for _, t := range s.text {
		if on.Text != nil {
			on.Text(t)
		}
	}

	return s.result, s.err
}

func (s *sessionSpy) seen() []session.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]session.Request(nil), s.requests...)
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

func TestRefusesWithoutAnyCause(t *testing.T) {
	h := &Harness{Log: zaptest.NewLogger(t)}
	err := h.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cause to wake a session")
}

func TestDeclarationFormatRefusesLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte("kind: trading-agent\n"), 0o600))

	h := &Harness{DeclarationPath: path, Log: zaptest.NewLogger(t)}
	err := h.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}

func TestAMessageWakesASessionAndTheChatSeesIt(t *testing.T) {
	chat := newChatDouble()
	agent := &sessionSpy{
		result: session.Result{ThreadID: "t1", LastMessage: "flat"},
		text:   []string{"closing the spread", "flat"},
		tools:  []string{"get_option_chain"},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Harness{Chat: chat, Sessions: agent, Sandbox: "workspace-write", Log: zaptest.NewLogger(t)}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "close everything", UserID: 42, Username: "joker"}

	waitFor(t, func() bool { return len(chat.postedTexts()) >= 3 })

	posted := chat.postedTexts()
	assert.Equal(t, "working", posted[0])
	assert.Contains(t, posted, "closing the spread")
	assert.Contains(t, posted, "flat")

	requests := agent.seen()
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0].Prompt, "close everything")
	assert.Contains(t, requests[0].Prompt, "joker", "the session must be able to say who woke it")
	assert.Equal(t, "workspace-write", requests[0].Sandbox)
	assert.Empty(t, requests[0].ThreadID, "the first session has nothing to continue")

	waitFor(t, func() bool { return len(chat.statusTexts()) >= 2 })
	assert.Contains(t, chat.statusTexts(), "working: get_option_chain")
	assert.Contains(t, chat.statusTexts(), "done")
}

func TestTheNextMessageContinuesTheSameThread(t *testing.T) {
	chat := newChatDouble()
	agent := &sessionSpy{result: session.Result{ThreadID: "t1"}, text: []string{"ok"}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Harness{Chat: chat, Sessions: agent, Log: zaptest.NewLogger(t)}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "what is open?", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { return len(agent.seen()) == 1 })

	chat.inbound <- telegram.Message{Text: "and the second leg?", UserID: 42, Username: "joker"}
	waitFor(t, func() bool { return len(agent.seen()) == 2 })

	assert.Equal(t, "t1", agent.seen()[1].ThreadID)
}

// A session that dies must say so in the room. Silence reads as "still working"
// and nobody goes looking.
func TestAFailedSessionIsReportedInTheChat(t *testing.T) {
	chat := newChatDouble()
	agent := &sessionSpy{err: fmt.Errorf("agent exited with status 3")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := &Harness{Chat: chat, Sessions: agent, Log: zaptest.NewLogger(t)}
	go func() { _ = h.Run(ctx) }()

	chat.inbound <- telegram.Message{Text: "go", UserID: 42, Username: "joker"}

	waitFor(t, func() bool {
		for _, s := range chat.statusTexts() {
			if s == "the session stopped: agent exited with status 3" {
				return true
			}
		}
		return false
	})
}
