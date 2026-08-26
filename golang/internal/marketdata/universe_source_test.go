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

// Where a list of underlyings should come from.
//
// The screener's 284 names were typed by hand, and a hand-typed list drifts: it
// already disagreed once with what the envelope permits, and the cost was
// visible - structures priced and then refused. It also cannot answer the only
// question that matters for a screener, which is where the trading is.
//
// So: can the broker name the busiest stocks, and does what it returns carry
// enough to build a universe from?
func TestWhereAUniverseCouldComeFrom(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "universe-probe", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == "get_most_active_stocks" {
			schema, _ := json.MarshalIndent(tool.InputSchema, "", "  ")
			t.Logf("get_most_active_stocks takes:\n%s", schema)
		}
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_most_active_stocks",
		Arguments: map[string]any{"top": 100},
	})
	require.NoError(t, err)
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		body := text.Text
		if len(body) > 2400 {
			body = body[:2400] + "\n...(truncated)"
		}
		t.Logf("refused=%v, %d bytes:\n%s", result.IsError, len(text.Text), body)
	}
}
