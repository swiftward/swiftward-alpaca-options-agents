package sessiontools

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/volatility"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

// The client here is the SDK's own, talking to our server over the same
// transport the agent uses. Nothing about the protocol is hand-built.
func connect(t *testing.T, state *record.Memory, now func() time.Time) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{Record: state, Now: now}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func TestToolsAreListed(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}
	assert.ElementsMatch(t, []string{"record_intent", "read_state"}, names)
}

func TestRecordIntentReachesTheState(t *testing.T) {
	state := record.NewMemory()
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

	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 1)
	assert.Equal(t, "entry", stored.Intents[0].Session)
	assert.Equal(t, at, stored.Intents[0].At)
}

func TestRecordIntentRefusesAnIncompleteIntent(t *testing.T) {
	state := record.NewMemory()
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
	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stored.Intents)
}

func TestReadStateReturnsWhatWasRecorded(t *testing.T) {
	state := record.NewMemory()
	require.NoError(t, state.AppendRefusal(context.Background(), record.Refusal{
		At:       time.Date(2026, 9, 3, 18, 0, 0, 0, time.UTC),
		Boundary: "max_loss_per_position",
		Detail:   "structure risks 1.4% of capital, ceiling is 1%",
	}))
	session := connect(t, state, time.Now)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_state"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var got record.State
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Refusals, 1)
	assert.Equal(t, "max_loss_per_position", got.Refusals[0].Boundary)
}

// The chat tool is offered only when a chat exists, and when it is offered the
// call goes through the real Telegram client - only Telegram's own server is
// replaced, so the request that leaves is the one this asserts.
func TestPostToChatIsAbsentWithoutAChat(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

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

	server := httptest.NewServer(Tools{Record: record.NewMemory(), Now: time.Now, Chat: bot}.Handler())
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

// The session sets its own wake-ups, sees them, and cancels them. The tools are
// driven through the real MCP client, and the store behind them is the real one.
func TestTheSessionManagesItsOwnWakeUps(t *testing.T) {
	store, err := wakeup.Open(filepath.Join(t.TempDir(), "wakeups.json"))
	require.NoError(t, err)

	at := time.Date(2026, 9, 4, 13, 30, 0, 0, time.UTC)
	server := httptest.NewServer(Tools{Record: record.NewMemory(), Now: func() time.Time { return at }, Wakeups: store}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	call := func(name string, args map[string]any) *mcp.CallToolResult {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
		require.NoError(t, err)
		return res
	}

	res := call("wake_me_at", map[string]any{
		"cause": "посмотреть, как открылась позиция",
		"at":    at.Add(time.Hour).Format(time.RFC3339),
	})
	require.False(t, res.IsError, res.Content)

	res = call("wake_me_on_price", map[string]any{
		"cause": "цена подошла к проданному страйку", "symbol": "SPY", "direction": "below", "level": 760.0,
	})
	require.False(t, res.IsError, res.Content)

	standing := store.List()
	require.Len(t, standing, 2)

	res = call("list_wakeups", map[string]any{})
	require.False(t, res.IsError, res.Content)
	listed, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	assert.Contains(t, string(listed), "проданному страйку")

	res = call("cancel_wakeup", map[string]any{"id": standing[0].ID})
	require.False(t, res.IsError, res.Content)
	assert.Len(t, store.List(), 1)

	res = call("cancel_wakeup", map[string]any{"id": standing[0].ID})
	assert.True(t, res.IsError, "cancelling what is already gone must not read as success")
}

// A time in the past would never fire, and a session that asked for it would
// wait for a wake-up that cannot come.
func TestAWakeUpInThePastIsRefused(t *testing.T) {
	store, err := wakeup.Open(filepath.Join(t.TempDir(), "wakeups.json"))
	require.NoError(t, err)

	at := time.Date(2026, 9, 4, 13, 30, 0, 0, time.UTC)
	server := httptest.NewServer(Tools{Record: record.NewMemory(), Now: func() time.Time { return at }, Wakeups: store}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "wake_me_at",
		Arguments: map[string]any{"cause": "уже прошло", "at": at.Add(-time.Hour).Format(time.RFC3339)},
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)
	assert.Empty(t, store.List())
}

// With no clock behind them the tools are not offered: a session that sees a
// tool assumes something will act on it.
func TestWakeUpToolsAreAbsentWithoutAStore(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}
	assert.NotContains(t, names, "wake_me_at")
	assert.NotContains(t, names, "list_wakeups")
}

type volatilityDouble struct {
	summary    volatility.Summary
	underlying string
	since      time.Time
}

func (v *volatilityDouble) Summarise(_ context.Context, underlying string, since time.Time) (volatility.Summary, error) {
	v.underlying = underlying
	v.since = since
	return v.summary, nil
}

func TestTheSessionCanAskWhereVolatilityStands(t *testing.T) {
	at := time.Date(2026, 9, 3, 18, 0, 0, 0, time.UTC)
	series := &volatilityDouble{summary: volatility.Summary{
		Underlying: "SPY", Samples: 240, Latest: 0.164, Lowest: 0.101, Median: 0.129, Highest: 0.180, Rank: 79.7,
	}}
	server := httptest.NewServer(Tools{
		Record: record.NewMemory(), Now: func() time.Time { return at }, Volatility: series,
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_volatility_history",
		Arguments: map[string]any{"underlying": "spy", "days": 7},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	assert.Equal(t, "SPY", series.underlying, "the broker names symbols in upper case and so does the history")
	assert.Equal(t, at.AddDate(0, 0, -7), series.since)

	var answered volatility.Summary
	require.NoError(t, json.Unmarshal(mustJSON(t, res.StructuredContent), &answered))
	assert.InDelta(t, 79.7, answered.Rank, 1e-9)
	assert.Equal(t, 240, answered.Samples)
}

// A deployment that records no volatility offers no tool for it: an agent that
// can see a tool assumes it answers.
func TestWithoutAHistoryTheToolIsNotOffered(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range tools.Tools {
		assert.NotEqual(t, "read_volatility_history", tool.Name)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}
