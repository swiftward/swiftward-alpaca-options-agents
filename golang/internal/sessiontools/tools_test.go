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

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/volatility"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

// runningDouble is the harness as the tools see it: which turn is in flight and
// who woke it.
type runningDouble struct {
	ref     string
	wokenBy string
}

func (r *runningDouble) RunningTurn() (string, string) { return r.ref, r.wokenBy }

// The client here is the SDK's own, talking to our server over the same
// transport the agent uses. Nothing about the protocol is hand-built.
func connect(t *testing.T, state *record.Memory, now func() time.Time) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{Record: state, Now: now, Running: &runningDouble{}}.Handler())
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
			"thesis":    "premium is rich into the close",
			"structure": "put spread on SPY expiring today",
			"max_loss":  "1% of capital",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 1)
	assert.Equal(t, at, stored.Intents[0].At)
}

func TestRecordIntentRefusesAnIncompleteIntent(t *testing.T) {
	state := record.NewMemory()
	session := connect(t, state, time.Now)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"thesis":    "premium is rich into the close",
			"structure": "put spread on SPY expiring today",
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
	require.NoError(t, state.CallStarted(context.Background(), record.ToolCall{
		Ref: "call-1", TurnRef: "turn-1", Server: "broker", Tool: "place_option_order",
		StartedAt: time.Date(2026, 9, 3, 18, 0, 0, 0, time.UTC), Status: "inProgress",
	}))
	require.NoError(t, state.CallFinished(context.Background(), "call-1",
		time.Date(2026, 9, 3, 18, 0, 1, 0, time.UTC), "failed",
		"insufficient options buying power", ""))
	session := connect(t, state, time.Now)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_state"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	raw, err := json.Marshal(res.StructuredContent)
	require.NoError(t, err)
	var got record.State
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Calls, 1)
	assert.Equal(t, "place_option_order", got.Calls[0].Tool)
	assert.Equal(t, "insufficient options buying power", got.Calls[0].Failure,
		"what an order ran into is what the broker said")
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

type scheduleDouble struct{ sessions []declaration.Scheduled }

func (s *scheduleDouble) Schedule() []declaration.Scheduled { return s.sessions }

// Asked whether it will act on its own, a session that cannot read its schedule
// answers from its own wake-ups alone - and says no while the declaration wakes
// it five times a day.
func TestTheSessionCanReadItsOwnSchedule(t *testing.T) {
	server := httptest.NewServer(Tools{
		Record: record.NewMemory(), Now: time.Now,
		Schedule: &scheduleDouble{sessions: []declaration.Scheduled{
			{Name: "entry", Cause: "окно входа", When: "at 14:20, on mon, tue, wed, thu, fri (America/New_York)"},
		}},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "read_schedule"})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	var answered struct {
		Sessions []declaration.Scheduled `json:"sessions"`
	}
	require.NoError(t, json.Unmarshal(mustJSON(t, res.StructuredContent), &answered))
	require.Len(t, answered.Sessions, 1)
	assert.Equal(t, "entry", answered.Sessions[0].Name)
	assert.Contains(t, answered.Sessions[0].When, "14:20")
}

// With no declaration there is no schedule to read, and a tool answering an
// empty list would tell the session nothing wakes it.
func TestWithoutADeclarationTheScheduleToolIsNotOffered(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)

	for _, tool := range tools.Tools {
		assert.NotEqual(t, "read_schedule", tool.Name)
	}
}

