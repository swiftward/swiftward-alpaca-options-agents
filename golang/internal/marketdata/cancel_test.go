//go:build broker

package marketdata

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// Cancels one resting order by id. An operator's tool, not part of the engine:
// the ladder cancels what the book refuses, and nothing in the running system
// cancels on a change of mind. Pass CANCEL_ORDER_ID.
func TestCancelOneOrder(t *testing.T) {
	id := os.Getenv("CANCEL_ORDER_ID")
	if id == "" {
		t.Skip("CANCEL_ORDER_ID not set")
	}

	broker := NewBroker(os.Getenv("BROKER_MCP_URL"))
	require.NoError(t, broker.CancelOrder(context.Background(), id))
	t.Logf("cancelled %s", id)
}
