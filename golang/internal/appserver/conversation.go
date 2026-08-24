package appserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Conversation is one thread with the agent, opened once and continued across
// turns. It exists so a caller states what it wants - open, take a turn, steer,
// interrupt - without carrying the thread identifier through every call.
type Conversation struct {
	client  *Client
	options ThreadOptions
	// rememberIn is where the thread identifier is kept between runs. Empty means
	// a restart starts a new conversation, which is the right behavior only when
	// nobody expects yesterday to be remembered.
	rememberIn string
	// callTimeout bounds ONE protocol request. Resuming a long conversation and
	// starting a fresh one are separate requests and get separate bounds: a slow
	// resume that ate the whole budget would leave nothing for the fallback, and
	// the caller would be told the agent is unreachable when it is merely busy.
	callTimeout time.Duration

	threadID string
}

func NewConversation(client *Client, options ThreadOptions, rememberIn string, callTimeout time.Duration) *Conversation {
	return &Conversation{client: client, options: options, rememberIn: rememberIn, callTimeout: callTimeout}
}

// Open returns the thread this conversation runs in, opening or resuming it once.
// A restart resumes what was remembered, so a person who wrote yesterday is not
// met by an agent that has never heard of them.
func (c *Conversation) Open(ctx context.Context) (string, error) {
	if c.threadID != "" {
		return c.threadID, nil
	}

	if remembered := c.remembered(); remembered != "" {
		resumeCtx, done := c.bounded(ctx)
		err := c.client.ResumeThread(resumeCtx, remembered, c.options)
		done()
		if err == nil {
			c.threadID = remembered
			return remembered, nil
		}
		// The remembered thread is gone or unusable. Starting a fresh one is
		// better than refusing to work, and the next write replaces the note.
	}

	startCtx, done := c.bounded(ctx)
	defer done()

	threadID, err := c.client.StartThread(startCtx, c.options)
	if err != nil {
		return "", err
	}
	c.threadID = threadID
	c.remember(threadID)

	return threadID, nil
}

// bounded gives one request its own deadline, taken from the parent so a shutdown
// still cancels everything.
func (c *Conversation) bounded(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.callTimeout <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(context.WithoutCancel(ctx), c.callTimeout)
}

func (c *Conversation) remembered() string {
	if c.rememberIn == "" {
		return ""
	}
	raw, err := os.ReadFile(c.rememberIn)
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(raw))
}

func (c *Conversation) remember(threadID string) {
	if c.rememberIn == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.rememberIn), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(c.rememberIn, []byte(threadID+"\n"), 0o600)
}

// ThreadID reports the thread in use, empty before the first turn.
func (c *Conversation) ThreadID() string { return c.threadID }

func (c *Conversation) Turn(ctx context.Context, text string) (string, error) {
	threadID, err := c.Open(ctx)
	if err != nil {
		return "", err
	}

	return c.client.StartTurn(ctx, threadID, text)
}

func (c *Conversation) Steer(ctx context.Context, turnID, text string) error {
	return c.client.Steer(ctx, c.threadID, turnID, text)
}

func (c *Conversation) Interrupt(ctx context.Context, turnID string) error {
	return c.client.Interrupt(ctx, c.threadID, turnID)
}

func (c *Conversation) Events() <-chan Event { return c.client.Events() }
