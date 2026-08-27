package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A server that accepts the connection and then says nothing must not stop the
// caller. This is the failure we actually met: reading our own orders from a
// machine that could not reach the proxy sat inside Connect for eight minutes
// and came back with nothing. The loops that call the broker - the screener, the
// ladder, the defence - run on the process context, which by design has no
// deadline, so a bound that lives in the caller would not have caught it.
func TestSilenceFromTheBrokerEndsTheCallInsteadOfTheLoop(t *testing.T) {
	held := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-held:
		case <-r.Context().Done():
		}
	}))
	// Order matters: cleanups run last-registered-first, and Close waits for the
	// handler to return. Registering it the other way round deadlocks the test.
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(held) })

	was := brokerCallLimit
	brokerCallLimit = 300 * time.Millisecond
	t.Cleanup(func() { brokerCallLimit = was })

	broker := NewBroker(server.URL)

	done := make(chan error, 1)
	go func() { done <- broker.call(context.Background(), "get_account_info", nil, &struct{}{}) }()

	select {
	case err := <-done:
		require.Error(t, err, "a server that never answers must produce an error, not an answer")
	case <-time.After(5 * time.Second):
		t.Fatal("the call outlived its own limit: this is the hang the limit exists to stop")
	}
}
