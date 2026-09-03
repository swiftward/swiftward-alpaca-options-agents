package marketdata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The profit watch places its close itself, on a path the policy gateway is not
// on, and the whole case for that is one property: the order can only REDUCE what
// is held. It rests on `position_intent` reaching the broker as buy_to_close or
// sell_to_close on every leg, because the broker refuses the order outright if it
// would open anything.
//
// Nothing tested that. `CloseStructure` was exercised only through a double in the
// profit watch's own tests, which answers whether the watch calls it, not what it
// sends. A close that quietly asked to OPEN would have passed every gate in this
// repository and been stopped, if at all, at the broker.
func TestEveryLegOfAClosingOrderSaysItCloses(t *testing.T) {
	sent := recordingBroker(t)

	id, err := sent.broker.CloseStructure(context.Background(), []Leg{
		{Symbol: "SPY260904C00650000", Ratio: 1, Buy: true},
		{Symbol: "SPY260904C00649000", Ratio: 1, Buy: false},
	}, 3, -0.42, "watch-1")
	require.NoError(t, err)
	assert.Equal(t, "order-1", id)

	got := sent.arguments()
	assert.Equal(t, "mleg", got["order_class"])
	assert.Equal(t, "3", got["qty"])

	legs, ok := got["legs"].([]any)
	require.True(t, ok, "the order carries its legs")
	require.Len(t, legs, 2)

	intents := map[string]string{}
	for _, raw := range legs {
		leg := raw.(map[string]any)
		intents[leg["symbol"].(string)] = leg["position_intent"].(string)
	}
	assert.Equal(t, "buy_to_close", intents["SPY260904C00650000"],
		"a leg bought back closes; buy_to_open here would open a position nobody chose")
	assert.Equal(t, "sell_to_close", intents["SPY260904C00649000"],
		"a leg sold closes the long; sell_to_open here would open a naked short")
}

// A set is made of legs, and a leg that goes into it fewer than once is not a
// ratio anybody meant. Sending it would ask the broker for a structure with a leg
// missing, which fills as something other than what the watch priced.
func TestAClosingOrderWithNothingToCloseIsRefusedBeforeItIsSent(t *testing.T) {
	sent := recordingBroker(t)
	ctx := context.Background()
	legs := []Leg{{Symbol: "SPY260904C00650000", Ratio: 1, Buy: true}}

	_, err := sent.broker.CloseStructure(ctx, nil, 1, -0.42, "watch-2")
	assert.Error(t, err, "no legs is not an order")

	_, err = sent.broker.CloseStructure(ctx, legs, 0, -0.42, "watch-3")
	assert.Error(t, err, "nought sets closes nothing")

	_, err = sent.broker.CloseStructure(ctx, []Leg{{Symbol: "SPY260904C00650000", Ratio: 0, Buy: true}},
		1, -0.42, "watch-4")
	assert.Error(t, err, "a leg that goes into the set no times is not a ratio")

	assert.Zero(t, sent.calls(), "none of those reached the broker")
}

// A broker pointed at an MCP server that answers place_option_order and keeps
// what it was asked for.
type recorded struct {
	mu     sync.Mutex
	seen   []map[string]any
	broker *Broker
}

func recordingBroker(t *testing.T) *recorded {
	t.Helper()
	kept := &recorded{}

	server := mcp.NewServer(&mcp.Implementation{Name: "probe", Version: "v1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "place_option_order", Description: "places an order"},
		func(ctx context.Context, request *mcp.CallToolRequest, arguments map[string]any) (*mcp.CallToolResult, any, error) {
			kept.mu.Lock()
			kept.seen = append(kept.seen, arguments)
			kept.mu.Unlock()
			return nil, map[string]any{"data": map[string]any{"id": "order-1"}}, nil
		})

	front := httptest.NewServer(mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server }, nil))
	t.Cleanup(front.Close)

	kept.broker = NewBroker(front.URL)
	t.Cleanup(func() { _ = kept.broker.Close() })
	return kept
}

func (r *recorded) arguments() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[len(r.seen)-1]
}

func (r *recorded) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.seen)
}
