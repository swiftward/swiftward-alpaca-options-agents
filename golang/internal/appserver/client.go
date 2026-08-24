// Package appserver speaks the agent's own protocol: one long-lived process, a
// line of JSON per request, a stream of notifications back.
//
// This is what makes a person able to interrupt work already in progress. A
// session started as a command takes its task once and cannot be told anything
// until it finishes; a session held by this protocol accepts a new message in the
// middle of the turn and acts on it at the next step.
package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"go.uber.org/zap"
)

// Kind names the events this program acts on. The protocol carries many more;
// modelling only these keeps the reader honest about what is used.
type Kind string

const (
	// KindText is a whole message the agent produced.
	KindText Kind = "text"
	// KindDelta is a fragment of a message still being written.
	KindDelta Kind = "delta"
	// KindTool is a tool the agent called.
	KindTool Kind = "tool"
	// KindTurnDone ends a turn, successfully or not.
	KindTurnDone Kind = "turn_done"
)

type Event struct {
	Kind   Kind
	Text   string
	Tool   string
	TurnID string
}

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	log    *zap.Logger
	events chan Event

	mu      sync.Mutex
	nextID  int
	waiting map[int]chan json.RawMessage

	closeOnce sync.Once
}

// Dial starts the agent's protocol server and completes the handshake. command
// is the agent binary; it is a parameter so a test drives a real process.
func Dial(ctx context.Context, command string, log *zap.Logger) (*Client, error) {
	cmd := exec.CommandContext(ctx, command, "app-server", "--listen", "stdio://")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open the agent's input: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open the agent's output: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start the agent: %w", err)
	}

	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		log:     log,
		events:  make(chan Event, eventBuffer),
		waiting: make(map[int]chan json.RawMessage),
	}
	go c.read(stdout)

	if _, err := c.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": clientName, "title": clientName, "version": clientVersion},
	}); err != nil {
		_ = c.Close()
		return nil, err
	}

	return c, nil
}

// Events carries what the agent says while it works. A slow reader loses events
// rather than stalling the agent, and the loss is logged: a chat that lags behind
// is better than a session that stops to wait for it.
func (c *Client) Events() <-chan Event { return c.events }

func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		err = c.cmd.Wait()
		close(c.events)
	})
	return err
}

// StartThread opens a conversation and returns its identifier.
func (c *Client) StartThread(ctx context.Context, opts ThreadOptions) (string, error) {
	raw, err := c.call(ctx, "thread/start", opts.params())
	if err != nil {
		return "", err
	}

	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("read the thread this started: %w", err)
	}
	if res.Thread.ID == "" {
		return "", fmt.Errorf("the agent started a thread without an identifier")
	}

	return res.Thread.ID, nil
}

// ResumeThread reopens an earlier conversation, so the next turn continues it.
func (c *Client) ResumeThread(ctx context.Context, threadID string, opts ThreadOptions) error {
	params := opts.params()
	params["threadId"] = threadID
	_, err := c.call(ctx, "thread/resume", params)

	return err
}

// StartTurn gives the conversation something to do and returns the turn's
// identifier, which is what steering and interrupting need.
func (c *Client) StartTurn(ctx context.Context, threadID, text string) (string, error) {
	raw, err := c.call(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]string{{"type": "text", "text": text}},
	})
	if err != nil {
		return "", err
	}

	var res struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("read the turn this started: %w", err)
	}

	return res.Turn.ID, nil
}

// Steer puts a message into a turn already running. The turn identifier is sent
// with it: if the turn has ended meanwhile, the agent refuses rather than
// steering the next one, which would land the message in a different piece of
// work than the person was watching.
func (c *Client) Steer(ctx context.Context, threadID, turnID, text string) error {
	_, err := c.call(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          []map[string]string{{"type": "text", "text": text}},
	})

	return err
}

// Interrupt stops the running turn.
func (c *Client) Interrupt(ctx context.Context, threadID, turnID string) error {
	_, err := c.call(ctx, "turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})

	return err
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	reply := make(chan json.RawMessage, 1)
	c.waiting[id] = reply
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.waiting, id)
		c.mu.Unlock()
	}()

	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", method, err)
	}
	if _, err := c.stdin.Write(append(request, '\n')); err != nil {
		return nil, fmt.Errorf("send %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case raw, ok := <-reply:
		if !ok {
			return nil, fmt.Errorf("the agent stopped before answering %s", method)
		}
		return raw, nil
	}
}

func (c *Client) read(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			c.log.Debug("unparsed line from the agent", zap.ByteString("line", line))
			continue
		}

		var msg struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			c.log.Debug("unreadable line from the agent", zap.Error(err))
			continue
		}

		if msg.ID != nil && msg.Method == "" {
			c.deliver(*msg.ID, msg.Result, msg.Error)
			continue
		}
		if ev, ok := eventFrom(msg.Method, msg.Params); ok {
			select {
			case c.events <- ev:
			default:
				c.log.Warn("event dropped: the reader is behind", zap.String("kind", string(ev.Kind)))
			}
		}
	}

	c.mu.Lock()
	for id, reply := range c.waiting {
		close(reply)
		delete(c.waiting, id)
	}
	c.mu.Unlock()
}

func (c *Client) deliver(id int, result json.RawMessage, failure *struct {
	Message string `json:"message"`
}) {
	c.mu.Lock()
	reply, ok := c.waiting[id]
	c.mu.Unlock()
	if !ok {
		return
	}

	if failure != nil {
		c.log.Error("the agent refused a request", zap.Int("id", id), zap.String("reason", failure.Message))
		close(reply)
		return
	}
	reply <- result
}

func eventFrom(method string, params json.RawMessage) (Event, bool) {
	switch method {
	case "item/agentMessage/delta":
		var p struct {
			TurnID string `json:"turnId"`
			Delta  string `json:"delta"`
		}
		if json.Unmarshal(params, &p) != nil {
			return Event{}, false
		}
		return Event{Kind: KindDelta, Text: p.Delta, TurnID: p.TurnID}, true

	case "item/completed":
		var p struct {
			TurnID string `json:"turnId"`
			Item   struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Command string `json:"command"`
				Name    string `json:"name"`
			} `json:"item"`
		}
		if json.Unmarshal(params, &p) != nil {
			return Event{}, false
		}
		switch p.Item.Type {
		case "agentMessage":
			return Event{Kind: KindText, Text: p.Item.Text, TurnID: p.TurnID}, true
		case "commandExecution", "mcpToolCall", "toolCall":
			name := p.Item.Name
			if name == "" {
				name = p.Item.Command
			}
			return Event{Kind: KindTool, Tool: name, TurnID: p.TurnID}, true
		}
		return Event{}, false

	case "turn/completed", "turn/failed", "turn/aborted":
		var p struct {
			ThreadID string `json:"threadId"`
			Turn     struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		_ = json.Unmarshal(params, &p)
		return Event{Kind: KindTurnDone, TurnID: p.Turn.ID}, true
	}

	return Event{}, false
}

const (
	clientName    = "swiftward-alpaca-options-agents"
	clientVersion = "v0.1.0"
	eventBuffer   = 256
	maxLineBytes  = 1 << 20
)
