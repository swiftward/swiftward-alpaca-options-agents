// Package harness holds the clock and the room. It decides WHEN a session runs
// and says why it woke it; it never decides what to trade - that is the session's
// job, and the autonomy requirement rests on the difference.
//
// Two causes wake a session: the schedule in the declaration, and a person
// writing in the chat. The session sees only a prompt naming its cause, so both
// look the same from inside it.
package harness

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/session"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// Chat is the room the session works in. The agent never holds this - the
// harness posts what the session said and passes on what a person wrote.
type Chat interface {
	Listen(ctx context.Context) error
	Inbound() <-chan telegram.Message
	Send(ctx context.Context, text string) (int, error)
	Edit(ctx context.Context, messageID int, text string) error
}

// Sessions starts one agent session and reports what it produced.
type Sessions interface {
	Run(ctx context.Context, req session.Request, on session.Handlers) (session.Result, error)
}

type Harness struct {
	Chat     Chat
	Sessions Sessions
	// DeclarationPath names the sessions the clock wakes. Empty means the clock
	// wakes nobody and the chat is the only cause.
	DeclarationPath string
	// Dir and Sandbox are what every session is given to work in.
	Dir     string
	Sandbox string
	Log     *zap.Logger

	// threadID carries the conversation forward: the next session continues the
	// last one rather than meeting the day from nothing.
	threadID string
}

// Run holds both causes until ctx ends. With neither a declaration nor a chat it
// refuses to start: a harness that runs while waking nobody looks exactly like a
// working one.
func (h *Harness) Run(ctx context.Context) error {
	if h.DeclarationPath == "" && h.Chat == nil {
		return fmt.Errorf("the harness has no cause to wake a session: set DECLARATION, configure the chat, or run neither role")
	}

	if h.DeclarationPath != "" {
		raw, err := os.ReadFile(h.DeclarationPath)
		if err != nil {
			return fmt.Errorf("read declaration %s: %w", h.DeclarationPath, err)
		}
		h.Log.Info("declaration read", zap.String("path", h.DeclarationPath), zap.Int("bytes", len(raw)))

		return fmt.Errorf("declaration format is not implemented yet: nothing can be scheduled from %s", h.DeclarationPath)
	}

	return h.serveChat(ctx)
}

func (h *Harness) serveChat(ctx context.Context) error {
	group := make(chan error, 1)
	go func() { group <- h.Chat.Listen(ctx) }()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-group:
			return err
		case msg, ok := <-h.Chat.Inbound():
			if !ok {
				return nil
			}
			h.runForPeople(ctx, append([]telegram.Message{msg}, h.drainWaiting()...))
		}
	}
}

// drainWaiting takes everything already waiting in the chat without blocking.
// Three lines typed in a row are one thing to say, and answering them as three
// sessions costs three turns and reads as an agent that is not listening.
func (h *Harness) drainWaiting() []telegram.Message {
	var waiting []telegram.Message
	for {
		select {
		case msg, ok := <-h.Chat.Inbound():
			if !ok {
				return waiting
			}
			waiting = append(waiting, msg)
		default:
			return waiting
		}
	}
}

// runForPeople runs one session for everything said so far and reports it as it
// goes. Anything written while it runs waits and is served next: two sessions on
// one account would close each other's positions.
func (h *Harness) runForPeople(ctx context.Context, msgs []telegram.Message) {
	status, err := h.Chat.Send(ctx, "working")
	if err != nil {
		h.Log.Error("could not open a status line in the chat", zap.Error(err))
	}

	on := session.Handlers{
		Text: func(text string) {
			if _, err := h.Chat.Send(ctx, text); err != nil {
				h.Log.Error("could not post what the session said", zap.Error(err))
			}
		},
		Tool: func(name string) {
			if status == 0 {
				return
			}
			if err := h.Chat.Edit(ctx, status, "working: "+name); err != nil {
				h.Log.Debug("could not update the status line", zap.Error(err))
			}
		},
	}

	result, runErr := h.Sessions.Run(ctx, session.Request{
		Prompt:   promptFor(msgs),
		ThreadID: h.threadID,
		Dir:      h.Dir,
		Sandbox:  h.Sandbox,
	}, on)

	if result.ThreadID != "" {
		h.threadID = result.ThreadID
	}
	h.Log.Info("session finished",
		zap.String("thread_id", result.ThreadID),
		zap.Int("messages_carried", len(msgs)))

	final := "done"
	if runErr != nil {
		h.Log.Error("session ended badly", zap.Error(runErr))
		final = "the session stopped: " + runErr.Error()
	}
	if status != 0 {
		if err := h.Chat.Edit(ctx, status, final); err != nil {
			h.Log.Debug("could not close the status line", zap.Error(err))
		}
	}
}

// promptFor names the cause. A session that cannot say why it ran cannot be
// judged on whether it should have.
func promptFor(msgs []telegram.Message) string {
	lines := make([]string, 0, len(msgs)+1)
	lines = append(lines, "Woken by a person in the chat.")
	for _, msg := range msgs {
		who := msg.Username
		if who == "" {
			who = fmt.Sprintf("user %d", msg.UserID)
		}
		lines = append(lines, fmt.Sprintf("From %s: %s", who, msg.Text))
	}

	return strings.Join(lines, "\n")
}
