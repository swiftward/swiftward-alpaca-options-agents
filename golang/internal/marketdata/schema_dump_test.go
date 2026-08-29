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

// The input shape of every tool this survey is about to call, read from the
// server rather than guessed - the schema is the only source that cannot be
// stale, and a guessed argument name fails the call instead of the survey.
func TestDumpSchemasForSurvey(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	want := map[string]bool{
		"get_stock_bars": true, "get_option_bars": true, "get_crypto_bars": true,
		"get_corporate_action_announcements": true, "get_market_movers": true,
		"get_most_active_stocks": true, "get_stock_snapshot": true,
		"get_portfolio_history": true, "get_calendar": true,
		"get_option_latest_quote": true, "get_crypto_quotes": true,
		"get_account_activities": true, "get_account_activities_by_type": true,
	}

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-dump", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if !want[tool.Name] {
			continue
		}
		schema, _ := json.MarshalIndent(tool.InputSchema, "", "  ")
		t.Logf("=== %s ===\n%s\n---description---\n%s", tool.Name, schema, tool.Description)
	}
}
