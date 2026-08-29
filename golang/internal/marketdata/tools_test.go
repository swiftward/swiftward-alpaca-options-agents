//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// Everything the broker offers, against everything this project asks for.
//
// A tool nobody calls is a capability nobody weighed. This lists the whole
// surface and marks what the engine already uses, so the question "are we using
// all of it" is answered from the server rather than from memory.
func TestWhatTheBrokerOffersAndWhatWeUse(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "inventory", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	listed, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	// What internal/marketdata calls today. Kept beside the listing so a tool
	// added to one and not the other shows up as a difference, not as silence.
	used := map[string]bool{
		"get_account_info": true, "get_all_positions": true, "get_orders": true,
		"replace_order_by_id": true, "cancel_order_by_id": true,
		"get_stock_latest_trade": true, "get_clock": true,
		"get_option_contracts": true, "get_option_snapshot": true,
		"place_option_order": true,
	}

	names := make([]string, 0, len(listed.Tools))
	summary := map[string]string{}
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
		first := strings.SplitN(strings.TrimSpace(tool.Description), "\n", 2)[0]
		if len(first) > 110 {
			first = first[:110]
		}
		summary[tool.Name] = first
	}
	sort.Strings(names)

	unused := 0
	t.Logf("the broker offers %d tools", len(names))
	for _, name := range names {
		mark := "   "
		if used[name] {
			mark = "USE"
		} else {
			unused++
		}
		t.Log(fmt.Sprintf("%s  %-34s %s", mark, name, summary[name]))
	}
	t.Logf("%d offered, %d used, %d never called", len(names), len(names)-unused, unused)

	for name := range used {
		if summary[name] == "" {
			t.Errorf("we call %s and the broker does not offer it", name)
		}
	}
}
