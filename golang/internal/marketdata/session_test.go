package marketdata

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// The session is made once and used for every call after it. This is the whole
// reason it is kept: measured against the policy gateway on 27 August, opening
// one costs 2.68 seconds - initialize, initialized, tools/list - and a call on
// an open one costs 0.85. A sweep asks about two hundred and ninety things, so
// a session per call is three quarters of what the sweep spends.
func TestTheSessionIsMadeOnceAndUsedAgain(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "probe", Version: "v1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "get_clock", Description: "the clock"},
		func(ctx context.Context, request *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			return nil, map[string]any{"data": map[string]any{"is_open": true}}, nil
		})

	var handshakes atomic.Int64
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	counting := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"initialize"`) {
				handshakes.Add(1)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
		}
		handler.ServeHTTP(w, r)
	})

	front := httptest.NewServer(counting)
	t.Cleanup(front.Close)

	broker := NewBroker(front.URL)
	t.Cleanup(func() { _ = broker.Close() })

	for range 5 {
		open, err := broker.MarketOpen(context.Background())
		require.NoError(t, err)
		require.True(t, open, "the fake broker says the market is open")
	}

	require.EqualValues(t, 1, handshakes.Load(),
		"five questions must share one session, not open five")
}
