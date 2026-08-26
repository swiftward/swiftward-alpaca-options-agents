//go:build broker

package marketdata

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Forty-four of the broker's fifty-four tools are never called (see
// TestWhatTheBrokerOffersAndWhatWeUse). Each subtest here calls one of them
// with real arguments and prints a slice of the real answer, so the question
// "is this worth wiring in" is decided from what the broker actually sends
// back, not from its schema description. Read-only: nothing here places,
// replaces, or cancels an order, or touches account config.
func TestUnusedToolsAgainstTheLiveBroker(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "unused-tools-survey", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	call := func(t *testing.T, name string, args map[string]any) {
		t.Helper()
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		for _, content := range result.Content {
			if result.IsError {
				if text, ok := content.(*mcp.TextContent); ok {
					t.Logf("%s(%v) refused: %s", name, args, text.Text)
				}
				continue
			}
			text, ok := content.(*mcp.TextContent)
			if !ok {
				continue
			}
			body := text.Text
			const limit = 2200
			if len(body) > limit {
				body = body[:limit] + "\n...(truncated, full length " + strconv.Itoa(len(text.Text)) + ")"
			}
			t.Logf("%s(%v) -> %d bytes:\n%s", name, args, len(text.Text), body)
		}
	}

	today := time.Now().UTC()
	ymd := func(d time.Time) string { return d.Format("2006-01-02") }

	// get_stock_bars: granularity and how many symbols one call carries.
	t.Run("get_stock_bars daily 5 symbols", func(t *testing.T) {
		call(t, "get_stock_bars", map[string]any{
			"symbols": "AAPL,MSFT,NVDA,TSLA,AVGO", "timeframe": "1Day", "days": 10,
		})
	})
	t.Run("get_stock_bars 1Min today", func(t *testing.T) {
		call(t, "get_stock_bars", map[string]any{
			"symbols": "NVDA", "timeframe": "1Min", "days": 1,
		})
	})
	t.Run("get_stock_bars long lookback", func(t *testing.T) {
		call(t, "get_stock_bars", map[string]any{
			"symbols": "NVDA", "timeframe": "1Day", "start": "2020-01-01", "limit": 10000,
		})
	})

	// get_option_bars: needs a real contract symbol - list QQQ contracts once,
	// take the first, then ask for its history.
	var optionSymbol string
	t.Run("get_option_contracts for a symbol to probe bars/quote with", func(t *testing.T) {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "get_option_contracts",
			Arguments: map[string]any{
				"underlying_symbols": "QQQ", "status": "active", "limit": 5,
			},
		})
		require.NoError(t, err)
		require.False(t, result.IsError, "%v", result.Content)
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				var parsed struct {
					Data struct {
						OptionContracts []struct {
							Symbol string `json:"symbol"`
						} `json:"option_contracts"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(text.Text), &parsed) == nil && len(parsed.Data.OptionContracts) > 0 {
					optionSymbol = parsed.Data.OptionContracts[0].Symbol
				}
				t.Logf("get_option_contracts sample: %.400s", text.Text)
			}
		}
	})
	t.Run("get_option_bars daily", func(t *testing.T) {
		if optionSymbol == "" {
			t.Skip("no contract symbol resolved")
		}
		call(t, "get_option_bars", map[string]any{
			"symbols": optionSymbol, "timeframe": "1Day", "start": "2026-01-01",
		})
	})
	t.Run("get_option_latest_quote vs get_option_snapshot payload size", func(t *testing.T) {
		if optionSymbol == "" {
			t.Skip("no contract symbol resolved")
		}
		call(t, "get_option_latest_quote", map[string]any{"symbols": optionSymbol})
		call(t, "get_option_snapshot", map[string]any{"symbols": optionSymbol})
	})

	// crypto: does it trade around the clock, what pairs, what spread.
	t.Run("get_crypto_bars across a weekend", func(t *testing.T) {
		call(t, "get_crypto_bars", map[string]any{
			"symbols": "BTC/USD,ETH/USD", "timeframe": "1Day", "days": 10,
		})
	})
	t.Run("get_crypto_quotes latest window", func(t *testing.T) {
		call(t, "get_crypto_quotes", map[string]any{
			"symbols": "BTC/USD,ETH/USD", "minutes": 15,
		})
	})

	// corporate actions: does the payload carry earnings/report dates at all.
	t.Run("get_corporate_action_announcements NVDA", func(t *testing.T) {
		call(t, "get_corporate_action_announcements", map[string]any{
			"ca_types": []string{"Dividend", "Split", "Merger", "Reorg", "Spinoff"},
			"since":    ymd(today), "until": ymd(today.AddDate(0, 0, 60)),
			"symbol": "NVDA",
		})
	})
	t.Run("get_corporate_action_announcements AVGO", func(t *testing.T) {
		call(t, "get_corporate_action_announcements", map[string]any{
			"ca_types": []string{"Dividend", "Split", "Merger", "Reorg", "Spinoff"},
			"since":    ymd(today), "until": ymd(today.AddDate(0, 0, 60)),
			"symbol": "AVGO",
		})
	})

	// screening sources: could either replace or feed the hand-written universe.
	t.Run("get_market_movers stocks", func(t *testing.T) {
		call(t, "get_market_movers", map[string]any{"market_type": "stocks", "top": 15})
	})
	t.Run("get_most_active_stocks by volume", func(t *testing.T) {
		call(t, "get_most_active_stocks", map[string]any{"by": "volume", "top": 15})
	})
	t.Run("get_stock_snapshot", func(t *testing.T) {
		call(t, "get_stock_snapshot", map[string]any{"symbols": "AAPL,NVDA,AVGO"})
	})

	// our own result, measured at different resolutions.
	t.Run("get_portfolio_history intraday", func(t *testing.T) {
		call(t, "get_portfolio_history", map[string]any{"period": "1D", "timeframe": "1Min", "intraday_reporting": "extended_hours"})
	})
	t.Run("get_portfolio_history daily", func(t *testing.T) {
		call(t, "get_portfolio_history", map[string]any{"period": "1M", "timeframe": "1D"})
	})

	// real trading days, including the Labor Day holiday inside our window.
	t.Run("get_calendar spanning Labor Day", func(t *testing.T) {
		call(t, "get_calendar", map[string]any{
			"start": ymd(today), "end": ymd(today.AddDate(0, 0, 14)),
		})
	})

	// commissions and fills, one by one.
	t.Run("get_account_activities trade_activity", func(t *testing.T) {
		call(t, "get_account_activities", map[string]any{"category": "trade_activity", "page_size": 10})
	})
	t.Run("get_account_activities_by_type FILL", func(t *testing.T) {
		call(t, "get_account_activities_by_type", map[string]any{"activity_type": "FILL", "page_size": 5})
	})
}
