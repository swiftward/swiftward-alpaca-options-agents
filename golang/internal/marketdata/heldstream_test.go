package marketdata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// heldServer answers the handshake like a healthy server and then holds the tool
// call open forever. This is what a gateway in front of the broker did on
// 27 August, and it is a different failure from a server that says nothing: the
// handshake completes, so nothing on the connection looks wrong, and the caller
// waits on a reply that never comes.
func heldServer(t *testing.T) (url string, release func()) {
	t.Helper()

	held := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var asked struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &asked)

		switch {
		case asked.Method == "initialize":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Mcp-Session-Id", "held")
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":`+string(asked.ID)+`,"result":{`+
				`"protocolVersion":"2025-06-18",`+
				`"capabilities":{"tools":{}},`+
				`"serverInfo":{"name":"held","version":"v1"}}}`)

		case strings.HasPrefix(asked.Method, "notifications/"):
			w.WriteHeader(http.StatusAccepted)

		default:
			// The tool call. Accepted, never answered, connection kept.
			select {
			case <-held:
			case <-r.Context().Done():
			}
		}
	}))
	// Order matters: cleanups run last-registered-first, and Close waits for the
	// handler to return. Registering it the other way round deadlocks the test.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(held) })

	return server.URL, func() { close(held) }
}

// The loops that call the broker - the screener, the account recorder, the
// volatility recorder - run on the process context by design, so the only place
// that can end a call held open like this is the call itself.
//
// What this test proves and what it does not. It proves the bound belongs to
// this function: the call ends on its own limit and says why, so a loop skips a
// turn instead of dying. It does NOT reproduce the live hang of 27 August, where
// the context deadline itself did not fire - against this server it does fire,
// and the hand-written server has not been made to hold the SDK the way the
// gateway held it. Until it is, the live evidence is the measurement in the
// journal, not this file.
func TestAHeldToolCallEndsOnItsOwnLimitInsteadOfStoppingTheLoop(t *testing.T) {
	url, _ := heldServer(t)

	was := brokerCallLimit
	brokerCallLimit = 300 * time.Millisecond
	t.Cleanup(func() { brokerCallLimit = was })

	broker := NewBroker(url)

	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- broker.call(context.Background(), "get_clock", nil, &struct{}{}) }()

	select {
	case err := <-done:
		require.Error(t, err, "a call that is never answered must produce an error, not an answer")
		require.Contains(t, err.Error(), "did not answer",
			"the error must name the silence, so the log says why the loop skipped a turn")
		require.Less(t, time.Since(started), 5*time.Second,
			"the call must end on its own limit, not on the test's patience")
	case <-time.After(5 * time.Second):
		t.Fatal("the call outlived its own limit: this is the hang that stopped the screener, " +
			"the account recorder and the volatility recorder on 27 August")
	}
}

// The limit must not swallow the caller's own cancellation: a loop shutting down
// gets its context's error, not a report that the broker was slow.
func TestACancelledCallerIsReportedAsCancelledAndNotAsSilence(t *testing.T) {
	url, _ := heldServer(t)

	was := brokerCallLimit
	brokerCallLimit = 10 * time.Second
	t.Cleanup(func() { brokerCallLimit = was })

	ctx, stop := context.WithCancel(context.Background())
	broker := NewBroker(url)

	done := make(chan error, 1)
	go func() { done <- broker.call(ctx, "get_clock", nil, &struct{}{}) }()
	time.Sleep(100 * time.Millisecond)
	stop()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the caller did not end the call")
	}
}
