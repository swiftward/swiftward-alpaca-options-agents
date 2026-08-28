package mailbox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/harness"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/mailbox"
)

// The harness must be able to hold this instead of a codex process, or none of
// the rest matters. Asserted at compile time rather than described in a comment.
var _ harness.Conversation = (*mailbox.Mailbox)(nil)

const token = "secret-token"

func serve(t *testing.T, m *mailbox.Mailbox) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(m.Handler("/mailbox/"))
	t.Cleanup(server.Close)

	return server
}

func poll(t *testing.T, server *httptest.Server, seconds int) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/mailbox/"+token+"/poll?wait="+itoa(seconds), nil)
	require.NoError(t, err)

	res, err := server.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

func post(t *testing.T, server *httptest.Server, action, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		server.URL+"/mailbox/"+token+"/"+action, strings.NewReader(body))
	require.NoError(t, err)

	res, err := server.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })

	return res
}

func itoa(n int) string { return string([]byte{byte('0' + n)}) }

// A turn parked by the clock is handed to whoever polls, whole.
func TestAPolledTurnCarriesWhatTheHarnessAskedFor(t *testing.T) {
	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)

	thread, err := m.Open(context.Background())
	require.NoError(t, err)

	turnID, err := m.Turn(context.Background(), "market opens in five minutes", "gpt-5-codex")
	require.NoError(t, err)

	res := poll(t, server, 2)
	require.Equal(t, http.StatusOK, res.StatusCode)

	var got mailbox.Delivery
	require.NoError(t, json.NewDecoder(res.Body).Decode(&got))

	assert.Equal(t, mailbox.KindTurn, got.Kind)
	assert.Equal(t, turnID, got.Turn)
	assert.Equal(t, thread, got.Thread)
	assert.Equal(t, "market opens in five minutes", got.Text)
	assert.Equal(t, "gpt-5-codex", got.Model)
	assert.Zero(t, m.Pending(), "the delivery is taken, not copied")
}

// The hold ends the moment there is something to say, not when it expires. This
// is the whole reason a poll is held open at all: without it a client learns
// about a window some fraction of a minute after it opened.
func TestTheHoldEndsAsSoonAsAThereIsATurn(t *testing.T) {
	m := mailbox.New(token, 5*time.Second, time.Minute, nil)
	server := serve(t, m)

	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = m.Turn(context.Background(), "wake up", "")
	}()

	started := time.Now()
	res := poll(t, server, 5)
	waited := time.Since(started)

	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Less(t, waited, 2*time.Second, "the poll waited out its hold instead of being woken")
}

// An empty hold is answered with 204 and nothing else. A client must be able to
// tell "nothing happened" from "here is an empty turn" without parsing.
func TestAnExpiredHoldSaysNothingRatherThanAnEmptyTurn(t *testing.T) {
	m := mailbox.New(token, 100*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	res := poll(t, server, 1)
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
}

// What the client says comes back as the harness's own events, so the room and
// the record get it without anything in between knowing where it came from.
func TestWhatTheClientSaysReachesTheHarness(t *testing.T) {
	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)

	turnID, err := m.Turn(context.Background(), "look at the account", "")
	require.NoError(t, err)

	res := post(t, server, "say", `{"turn":"`+turnID+`","text":"nothing to do, the market is closed"}`)
	require.Equal(t, http.StatusAccepted, res.StatusCode)

	res = post(t, server, "done", `{"turn":"`+turnID+`"}`)
	require.Equal(t, http.StatusAccepted, res.StatusCode)

	first := next(t, m)
	assert.Equal(t, agent.KindText, first.Kind)
	assert.Equal(t, turnID, first.TurnID)
	assert.Equal(t, "nothing to do, the market is closed", first.Text)

	second := next(t, m)
	assert.Equal(t, agent.KindTurnDone, second.Kind)
	assert.Equal(t, turnID, second.TurnID)
}

// A turn ended twice is accepted twice. A client that lost its connection and
// repeated itself is behaving correctly, and a failure here would teach it not
// to.
func TestEndingATurnTwiceIsNotAnError(t *testing.T) {
	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)

	turnID, err := m.Turn(context.Background(), "check", "")
	require.NoError(t, err)

	require.Equal(t, http.StatusAccepted, post(t, server, "done", `{"turn":"`+turnID+`"}`).StatusCode)
	require.Equal(t, http.StatusAccepted, post(t, server, "done", `{"turn":"`+turnID+`"}`).StatusCode)

	assert.Equal(t, agent.KindTurnDone, next(t, m).Kind)
	assert.Nil(t, maybeNext(m), "the second ending produced a second turn_done")
}