// An intent belongs to the turn that produced it. Filing it under a name the
// model typed would leave the judge comparing timestamps to guess causality.
func TestAnIntentIsFiledUnderTheTurnThatProducedIt(t *testing.T) {
	state := record.NewMemory()
	at := time.Date(2026, 9, 3, 18, 20, 0, 0, time.UTC)
	server := httptest.NewServer(Tools{
		Record: state, Now: func() time.Time { return at },
		Running: &runningDouble{ref: "turn-7", wokenBy: "entry"},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"thesis":    "premium is rich into the close",
			"structure": "SPY put spread 759/758 expiring today",
			"max_loss":  "0.5% of capital",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError, res.Content)

	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	require.Len(t, stored.Intents, 1)
	assert.Equal(t, "turn-7", stored.Intents[0].TurnRef)
	assert.Equal(t, "entry", stored.Intents[0].Session, "the waker of the turn, not a name the model typed")
}

// One order, one intent. A session that states the same structure twice has
// written one decision down twice, and the record would show two trades meant.
func TestTheSameStructureIsNotRecordedTwiceInOneTurn(t *testing.T) {
	state := record.NewMemory()
	at := time.Date(2026, 8, 25, 15, 26, 0, 0, time.UTC)
	server := httptest.NewServer(Tools{
		Record: state, Now: func() time.Time { return at },
		Running: &runningDouble{ref: "turn-9", wokenBy: "entry-call"},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	intent := map[string]any{
		"thesis":    "премия дорога, движения нет",
		"structure": "1× QQQ call credit spread 2026-08-26: sell 718 / buy 719",
		"max_loss":  "$82",
	}

	first, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "record_intent", Arguments: intent})
	require.NoError(t, err)
	require.False(t, first.IsError, first.Content)

	again, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "record_intent", Arguments: intent})
	require.NoError(t, err)
	assert.True(t, again.IsError, "the second statement of the same structure must be refused")

	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	assert.Len(t, stored.Intents, 1)
}

// A different structure in the same turn is a different decision: the judged
// declaration opens one position per underlying inside one turn.
func TestADifferentStructureInTheSameTurnIsRecorded(t *testing.T) {
	state := record.NewMemory()
	server := httptest.NewServer(Tools{
		Record: state, Now: time.Now,
		Running: &runningDouble{ref: "turn-9", wokenBy: "entry"},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	for _, structure := range []string{"SPY 758/757", "QQQ 701/700"} {
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: "record_intent",
			Arguments: map[string]any{
				"thesis": "премия дорога", "structure": structure, "max_loss": "$90",
			},
		})
		require.NoError(t, err)
		require.False(t, res.IsError, res.Content)
	}

	stored, err := state.Read(context.Background())
	require.NoError(t, err)
	assert.Len(t, stored.Intents, 2)
}

// askedDouble answers whether a tool was called in a turn.
type askedDouble struct {
	inTurn map[string]bool
	failed error
}

func (a *askedDouble) AskedInTurn(_ context.Context, turnRef, tool string) (bool, error) {
	if a.failed != nil {
		return false, a.failed
	}
	return a.inTurn[turnRef+"/"+tool], nil
}

func withLimits(t *testing.T, asked Asked) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{
		Record: record.NewMemory(), Now: time.Now,
		Running: &runningDouble{ref: "turn-1", wokenBy: "entry"}, Asked: asked,
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func statingAnIntent(t *testing.T, session *mcp.ClientSession) *mcp.CallToolResult {
	t.Helper()

	out, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "record_intent",
		Arguments: map[string]any{
			"thesis": "sell the far strike", "structure": "QQQ 701/700 put", "max_loss": "80",
		},
	})
	require.NoError(t, err)

	return out
}

// A session states an intent against the limits it believes it has. Sessions are
// turns on ONE conversation, so an envelope answer from an earlier turn is still
// in the model's context - and from inside that is memory, not a stale cache,
// which is why asking a session not to cache does nothing. This checks instead.
func TestAnIntentNeedsTheLimitsReadInTheSameTurn(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{}})

	out := statingAnIntent(t, session)
	require.True(t, out.IsError, "an intent stated from memory is refused")

	said := ""
	for _, part := range out.Content {
		if text, ok := part.(*mcp.TextContent); ok {
			said += text.Text
		}
	}
	assert.Contains(t, said, "read_envelope", "the session is told what to call")
}

func TestAnIntentIsAcceptedWhenTheLimitsWereReadInThisTurn(t *testing.T) {
	session := withLimits(t, &askedDouble{inTurn: map[string]bool{"turn-1/read_envelope": true}})

	out := statingAnIntent(t, session)
	assert.False(t, out.IsError, "the limits were read in this turn, so the intent stands")
}

// Without a database there is nothing to check against, and refusing every
// intent would stop a run that keeps no record at all.
func TestWithoutARecordTheCheckIsSkipped(t *testing.T) {
	server := httptest.NewServer(Tools{
		Record: record.NewMemory(), Now: time.Now,
		Running: &runningDouble{ref: "turn-1", wokenBy: "entry"},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	assert.False(t, statingAnIntent(t, session).IsError)
}
