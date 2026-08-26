//go:build broker

package marketdata

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

// What the broker's order tool DECLARES about itself.
//
// The envelope design turns on this: a limit can be mirrored into the declared
// schema only if the value it bounds has an address there. Read from the running
// server rather than from documentation, because the two have disagreed before.
func TestWhatTheOrderToolDeclares(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	client := mcp.NewClient(&mcp.Implementation{Name: "schema-probe", Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, nil)
	require.NoError(t, err)

	for _, tool := range tools.Tools {
		if tool.Name != "place_option_order" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		require.NoError(t, err)

		var schema struct {
			Required   []string                   `json:"required"`
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))

		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)

		t.Logf("properties (%d): %v", len(names), names)
		t.Logf("required: %v", schema.Required)
		t.Logf("legs: %s", string(schema.Properties["legs"]))
		for _, absent := range []string{"underlying", "expiration", "max_loss"} {
			_, present := schema.Properties[absent]
			t.Logf("declares %q: %v", absent, present)
		}

		return
	}
	t.Fatal("place_option_order is not among the tools the broker declares")
}
