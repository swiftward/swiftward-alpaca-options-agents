//go:build broker

package marketdata

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Does the broker know when a company reports?
//
// The rule against selling premium into an earnings report is the one place this
// project depends on a fact it cannot compute: the date. Today it comes from the
// news feed, which says nothing at all about most names - and a rule that reads
// silence as danger refused two structures on 26 August whose companies had no
// report in the window at all.
//
// Answered on 26 August, and the answer is NO. The tool's own schema allows
// exactly five kinds of corporate action - Spinoff, Merger, Split, Reorg,
// Dividend - and an earnings report is none of them. The broker does not carry
// the date at all.
//
// So the rule has no authoritative source here and will not get one from Alpaca.
// That is why it acts on a CONFIRMED date from the news and lets silence pass:
// with no calendar to consult, reading silence as danger refuses the many names
// that simply are not reporting.
//
// Run this again if Alpaca adds a type. Until then, stop looking.
func TestWhetherTheBrokerKnowsWhenACompanyReports(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "calendar-probe", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		if tool.Name == "get_corporate_action_announcements" {
			schema, _ := json.MarshalIndent(tool.InputSchema, "", "  ")
			t.Logf("it takes:\n%s", schema)
		}
	}

	from := time.Now()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_corporate_action_announcements",
		Arguments: map[string]any{
			"symbols": "NVDA,DELL,AVGO,CRM,MU",
			"since":   from.Format(time.DateOnly),
			"until":   from.AddDate(0, 0, 21).Format(time.DateOnly),
		},
	})
	if err != nil {
		t.Logf("the call itself failed: %v", err)
		return
	}
	for _, content := range result.Content {
		text, ok := content.(*mcp.TextContent)
		if !ok {
			continue
		}
		body := text.Text
		if len(body) > 2000 {
			body = body[:2000] + "\n...(truncated)"
		}
		t.Logf("refused=%v, %d bytes:\n%s", result.IsError, len(text.Text), body)
	}
}
