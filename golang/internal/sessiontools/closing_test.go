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

const envelopeInTurnOne = "turn-1/" + envelopeTool

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
func TestAClosingIntentIsExcusedAnEnvelopeThatCannotAnswer(t *testing.T) {
	session := withLimits(t, &askedDouble{
		inTurn:      map[string]bool{},
		triedInTurn: map[string]bool{envelopeInTurnOne: true},
	})

	require.True(t, statingAnIntent(t, session).IsError,
		"an opening is still held to the limits, or this test proves nothing")

	assert.False(t, statingAClose(t, session, "QQQ 701/700 put").IsError,
		"a close goes through an envelope that could not answer")
}

// And no further. The flag is a way past a service that is DOWN, never a way to
// skip the step: both leave the same absence behind, and only one of them is a
// reason to let anything through.
func TestAClosingIntentIsNotExcusedAnEnvelopeNobodyCalled(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{}})

	out := statingAClose(t, session, "QQQ 701/700 put")
	require.True(t, out.IsError, "a close that never asked is refused like any other intent")

	said := ""
	for _, part := range out.Content {
		if text, ok := part.(*mcp.TextContent); ok {
			said += text.Text
		}
	}
	assert.Contains(t, said, envelopeTool, "and it is told what to call")
}

// Opening a structure and then leaving it in the same turn is two decisions about
// one structure. The refusal that stops one decision being written down twice
// must not stop the second of those two.
func TestAStructureCanBeOpenedAndLeftInOneTurn(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{envelopeInTurnOne: true}})

	require.False(t, statingAnIntent(t, session).IsError)
	assert.False(t, statingAClose(t, session, "QQQ 701/700 put").IsError,
		"the same structure may be left in the turn that opened it")
}

func recording(t *testing.T, asked Asked) (*record.Memory, *mcp.ClientSession) {
	t.Helper()

	kept := record.NewMemory()
	server := httptest.NewServer(Tools{
		Record: kept, Now: time.Now,
		Running: &runningDouble{ref: "turn-1"}, Asked: asked,
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return kept, session
}

// The record says which intents were closes, and separately whether the envelope
// actually answered. Two columns, because a close during an outage and a close
// beside a healthy envelope are not the same row.
func TestTheRecordSaysWhichIntentsWereCloses(t *testing.T) {
	kept, session := recording(t, &askedDouble{inTurn: map[string]bool{envelopeInTurnOne: true}})

	require.False(t, statingAnIntent(t, session).IsError)
	require.False(t, statingAClose(t, session, "QQQ 705/704 put").IsError)

	stored, err := kept.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 2)

	opened, left := stored.Intents[0], stored.Intents[1]
	assert.False(t, opened.IsClosing)
	assert.True(t, left.IsClosing)
	for _, intent := range stored.Intents {
		require.NotNil(t, intent.EnvelopeChecked)
		assert.True(t, *intent.EnvelopeChecked,
			"the envelope answered, so both rows say it was read")
	}
}

func TestACloseDuringAnOutageSaysTheEnvelopeWasNotRead(t *testing.T) {
	kept, session := recording(t, &askedDouble{
		inTurn:      map[string]bool{},
		triedInTurn: map[string]bool{envelopeInTurnOne: true},
	})

	require.False(t, statingAClose(t, session, "QQQ 705/704 put").IsError)

	stored, err := kept.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 1)
	require.NotNil(t, stored.Intents[0].EnvelopeChecked)
	assert.False(t, *stored.Intents[0].EnvelopeChecked)
	assert.True(t, stored.Intents[0].IsClosing)
}
