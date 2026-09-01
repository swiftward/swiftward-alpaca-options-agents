package sessiontools

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

func statingAClose(t *testing.T, session *mcp.ClientSession, structure string) *mcp.CallToolResult {
	t.Helper()

	out, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"thesis": "leave it", "structure": structure, "max_loss": "0",
			"underlying_price": "612.40", "closing": true,
		},
	})
	require.NoError(t, err)

	return out
}

// The envelope says how much may be RISKED. Leaving a position risks nothing it
// was not already risking, and a session that cannot record an intent cannot
// order at all by its own rule - so a limit service that cannot answer would hold
// a position open. Measured 1 September on both accounts.
func TestAClosingIntentDoesNotWaitForTheLimits(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{}})

	require.True(t, statingAnIntent(t, session).IsError,
		"an opening is still held to the limits, or this test proves nothing")

	out := statingAClose(t, session, "QQQ 701/700 put")
	assert.False(t, out.IsError, "a close is recorded whatever the envelope says")
}

// Opening a structure and then leaving it in the same turn is two decisions about
// one structure. The refusal that stops one decision being written down twice
// must not stop the second of those two.
func TestAStructureCanBeOpenedAndLeftInOneTurn(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{"turn-1/read_envelope": true}})

	require.False(t, statingAnIntent(t, session).IsError)
	assert.False(t, statingAClose(t, session, "QQQ 701/700 put").IsError,
		"the same structure may be left in the turn that opened it")
}

// The record separates a close from a deployment that cannot check: both leave
// envelope_checked false, and only one of them is a decision anybody made.
func TestTheRecordSaysWhichIntentsWereCloses(t *testing.T) {
	kept := record.NewMemory()
	server := httptest.NewServer(Tools{
		Record: kept, Now: time.Now,
		Running: &runningDouble{ref: "turn-1"},
		Asked:   &askedDouble{inTurn: map[string]bool{"turn-1/read_envelope": true}},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	require.False(t, statingAnIntent(t, session).IsError)
	require.False(t, statingAClose(t, session, "QQQ 705/704 put").IsError)

	stored, err := kept.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 2)

	opened, left := stored.Intents[0], stored.Intents[1]
	assert.False(t, opened.IsClosing)
	require.NotNil(t, opened.EnvelopeChecked)
	assert.True(t, *opened.EnvelopeChecked, "an opening carries the check it passed")

	assert.True(t, left.IsClosing)
	require.NotNil(t, left.EnvelopeChecked)
	assert.False(t, *left.EnvelopeChecked, "a close is not held to it, and says so")
}
