package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// fakeAgent is a real process speaking the protocol over its own pipes: request
// lines in, response and notification lines out. The client under test is the
// production one, and nothing about the framing is stubbed.
func fakeAgent(t *testing.T, script string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "agent")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700))

	return path
}

const answerHandshake = `
read line   # initialize
echo '{"jsonrpc":"2.0","id":1,"result":{"userAgent":"fake"}}'
`

func TestDialCompletesTheHandshake(t *testing.T) {
	agent := fakeAgent(t, answerHandshake+"\nread line\n")

	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
}

func TestStartThreadAndTurn(t *testing.T) {
	agent := fakeAgent(t, answerHandshake+`
read line   # thread/start
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"th-1"}}}'
read line   # turn/start
echo '{"jsonrpc":"2.0","id":3,"result":{"turn":{"id":"tu-1"}}}'
echo '{"method":"item/completed","params":{"turnId":"tu-1","item":{"type":"commandExecution","command":"ls"}}}'
echo '{"method":"item/agentMessage/delta","params":{"turnId":"tu-1","delta":"pre"}}'
echo '{"method":"item/completed","params":{"turnId":"tu-1","item":{"type":"agentMessage","text":"premium is rich"}}}'
echo '{"method":"turn/completed","params":{"threadId":"th-1","turn":{"id":"tu-1"}}}'
read line
`)

	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	threadID, err := client.StartThread(ctx, ThreadOptions{Model: "m", Sandbox: "read-only", Dir: "/work"})
	require.NoError(t, err)
	assert.Equal(t, "th-1", threadID)

	turnID, err := client.StartTurn(ctx, threadID, "entry session: the window is open", "")
	require.NoError(t, err)
	assert.Equal(t, "tu-1", turnID)

	var kinds []Kind
	var text, tool string
	for ev := range client.Events() {
		kinds = append(kinds, ev.Kind)
		switch ev.Kind {
		case KindText:
			text = ev.Text
		case KindTool:
			tool = ev.Tool
		}
		if ev.Kind == KindTurnDone {
			break
		}
	}

	assert.Equal(t, []Kind{KindTool, KindDelta, KindText, KindTurnDone}, kinds)
	assert.Equal(t, "premium is rich", text)
	assert.Equal(t, "ls", tool)
}

// Steering carries the turn it means. Sending it without one would let a message
// meant for work in progress land in whatever ran next.
func TestSteerAndInterruptCarryTheTurn(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "requests.txt")
	agent := fakeAgent(t, answerHandshake+`
while read line; do
  echo "$line" >> `+seen+`
  id=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{}}"
done
`)

	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.NoError(t, client.Steer(ctx, "th-1", "tu-1", "close it now"))
	require.NoError(t, client.Interrupt(ctx, "th-1", "tu-1"))

	raw, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"method":"turn/steer"`)
	assert.Contains(t, string(raw), `"expectedTurnId":"tu-1"`)
	assert.Contains(t, string(raw), `"method":"turn/interrupt"`)
}

// A refusal must reach the caller as an error. Treating it as success would let
// the harness report to the chat that a message was delivered when it was not.
func TestARefusedRequestIsAnError(t *testing.T) {
	agent := fakeAgent(t, answerHandshake+`
read line
echo '{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"turn already finished"}}'
read line
`)

	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	err = client.Steer(context.Background(), "th-1", "tu-1", "too late")
	require.Error(t, err)
}

// Nobody sits at this keyboard. A policy that asks would stall the turn forever,
// so the request that opens a thread must carry one that does not.
func TestThreadOptionsRefuseToAsk(t *testing.T) {
	params := ThreadOptions{Model: "m", Sandbox: "workspace-write", Dir: "/work"}.params()
	assert.Equal(t, ApprovalNever, params["approvalPolicy"])

	chosen := ThreadOptions{ApprovalPolicy: "on-request"}.params()
	assert.Equal(t, "on-request", chosen["approvalPolicy"], "an operator who names a policy gets it")
}

// A restart must not meet a person who wrote yesterday as a stranger, so the
// thread identifier outlives the process.
func TestConversationRemembersItsThread(t *testing.T) {
	agent := fakeAgent(t, answerHandshake+`
read line   # thread/start
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"th-remembered"}}}'
read line
`)
	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	file := filepath.Join(t.TempDir(), "state", ".thread")
	first := NewConversation(client, ThreadOptions{}, file, 5*time.Second)

	threadID, err := first.Open(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "th-remembered", threadID)

	written, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, "th-remembered", strings.TrimSpace(string(written)))
}

func TestConversationResumesWhatWasRemembered(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "requests.txt")
	agent := fakeAgent(t, answerHandshake+`
while read line; do
  echo "$line" >> `+seen+`
  id=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{}}"
done
`)
	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	file := filepath.Join(t.TempDir(), ".thread")
	require.NoError(t, os.WriteFile(file, []byte("th-from-yesterday\n"), 0o600))

	threadID, err := NewConversation(client, ThreadOptions{}, file, 5*time.Second).Open(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "th-from-yesterday", threadID)

	raw, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"method":"thread/resume"`)
	assert.NotContains(t, string(raw), `"method":"thread/start"`)
}

// A server that starts and never answers must fail, not hang: a hang leaves
// nothing in the log and looks like the whole program is thinking.
func TestDialGivesUpOnASilentServer(t *testing.T) {
	agent := fakeAgent(t, "sleep 30\n")

	start := time.Now()
	_, err := Dial(context.Background(), agent, 300*time.Millisecond, zaptest.NewLogger(t))
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second)
}

// Opening must not wait forever on a thread the agent will not resume: the whole
// program waits on this call before the room is served.
func TestOpenGivesUpOnASilentResume(t *testing.T) {
	agent := fakeAgent(t, answerHandshake+`
read line   # thread/resume - never answered
sleep 30
`)
	client, err := Dial(context.Background(), agent, 2*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	file := filepath.Join(t.TempDir(), ".thread")
	require.NoError(t, os.WriteFile(file, []byte("th-stuck\n"), 0o600))

	start := time.Now()
	_, err = NewConversation(client, ThreadOptions{}, file, 300*time.Millisecond).Open(context.Background())
	require.Error(t, err)
	assert.Less(t, time.Since(start), 5*time.Second, "the bound on one request did not fire")

	_, statErr := os.Stat(file)
	assert.True(t, os.IsNotExist(statErr), "a thread that will not resume must not be remembered into the next start")
}

// A cheap session must actually run cheap: the model it names has to reach the
// agent, or the saving is imaginary.
func TestATurnCanNameItsModel(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "requests.txt")
	agent := fakeAgent(t, answerHandshake+`
while read line; do
  echo "$line" >> `+seen+`
  id=$(echo "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  echo "{\"jsonrpc\":\"2.0\",\"id\":$id,\"result\":{\"turn\":{\"id\":\"tu-1\"}}}"
done
`)
	client, err := Dial(context.Background(), agent, 5*time.Second, zaptest.NewLogger(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	_, err = client.StartTurn(context.Background(), "th-1", "read the news", "gpt-5.6-luna")
	require.NoError(t, err)

	raw, err := os.ReadFile(seen)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"model":"gpt-5.6-luna"`)
}
