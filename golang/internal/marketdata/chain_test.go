//go:build broker

package marketdata

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// What one call to get_option_chain actually returns, before anything is built
// on it.
//
// The sweep spends two rate-limited calls per underlying - the contract list and
// then the snapshot - against a limit of 180 a minute. If the chain carries the
// same quotes, volatility and greeks in one call, the same limit buys twice the
// universe or twice the frequency. The question is what it carries and whether
// the strikes can be bounded, so the answer comes from the server.
func TestWhatTheOptionChainReturns(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "chain-probe", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == "get_option_chain" {
			schema, _ := json.MarshalIndent(tool.InputSchema, "", "  ")
			t.Logf("get_option_chain takes:\n%s", schema)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_option_chain",
		Arguments: map[string]any{
			"underlying_symbol": "QQQ",
			"strike_price_gte":  700,
			"strike_price_lte":  715,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, "the chain refused: %v", result.Content)

	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		body := text.Text
		if len(body) > 2600 {
			body = body[:2600] + "\n...(truncated)"
		}
		t.Logf("it answered %d bytes; the first of them:\n%s", len(text.Text), body)
	}
}
