package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// fakeAgent is a real process emitting the event stream the agent emits, so the
// parser is exercised across a pipe rather than over a string in memory.
func fakeAgent(t *testing.T, script string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700))

	return path
}

func TestRunReportsThreadAndText(t *testing.T) {
	agent := fakeAgent(t, `
cat > /dev/null
echo '{"type":"thread.started","thread_id":"01a03463"}'
echo '{"type":"turn.started"}'
echo '{"type":"item.completed","item":{"id":"i0","type":"command_execution","command":"ls"}}'
echo '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"premium is rich, opening the spread"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10}}'
`)

	var text, tools []string
	result, err := Runner{Command: agent, Log: zaptest.NewLogger(t)}.Run(
		context.Background(),
		Request{Prompt: "entry session: the window is open"},
		Handlers{
			Text: func(s string) { text = append(text, s) },
			Tool: func(s string) { tools = append(tools, s) },
		},
	)
	require.NoError(t, err)

	assert.Equal(t, "01a03463", result.ThreadID)
	assert.Equal(t, "premium is rich, opening the spread", result.LastMessage)
	assert.Equal(t, []string{"premium is rich, opening the spread"}, text)
	assert.Equal(t, []string{"ls"}, tools)
}

// The prompt carries the reason the session was woken, and it must reach the
// agent - a session that starts without it would trade for no stated cause.
func TestPromptReachesTheAgent(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "prompt.txt")
	agent := fakeAgent(t, "cat > "+seen+"\necho '{\"type\":\"thread.started\",\"thread_id\":\"t\"}'\n")

	_, err := Runner{Command: agent, Log: zaptest.NewLogger(t)}.Run(
		context.Background(),
		Request{Prompt: "defend session: price crossed the sold strike"},
		Handlers{},
	)
	require.NoError(t, err)

	got, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Equal(t, "defend session: price crossed the sold strike", string(got))
}

func TestResumeContinuesTheSameThread(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "args.txt")
	agent := fakeAgent(t, "echo \"$@\" > "+seen+"\ncat > /dev/null\n")

	_, err := Runner{Command: agent, Log: zaptest.NewLogger(t)}.Run(
		context.Background(),
		Request{Prompt: "and what about the second leg?", ThreadID: "01a03463", Sandbox: "read-only", Model: "gpt-5.6-sol-mini"},
		Handlers{},
	)
	require.NoError(t, err)

	got, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Contains(t, string(got), "exec resume 01a03463")
	assert.Contains(t, string(got), "--json")
	// The agent refuses -s on a resumed thread, so the bound travels as a
	// configuration override instead - and it must travel, or a continuing
	// session would be less bounded than the one that started it.
	assert.Contains(t, string(got), `-c sandbox_mode="read-only"`)
	assert.NotContains(t, string(got), "-s read-only")
	assert.Contains(t, string(got), "-m gpt-5.6-sol-mini")
}

func TestRunRefusesWithoutAPrompt(t *testing.T) {
	_, err := Runner{Command: "true", Log: zaptest.NewLogger(t)}.Run(context.Background(), Request{}, Handlers{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "needs a prompt")
}

// A session that dies is not a session that said nothing: whatever it managed to
// produce is still reported, and the failure is reported with it.
func TestFailedSessionStillReportsWhatItSaid(t *testing.T) {
	agent := fakeAgent(t, `
cat > /dev/null
echo '{"type":"thread.started","thread_id":"t9"}'
echo '{"type":"item.completed","item":{"type":"agent_message","text":"halfway through"}}'
exit 3
`)

	result, err := Runner{Command: agent, Log: zaptest.NewLogger(t)}.Run(
		context.Background(),
		Request{Prompt: "go"},
		Handlers{},
	)
	require.Error(t, err)
	assert.Equal(t, "t9", result.ThreadID)
	assert.Equal(t, "halfway through", result.LastMessage)
}