// A person writing into a turn that already ended must not be swallowed. The
// mailbox refuses the steer, and the harness reads that refusal as "start a
// fresh turn" - which is the answer the person is waiting for.
func TestSteeringAFinishedTurnIsRefusedSoTheHarnessStartsAFreshOne(t *testing.T) {
	m := mailbox.New(token, time.Second, time.Minute, nil)
	server := serve(t, m)

	turnID, err := m.Turn(context.Background(), "check", "")
	require.NoError(t, err)

	require.NoError(t, m.Steer(context.Background(), turnID, "and also look at QQQ"))

	require.Equal(t, http.StatusAccepted, post(t, server, "done", `{"turn":"`+turnID+`"}`).StatusCode)
	assert.Error(t, m.Steer(context.Background(), turnID, "too late"))
}

// Interrupt ends the turn here without waiting to be told that it did. The case
// it exists for is a client that has stopped answering, and waiting for that
// client would block the clock on the thing the interrupt was meant to unblock.
func TestInterruptEndsTheTurnWithoutTheClient(t *testing.T) {
	m := mailbox.New(token, time.Second, time.Minute, nil)

	turnID, err := m.Turn(context.Background(), "a turn that will overrun", "")
	require.NoError(t, err)

	require.NoError(t, m.Interrupt(context.Background(), turnID))

	ev := next(t, m)
	assert.Equal(t, agent.KindTurnDone, ev.Kind)
	assert.Equal(t, turnID, ev.TurnID)
}

// A turn nobody came for is given up on, said out loud, and closed. Left alone
// it would keep the harness believing a session is at work, and every later
// window would be refused because one is already running.
func TestATurnNobodyTookIsGivenUpOnAndSaidOutLoud(t *testing.T) {
	now := time.Now()
	m := mailbox.New(token, time.Second, 50*time.Millisecond, nil)
	m.Now = func() time.Time { return now }

	turnID, err := m.Turn(context.Background(), "the window nobody attended", "")
	require.NoError(t, err)

	now = now.Add(time.Minute)

	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = m.Run(ctx) }()
	defer stop()

	said := next(t, m)
	require.Equal(t, agent.KindText, said.Kind)
	assert.Contains(t, said.Text, "никто не забрал")

	done := next(t, m)
	assert.Equal(t, agent.KindTurnDone, done.Kind)
	assert.Equal(t, turnID, done.TurnID)
	assert.Zero(t, m.Pending())
}

// A wrong token is answered exactly the way a path that does not exist is
// answered. Anything else tells a guesser that the mailbox is there.
func TestAWrongTokenIsIndistinguishableFromNoMailbox(t *testing.T) {
	m := mailbox.New(token, 50*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/mailbox/wrong-token/poll", nil)
	require.NoError(t, err)
	wrong, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = wrong.Body.Close() }()

	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/mailbox/", nil)
	require.NoError(t, err)
	missing, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = missing.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, wrong.StatusCode)
	assert.Equal(t, missing.StatusCode, wrong.StatusCode)
}

// A mailbox with no token serves nobody. The safe reading of a secret that was
// never set is "this deployment is broken", not "then anyone may have it".
func TestAMailboxWithNoTokenRefusesEveryone(t *testing.T) {
	m := mailbox.New("", 50*time.Millisecond, time.Minute, nil)
	server := httptest.NewServer(m.Handler("/mailbox/"))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		server.URL+"/mailbox//poll", nil)
	require.NoError(t, err)
	res, err := server.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()

	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

// A client may ask for a shorter hold than the deployment's, never a longer one:
// the bound belongs to the side holding the connection open.
func TestAClientMayShortenTheHoldButNotLengthenIt(t *testing.T) {
	m := mailbox.New(token, 100*time.Millisecond, time.Minute, nil)
	server := serve(t, m)

	started := time.Now()
	res := poll(t, server, 9)
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Less(t, time.Since(started), 3*time.Second, "the client's longer wait was honoured")
}

func next(t *testing.T, m *mailbox.Mailbox) agent.Event {
	t.Helper()

	select {
	case ev := <-m.Events():
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("the harness was told nothing")

		return agent.Event{}
	}
}

func maybeNext(m *mailbox.Mailbox) *agent.Event {
	select {
	case ev := <-m.Events():
		return &ev
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// The harness must not judge a driver on what that driver cannot show it. A
// mailbox carries what the agent says, not what it does: the client calls its own
// servers, and those calls never pass through here. Without this, the broker
// watchdog fires on a perfectly healthy agent - three scan turns in a row on 28
// August warned "a turn finished without reaching the broker" while the session
// was reading chains and quotes the whole time.
func TestAMailboxDoesNotClaimToSeeToolCalls(t *testing.T) {
	var conversation harness.Conversation = mailbox.New(token, time.Second, time.Minute, nil)

	reporter, ok := conversation.(harness.ReportsToolCalls)
	require.True(t, ok, "ящик обязан отвечать на вопрос, видит ли он вызовы инструментов")
	assert.False(t, reporter.ReportsToolCalls())
}
