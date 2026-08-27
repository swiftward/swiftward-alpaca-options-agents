package agent

import (
	"context"
	"errors"
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
	// resumeLimit bounds the ONE request that resumes yesterday's conversation.
	// It is deliberately far shorter than callTimeout: continuity is worth little
	// and a fresh thread costs nothing, while the harness wakes nobody until this
	// returns. On 25 August a resume and a start, each bounded by the full call
	// timeout, held the agent silent for six minutes of open market and swallowed
	// a whole session's window.
	resumeLimit time.Duration

	threadID string
}

func NewConversation(client *Client, options ThreadOptions, rememberIn string, callTimeout, resumeLimit time.Duration) *Conversation {
	return &Conversation{
		client: client, options: options, rememberIn: rememberIn,
		callTimeout: callTimeout, resumeLimit: resumeLimit,
	}
}

// Open returns the thread this conversation runs in, opening or resuming it once.
// A restart resumes what was remembered, so a person who wrote yesterday is not
// met by an agent that has never heard of them.
func (c *Conversation) Open(ctx context.Context) (string, error) {
	if c.threadID != "" {
		return c.threadID, nil
	}

	// A remembered thread we have no bound for is the worst of both: the harness
	// wakes nobody until the resume answers, and nothing says when to give up.
	// Refusing here turns a silent unbounded wait into a start that fails loudly -
	// which is what a knob declared in one place and never passed to the process
	// looks like from the inside.
	if c.rememberIn != "" && c.resumeLimit <= 0 {
		return "", errors.New("a conversation may be remembered only with a bound on resuming it: set THREAD_RESUME_LIMIT")
	}

	if remembered := c.remembered(); remembered != "" {
		resumeCtx, done := c.boundedBy(ctx, c.resumeLimit)
		err := c.client.ResumeThread(resumeCtx, remembered, c.options)
		done()
		if err == nil {
			c.threadID = remembered
			return remembered, nil
		}
		// The remembered thread is gone, stuck, or refuses to resume. Forget it
		// before starting a fresh one: keeping the note would make every later
		// start pay the same wait for the same thread that never comes back.
		c.forget()
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
	return c.boundedBy(ctx, c.callTimeout)
}

func (c *Conversation) boundedBy(ctx context.Context, limit time.Duration) (context.Context, context.CancelFunc) {
	if limit <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(context.WithoutCancel(ctx), limit)
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

// Forget drops the remembered thread AND the one held in memory, so the next
// Open starts a fresh conversation rather than resuming this one.
//
// Both halves matter. The file is what survives a restart of this process; the
// field is what this process would otherwise keep using without ever consulting
// the file again. Forgetting one and not the other looks like it worked and
// changes nothing.
func (c *Conversation) Forget() {
	c.threadID = ""
	c.forget()
}

func (c *Conversation) forget() {
	if c.rememberIn == "" {
		return
	}
	_ = os.Remove(c.rememberIn)
}

// ThreadID reports the thread in use, empty before the first turn.
func (c *Conversation) ThreadID() string { return c.threadID }

func (c *Conversation) Turn(ctx context.Context, text, model string) (string, error) {
	threadID, err := c.Open(ctx)
	if err != nil {
		return "", err
	}

	return c.client.StartTurn(ctx, threadID, text, model)
}

func (c *Conversation) Steer(ctx context.Context, turnID, text string) error {
	return c.client.Steer(ctx, c.threadID, turnID, text)
}

func (c *Conversation) Interrupt(ctx context.Context, turnID string) error {
	return c.client.Interrupt(ctx, c.threadID, turnID)
}

func (c *Conversation) Events() <-chan Event { return c.client.Events() }
