package mailbox_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/mailbox"
)

// The scripts in poller/ are the client side of this protocol, and they are the
// part an operator actually holds. Testing the server alone would leave the two
// free to drift, and the drift would only show on the day a session was waiting
// for a turn nobody could hand it. So they are run here, against the real
// handler, exactly as a harness would run them.

func script(t *testing.T, name string) string {
	t.Helper()

	// From golang/internal/mailbox up to the repository root.
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "poller", name))
	require.NoError(t, err)

	if _, err := os.Stat(path); err != nil {
		t.Skipf("no %s to test: %v", name, err)
	}
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl on this machine")
	}

	return path
}

func run(t *testing.T, timeout time.Duration, name string, args ...string) (string, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	if err == nil {
		return string(out), 0
	}

	var exit *exec.ExitError
	if ok := asExitError(err, &exit); ok {
		return string(out), exit.ExitCode()
	}
	t.Fatalf("could not run %s: %v", name, err)

	return "", -1
}

func asExitError(err error, into **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*into = exit
	}

	return ok
}

// A parked turn reaches the script, whole, and the script exits 0 to say so.
// Exit zero IS the wake-up for a harness that watches a command rather than a
// stream; the object on stdout is what it wakes with.
func TestThePollerHandsOverATurnAndExitsZero(t *testing.T) {
	poll := script(t, "poll-once.sh")

	m := mailbox.New(token, 5*time.Second, time.Minute, nil)
	server := serve(t, m)

	turnID, err := m.Turn(context.Background(), "the защита window is open", "sonnet")
	require.NoError(t, err)

	out, code := run(t, 20*time.Second, poll, server.URL+"/mailbox/"+token, "3")
	require.Equal(t, 0, code, "stdout was: %s", out)

	var got mailbox.Delivery
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &got),
		"what the script printed was not one JSON object: %q", out)
	assert.Equal(t, mailbox.KindTurn, got.Kind)
	assert.Equal(t, turnID, got.Turn)
	assert.Equal(t, "the защита window is open", got.Text)
	assert.Equal(t, "sonnet", got.Model)
}

// A quiet market is exit 3 and nothing on stdout. It has to be distinguishable
// from a turn with no text, and from a failure to reach the mailbox at all -
// those three want three different things from the caller.
func TestAQuietHoldIsExitThreeAndSaysNothing(t *testing.T) {
	poll := script(t, "poll-once.sh")

	m := mailbox.New(token, 500*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	out, code := run(t, 20*time.Second, poll, server.URL+"/mailbox/"+token, "1")
	assert.Equal(t, 3, code)
	assert.Empty(t, strings.TrimSpace(out))
}

// A wrong token is exit 4, and it must not be retried: no amount of waiting
// turns a wrong token into a right one.
func TestAWrongTokenIsExitFour(t *testing.T) {
	poll := script(t, "poll-once.sh")

	m := mailbox.New(token, 500*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	_, code := run(t, 20*time.Second, poll, server.URL+"/mailbox/not-the-token", "1")
	assert.Equal(t, 4, code)
}

// An address nobody is listening on is exit 69, which is the one worth retrying.
func TestAnUnreachableMailboxIsExitSixtyNine(t *testing.T) {
	poll := script(t, "poll-once.sh")

	// Port 1 on the loopback: refused at once rather than waited out.
	_, code := run(t, 20*time.Second, poll, "http://127.0.0.1:1/mailbox/"+token, "1")
	assert.Equal(t, 69, code)
}

// What the reply script posts is what the harness hears. This is the half that
// puts the agent's words into the room and the record, and it is the half a
// client written by hand gets wrong first.
func TestTheReplyScriptSpeaksAndEndsTheTurn(t *testing.T) {
	reply := script(t, "reply.sh")

	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)
	base := server.URL + "/mailbox/" + token

	turnID, err := m.Turn(context.Background(), "look at the account", "")
	require.NoError(t, err)

	_, code := run(t, 20*time.Second, reply, base, "say", turnID, "закрыл спред, кредит 0,28")
	require.Equal(t, 0, code)

	_, code = run(t, 20*time.Second, reply, base, "done", turnID)
	require.Equal(t, 0, code)

	said := next(t, m)
	assert.Equal(t, agent.KindText, said.Kind)
	assert.Equal(t, turnID, said.TurnID)
	assert.Equal(t, "закрыл спред, кредит 0,28", said.Text)

	assert.Equal(t, agent.KindTurnDone, next(t, m).Kind)
}

// A turn that ended badly says so in the room rather than ending in silence: a
// session that failed and a session that decided to do nothing look identical
// from outside, and they need different answers from a person.
func TestAFailedTurnIsSaidOutLoud(t *testing.T) {
	reply := script(t, "reply.sh")

	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)
	base := server.URL + "/mailbox/" + token

	turnID, err := m.Turn(context.Background(), "enter", "")
	require.NoError(t, err)

	_, code := run(t, 20*time.Second, reply, base, "done", turnID, "the broker refused every leg")
	require.Equal(t, 0, code)

	said := next(t, m)
	require.Equal(t, agent.KindText, said.Kind)
	assert.Contains(t, said.Text, "the broker refused every leg")
	assert.Equal(t, agent.KindTurnDone, next(t, m).Kind)
}

// The streaming shape prints one line per event and does not exit. A harness
// that watches a command's output reads it this way, and the two shapes must
// never be swapped: this one exiting would look like a wake-up, and the
// wake-on-exit one would say its single line to a reader that expects many.
func TestTheStreamingPollerPrintsALinePerEventAndKeepsRunning(t *testing.T) {
	stream := script(t, "poll-stream.sh")

	m := mailbox.New(token, 500*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, stream, server.URL+"/mailbox/"+token, "1")
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	lines := make(chan string, 4)
	go func() {
		decoder := json.NewDecoder(out)
		for {
			var d mailbox.Delivery
			if err := decoder.Decode(&d); err != nil {
				return
			}
			lines <- d.Turn
		}
	}()

	// Two turns, with a quiet hold between them: the second proves the stream
	// survived an empty answer instead of treating it as the end.
	first, err := m.Turn(context.Background(), "first", "")
	require.NoError(t, err)
	assert.Equal(t, first, waitLine(t, lines))

	time.Sleep(time.Second)

	second, err := m.Turn(context.Background(), "second", "")
	require.NoError(t, err)
	assert.Equal(t, second, waitLine(t, lines))

	assert.Nil(t, cmd.ProcessState, "the stream exited; it is meant to keep running")
}

func waitLine(t *testing.T, lines <-chan string) string {
	t.Helper()

	select {
	case line := <-lines:
		return line
	case <-time.After(15 * time.Second):
		t.Fatal("the stream printed nothing")

		return ""
	}
}
