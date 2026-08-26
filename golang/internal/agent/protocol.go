// Package appserver speaks the agent's own protocol: one long-lived process, a
// line of JSON per request, a stream of notifications back.
//
// This is what makes a person able to interrupt work already in progress. A
// session started as a command takes its task once and cannot be told anything
// until it finishes; a session held by this protocol accepts a new message in the
// middle of the turn and acts on it at the next step.
package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

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
	// KindToolStarted is a tool the agent has begun calling.
	KindToolStarted Kind = "tool_started"
	// KindTool is a tool call that finished, successfully or not.
	KindTool Kind = "tool"
	// KindTurnDone ends a turn, successfully or not.
	KindTurnDone Kind = "turn_done"
)

type Event struct {
	Kind   Kind
	Text   string
	TurnID string
	// Call is set on the two tool events: which call, on which server, with what
	// arguments, and how it ended.
	Call Call
}

// Call is one tool call as the agent reports it. Arguments are carried verbatim
// so the record can say what was asked, not only what was asked for.
type Call struct {
	Ref       string
	Server    string
	Tool      string
	Arguments json.RawMessage
	Status    string
	Failure   string
	// Answer is the beginning of what the tool said back. A refusal lives here.
	Answer string
}

// Named is what the chat shows for a call: the tool, and the server when it is
// not obvious from the tool alone.
func (c Call) Named() string {
	if c.Server == "" || c.Server == "shell" {
		return c.Tool
	}

	return c.Server + "." + c.Tool
}

type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	log    *zap.Logger
	events chan Event

	mu      sync.Mutex
	nextID  int
	waiting map[int]chan answer

	closeOnce sync.Once
}

// Dial starts the agent's protocol server and completes the handshake. command
// is the agent binary; it is a parameter so a test drives a real process.
//
// handshakeTimeout bounds the first request. Without it a server that starts but
// never answers leaves the whole program waiting with nothing in the log - the
// failure looks like a hang, not like a fault.
func Dial(ctx context.Context, command string, handshakeTimeout time.Duration, log *zap.Logger) (*Client, error) {
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
		waiting: make(map[int]chan answer),
	}
	go c.read(stdout)

	handshakeCtx := ctx
	if handshakeTimeout > 0 {
		var done context.CancelFunc
		handshakeCtx, done = context.WithTimeout(ctx, handshakeTimeout)
		defer done()
	}

	if _, err := c.call(handshakeCtx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": clientName, "title": clientName, "version": clientVersion},
	}); err != nil {
		// A server that did not answer the handshake will not answer a polite
		// shutdown either, so it is killed rather than waited for.
		c.kill()
		return nil, fmt.Errorf("the agent did not answer the handshake: %w", err)
	}

	return c, nil
}

// Events carries what the agent says while it works. A slow reader loses events
// rather than stalling the agent, and the loss is logged: a chat that lags behind
// is better than a session that stops to wait for it.
func (c *Client) Events() <-chan Event { return c.events }

// kill ends the process without waiting for it to agree.
func (c *Client) kill() {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		_ = c.cmd.Wait()
		close(c.events)
	})
}

// leaveTime is how long a polite shutdown is given before the agent is killed.
// A variable rather than a constant so the test can prove the escalation without
// standing there for ten seconds.
//
// The wait that used to be unbounded held the whole process on 26 August: a
// server that ignored the closed input never exited, main never returned, and
// because the process never exited Docker's restart policy never fired. The
// container read as Up for five minutes of a trading day while the harness had
// already stopped.
var leaveTime = 10 * time.Second

