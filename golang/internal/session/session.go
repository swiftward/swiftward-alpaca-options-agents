// Package session runs one agent session as a child process and turns its event
// stream into things the rest of this program can act on: the text it produced,
// the tools it called, and the identifier that lets the next session continue
// the same thread.
//
// The session decides what to trade. Nothing here does - this package starts it,
// reads it and reports it.
package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"go.uber.org/zap"
)

// Request is one run of the agent.
type Request struct {
	// Prompt is the task and the reason this session was woken.
	Prompt string
	// ThreadID continues an earlier session instead of starting a new one. Empty
	// starts a new thread.
	ThreadID string
	// Dir is the working directory the session is given.
	Dir string
	// Sandbox is the agent's own sandbox mode; the caller names it so a reading
	// session and a working session are not the same by accident.
	Sandbox string
}

// Result is what a finished session leaves behind.
type Result struct {
	ThreadID    string
	LastMessage string
}

// Handlers observe a running session. Each is optional; a nil one is not called.
type Handlers struct {
	// Text receives every message the agent produced, in the order it produced it.
	Text func(string)
	// Tool receives the name of each tool call, so the people watching see what it
	// is doing between messages.
	Tool func(name string)
}

// Runner starts sessions with one command. Command is the agent binary; it is a
// field rather than a constant so a test drives a real process without needing
// the real agent installed.
type Runner struct {
	Command string
	Log     *zap.Logger
}

func (r Runner) Run(ctx context.Context, req Request, on Handlers) (Result, error) {
	if req.Prompt == "" {
		return Result{}, fmt.Errorf("a session needs a prompt: it carries the reason it was woken")
	}

	cmd := exec.CommandContext(ctx, r.Command, r.args(req)...)
	cmd.Dir = req.Dir
	cmd.Stdin = strings.NewReader(req.Prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, fmt.Errorf("open the session's output: %w", err)
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start the session: %w", err)
	}

	result := r.read(stdout, on)

	if err := cmd.Wait(); err != nil {
		return result, fmt.Errorf("session ended badly: %w", err)
	}

	return result, nil
}

func (r Runner) args(req Request) []string {
	args := []string{"exec"}
	if req.ThreadID != "" {
		args = append(args, "resume", req.ThreadID)
	}
	args = append(args, "--json", "--skip-git-repo-check")
	if req.Sandbox != "" {
		args = append(args, "-s", req.Sandbox)
	}
	// The prompt arrives on stdin: a session's reason can be long, and an argument
	// list is the wrong place for text a person wrote.
	return append(args, "-")
}

// event is the part of the agent's stream this program acts on. Everything else
// in the stream is left alone rather than half-modelled.
type event struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Command string `json:"command"`
		Name    string `json:"name"`
	} `json:"item"`
}

func (r Runner) read(out io.Reader, on Handlers) Result {
	var result Result

	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 0, 64*1024), maxEventBytes)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var ev event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			// A line that is not an event is the agent's own console noise; it is
			// worth seeing in the log and worth nothing to the logic.
			r.Log.Debug("unparsed line from the session", zap.String("line", line))
			continue
		}

		switch ev.Type {
		case "thread.started":
			result.ThreadID = ev.ThreadID
		case "item.completed":
			switch ev.Item.Type {
			case "agent_message":
				result.LastMessage = ev.Item.Text
				if on.Text != nil && ev.Item.Text != "" {
					on.Text(ev.Item.Text)
				}
			case "command_execution", "tool_call", "mcp_tool_call":
				if on.Tool != nil {
					on.Tool(toolName(ev))
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		r.Log.Error("reading the session's output stopped early", zap.Error(err))
	}

	return result
}

func toolName(ev event) string {
	if ev.Item.Name != "" {
		return ev.Item.Name
	}
	if ev.Item.Command != "" {
		return ev.Item.Command
	}
	return ev.Item.Type
}

// One event line is bounded: a runaway line would otherwise grow until the
// process dies, and the agent's own output is the one input we do not control.
const maxEventBytes = 1 << 20
