package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/store"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// The client here is the SDK's own, talking to our server over the same
// transport the agent uses. Nothing about the protocol is hand-built.
func connect(t *testing.T, state *store.Memory, now func() time.Time) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Handler(state, now, nil))
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestToolsAreListed(t *testing.T) {
	session := connect(t, store.NewMemory(), time.Now)

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"record_intent", "read_state"}, names)
}

func TestRecordIntentReachesTheState(t *testing.T) {
	state := store.NewMemory()
	at := time.Date(2026, 9, 4, 13, 40, 0, 0, time.UTC)
	session := connect(t, state, func() time.Time { return at })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"session":   "entry",
			"thesis":    "premium is rich into the close",
			"structure": "put spread on SPXW expiring today",
			"max_loss":  "1% of capital",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	stored := state.Read()
	require.Len(t, stored.Intents, 1)
	assert.Equal(t, "entry", stored.Intents[0].Session)
	assert.Equal(t, at, stored.Intents[0].At)
}

func TestRecordIntentRefusesAnIncompleteIntent(t *testing.T) {
	state := store.NewMemory()
	session := connect(t, state, time.Now)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"session":   "entry",
			"thesis":    "premium is rich into the close",
			"structure": "put spread on SPXW expiring today",
		},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError, "an intent without its maximum loss is not an intent")
	assert.Empty(t, state.Read().Intents)
}

func TestReadStateReturnsWhatWasRecorded(t *testing.T) {
	state := store.NewMemory()
	state.AppendRefusal(store.Refusal{
		At:       time.Date(2026, 9, 3, 18, 0, 0, 0, time.UTC),
		Boundary: "max_loss_per_position",
		Detail:   "structure risks 1.4% of capital, ceiling is 1%",
	})
	session := connect(t, state, time.Now)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_state"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var got store.State
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Refusals, 1)
	assert.Equal(t, "max_loss_per_position", got.Refusals[0].Boundary)
}

// The chat tool is offered only when a chat exists, and when it is offered the
// call goes through the real Telegram client - only Telegram's own server is
// replaced, so the request that leaves is the one this asserts.
func TestPostToChatIsAbsentWithoutAChat(t *testing.T) {
	session := connect(t, store.NewMemory(), time.Now)

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}
	assert.NotContains(t, names, "post_to_chat")
}

func TestPostToChatReachesTelegram(t *testing.T) {
	var seen map[string]any
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &seen))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":-100,"type":"supergroup"}}}`))
	}))
	t.Cleanup(api.Close)

	bot, err := telegram.New(telegram.Config{
		Token:     "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		ChatID:    -1003770330300,
		TopicID:   7287,
		APIServer: api.URL,
	}, zaptest.NewLogger(t))
	require.NoError(t, err)

	server := httptest.NewServer(Handler(store.NewMemory(), time.Now, bot))
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "post_to_chat",
		Arguments: map[string]any{"text": "flatten done, no positions left"},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)
	assert.Equal(t, "flatten done, no positions left", seen["text"])
	assert.EqualValues(t, 7287, seen["message_thread_id"])
}
