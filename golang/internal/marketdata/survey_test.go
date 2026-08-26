//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Calls the broker's unused read-only tools with real arguments and prints what
// each actually returns.
//
// A description is not an answer: get_option_chain's schema promised greeks on
// every contract and the expiry-day book carries none, and
// get_corporate_action_announcements sounds like a calendar and holds no
// earnings date. The only way to know what a tool is worth is to call it.
//
// Reads only. Nothing here can place, close, cancel or exercise anything - the
// account it runs against is trading.
func TestWhatTheUnusedToolsActuallyReturn(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "survey", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	day := time.Now().Format(time.DateOnly)
	for _, ask := range []struct {
		tool string
		with map[string]any
	}{
		{"get_stock_bars", map[string]any{
			"symbols": "QQQ", "timeframe": "1Min", "start": day, "limit": 5}},
		{"get_stock_snapshot", map[string]any{"symbol_or_symbols": "QQQ"}},
		{"get_portfolio_history", map[string]any{"period": "1D", "timeframe": "5Min"}},
		{"get_calendar", map[string]any{"start": day, "end": day}},
		{"get_account_activities", map[string]any{"activity_types": "FILL"}},
		{"get_crypto_quotes", map[string]any{
			"symbols": "BTC/USD", "start": day, "limit": 3}},
		{"get_option_bars", map[string]any{
			"symbols": "QQQ260828P00700000", "timeframe": "1Min", "start": day, "limit": 3}},
	} {
		t.Run(ask.tool, func(t *testing.T) {
			result, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name: ask.tool, Arguments: ask.with,
			})
			if err != nil {
				t.Logf("call failed: %v", err)
				return
			}
			for _, content := range result.Content {
				text, ok := content.(*mcp.TextContent)
				if !ok {
					continue
				}
				body := strings.TrimSpace(text.Text)
				if len(body) > 1100 {
					body = body[:1100] + "...(truncated)"
				}
				t.Log(fmt.Sprintf("refused=%v, %d bytes:\n%s", result.IsError, len(text.Text), body))
			}
		})
	}
}