// Close asks the agent to leave and, if it will not, ends it.
//
// The bound is the whole point: a shutdown that can hang forever is worse than
// a kill, because a process stuck on the way out looks alive to everything above
// it - to the supervisor that would have restarted it and to the operator
// reading the container list.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()

		left := make(chan error, 1)
		go func() { left <- c.cmd.Wait() }()

		select {
		case err = <-left:
		case <-time.After(leaveTime):
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Kill()
			}
			err = <-left
			c.log.Warn("the agent did not leave when its input closed, so it was ended",
				zap.Duration("waited", leaveTime))
		}
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
// identifier, which is what steering and interrupting need. An empty model
// leaves the one the thread was opened with; naming one lets a cheap session
// cost what it is worth.
func (c *Client) StartTurn(ctx context.Context, threadID, text, model string) (string, error) {
	params := map[string]any{
		"threadId": threadID,
		"input":    []map[string]string{{"type": "text", "text": text}},
	}
	if model != "" {
		params["model"] = model
	}

	raw, err := c.call(ctx, "turn/start", params)
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

// answer is one reply to one request. A refusal and a lost connection are two
// different facts - the first says what is wrong, the second says nothing at all -
// and a caller that cannot tell them apart treats "there is no turn to interrupt"
// as "the agent may still be working".
type answer struct {
	result  json.RawMessage
	refusal error
}

func (c *Client) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	reply := make(chan answer, 1)
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
	// The write is bounded by the same deadline as the wait, because it can block
	// for exactly as long. A pipe accepts bytes only while the other end reads
	// them, so a server that has stopped reading turns this line into a wait with
	// no end - and it sits BEFORE the select below, where every declared timeout
	// lives. That is what happened on 26 August: the harness stopped here on its
	// way to opening a conversation, never reached the deadline it had been given,
	// never returned an error, and so never exited. The scheduler starts after the
	// conversation opens, so nothing ran for five hours and nothing said why.
	//
	// The goroutine may stay blocked on a pipe nobody drains; it holds nothing but
	// itself and dies with the process, which by then is on its way out.
	sent := make(chan error, 1)
	go func() {
		_, err := c.stdin.Write(append(request, '\n'))
		sent <- err
	}()

	select {
	case err := <-sent:
		if err != nil {
			return nil, fmt.Errorf("send %s: %w", method, err)
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("send %s: %w", method, ctx.Err())
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case given, ok := <-reply:
		if !ok {
			return nil, fmt.Errorf("the agent stopped before answering %s", method)
		}
		if given.refusal != nil {
			return nil, fmt.Errorf("%s: %w", method, given.refusal)
		}
		return given.result, nil
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
		if msg.Method != "" {
			c.log.Debug("event from the agent",
				zap.String("method", msg.Method), zap.ByteString("params", msg.Params))
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
		reply <- answer{refusal: refusalFrom(failure.Message)}
		return
	}
	reply <- answer{result: result}
}

// ErrNoActiveTurn is the agent saying the turn is already over. It is an answer,
// not a fault: the harness asked to interrupt a turn that had just finished.
var ErrNoActiveTurn = errors.New("no active turn")

// refusalFrom names the refusals the harness has to act on differently and passes
// the rest through as themselves.
func refusalFrom(reason string) error {
	if strings.Contains(strings.ToLower(reason), "no active turn") {
		return fmt.Errorf("%w: %s", ErrNoActiveTurn, reason)
	}

	return errors.New(reason)
}

// itemEvent is the shape both item events arrive in. Measured against the agent
// on 25 August 2026: a tool call carries its server, its name, its arguments and
// its status, and a shell command carries the command line instead of a name.
type itemEvent struct {
	TurnID string `json:"turnId"`
	Item   struct {
		ID        string          `json:"id"`
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		Command   string          `json:"command"`
		Name      string          `json:"name"`
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Status    string          `json:"status"`
		Result    *struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	} `json:"item"`
}

func (e itemEvent) call() Call {
	call := Call{
		Ref:       e.Item.ID,
		Server:    e.Item.Server,
		Tool:      e.Item.Tool,
		Arguments: e.Item.Arguments,
		Status:    e.Item.Status,
	}
	if call.Tool == "" {
		// A shell command has no tool name; the command line is what it did.
		call.Server = "shell"
		call.Tool = e.Item.Command
		if call.Tool == "" {
			call.Tool = e.Item.Name
		}
	}
	if e.Item.Error != nil {
		call.Failure = e.Item.Error.Message
	}
	call.Answer = answerOf(e)

	return call
}

// answerOf keeps the beginning of what a tool answered. A broker refusal travels
// inside the answer, not as a protocol error - measured on 25 August 2026, when a
// rejected order was recorded as a call that completed and the record could not
// tell it from a filled one. The whole answer is not kept: an option chain runs
// to tens of thousands of characters and says nothing a reader needs.
func answerOf(e itemEvent) string {
	if e.Item.Result == nil {
		return ""
	}

	var said strings.Builder
	for _, part := range e.Item.Result.Content {
		if part.Text == "" {
			continue
		}
		if said.Len() > 0 {
			said.WriteString(" ")
		}
		said.WriteString(part.Text)
		if said.Len() >= answerKept {
			break
		}
	}

	answer := said.String()
	if len(answer) > answerKept {
		answer = answer[:answerKept]
	}
	if e.Item.Result.IsError && answer != "" {
		return "refused: " + answer
	}

	return answer
}

// answerKept is how much of a tool's answer is written down: enough for a
// refusal to name its reason, short enough that a chain does not fill the record.
const answerKept = 400

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

	case "item/started", "item/completed":
		var p itemEvent
		if json.Unmarshal(params, &p) != nil {
			return Event{}, false
		}
		switch p.Item.Type {
		case "agentMessage":
			if method == "item/started" {
				return Event{}, false
			}
			return Event{Kind: KindText, Text: p.Item.Text, TurnID: p.TurnID}, true
		case "commandExecution", "mcpToolCall", "toolCall":
			kind := KindTool
			if method == "item/started" {
				kind = KindToolStarted
			}
			return Event{Kind: kind, TurnID: p.TurnID, Call: p.call()}, true
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
