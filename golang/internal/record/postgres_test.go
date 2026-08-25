//go:build db

package record

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db/dbtest"
)

// The record is proved against a real Postgres, on its own database, under the
// migrations the stack applies. A pool the test built itself would prove that
// the test can write rows, not that the schema the stack ships accepts them.
func TestPostgresKeepsTheRecord(t *testing.T) {
	pool, err := db.Open(context.Background(), dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	kept, err := NewPostgres(pool, 2)
	require.NoError(t, err)

	ctx := context.Background()
	started := time.Date(2026, 9, 3, 18, 20, 0, 0, time.UTC)
	turn := Turn{
		Ref: "turn-1", ThreadRef: "thread-1", StartedAt: started,
		WokenBy: "entry", Cause: "declaration: entry", Model: "gpt-5.6",
	}
	require.NoError(t, kept.TurnStarted(ctx, turn))
	require.NoError(t, kept.TurnStarted(ctx, turn), "the same turn twice is one row")
	require.NoError(t, kept.TurnFinished(ctx, "turn-1", started.Add(90*time.Second), ""))
	require.NoError(t, kept.AppendIntent(ctx, Intent{
		At: started.Add(time.Minute), TurnRef: "turn-1", Session: "entry",
		Thesis: "premium is rich into the close", Structure: "put spread on SPY expiring today",
		MaxLoss: "1% of capital",
	}))

	require.NoError(t, kept.CallStarted(ctx, ToolCall{
		Ref: "call-1", TurnRef: "turn-1", Server: "broker", Tool: "place_option_order",
		Arguments: []byte(`{"symbol":"SPY260825P00760000","qty":1}`),
		StartedAt: started.Add(30 * time.Second), Status: "inProgress",
	}))
	require.NoError(t, kept.CallStarted(ctx, ToolCall{
		Ref: "call-2", TurnRef: "turn-1", Server: "broker", Tool: "get_option_snapshot",
		StartedAt: started.Add(40 * time.Second), Status: "inProgress",
	}))
	require.NoError(t, kept.CallFinished(ctx, "call-2", started.Add(41*time.Second), "completed", "",
		`{"orders": []}`))

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 1)
	assert.Equal(t, "entry", state.Turns[0].WokenBy)
	assert.Equal(t, started.UTC(), state.Turns[0].StartedAt.UTC())
	require.NotNil(t, state.Turns[0].FinishedAt)
	assert.Equal(t, started.Add(90*time.Second).UTC(), state.Turns[0].FinishedAt.UTC())
	require.Len(t, state.Intents, 1)
	assert.Equal(t, "1% of capital", state.Intents[0].MaxLoss)
	assert.Equal(t, "turn-1", state.Intents[0].TurnRef, "an intent belongs to the turn that produced it")

	// The page carries the newest rows, and only as many as it was told to show.
	for i := range 3 {
		require.NoError(t, kept.TurnStarted(ctx, Turn{
			Ref: fmt.Sprintf("turn-later-%d", i), ThreadRef: "thread-1",
			StartedAt: started.Add(time.Duration(i+1) * time.Hour),
			WokenBy:   "clock", Cause: "declaration: defend",
		}))
	}
	state, err = kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 2)
	assert.Equal(t, "turn-later-2", state.Turns[0].Ref)
	assert.Equal(t, "turn-later-1", state.Turns[1].Ref)

	// The order was in flight when the process died: it may or may not have
	// reached the broker, and the record must not choose.
	inFlight, err := kept.CloseCallsLeftOpen(ctx, started.Add(2*time.Minute))
	require.NoError(t, err)
	assert.Equal(t, 1, inFlight)

	state, err = kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Calls, 2)
	byRef := map[string]ToolCall{}
	for _, call := range state.Calls {
		byRef[call.Ref] = call
	}
	assert.Equal(t, StatusUnknown, byRef["call-1"].Status)
	assert.Equal(t, RestartedFailure, byRef["call-1"].Failure)
	assert.JSONEq(t, `{"symbol":"SPY260825P00760000","qty":1}`, string(byRef["call-1"].Arguments))
	assert.Equal(t, "completed", byRef["call-2"].Status, "a call that ended keeps how it ended")
	assert.Equal(t, `{"orders": []}`, byRef["call-2"].Answer, "and what it answered, where a refusal lives")
	assert.Empty(t, byRef["call-2"].Arguments, "a call with no arguments reported carries none")

	// The three later turns were never closed: this is what a process dying
	// mid-turn leaves behind, and startup has to answer for it.
	left, err := kept.CloseTurnsLeftOpen(ctx, started.Add(4*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, left)

	state, err = kept.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Turns[0].FinishedAt)
	assert.Equal(t, RestartedFailure, state.Turns[0].Failure)

	again, err := kept.CloseTurnsLeftOpen(ctx, started.Add(5*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, again, "a closed turn is not closed twice")
}

// The database is what makes a fill new, so the constraint that guarantees it is
// proved against the real schema. A double agreeing with itself would not catch
// a missing index, and a missing index is silent: every pass of the ladder would
// write the same trade again and say it again.
func TestPostgresWritesAFillOnce(t *testing.T) {
	pool, err := db.Open(context.Background(), dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	kept, err := NewPostgres(pool, 20)
	require.NoError(t, err)

	ctx := context.Background()
	at := time.Date(2026, 8, 25, 19, 12, 0, 0, time.UTC)
	price := -0.28
	step := ExecutionStep{OrderRef: "order-1", At: at, Action: "filled", Was: -0.30, Became: &price}

	first, err := kept.NoteFill(ctx, step)
	require.NoError(t, err)
	assert.True(t, first, "the first time is the one that writes it")

	again, err := kept.NoteFill(ctx, step)
	require.NoError(t, err)
	assert.False(t, again, "the same order does not fill twice")

	other := step
	other.OrderRef = "order-2"
	third, err := kept.NoteFill(ctx, other)
	require.NoError(t, err)
	assert.True(t, third, "a different order is a different fill")

	// Walking the price is not a fill: the constraint bounds one action, not the
	// whole table, or an order could be walked only once.
	require.NoError(t, kept.AppendExecutionStep(ctx, ExecutionStep{
		OrderRef: "order-1", At: at, Action: "walked", Was: -0.30, Became: &price,
	}))
	require.NoError(t, kept.AppendExecutionStep(ctx, ExecutionStep{
		OrderRef: "order-1", At: at.Add(time.Minute), Action: "walked", Was: -0.29, Became: &price,
	}))

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	fills, walks := 0, 0
	for _, kept := range state.Steps {
		switch kept.Action {
		case "filled":
			fills++
		case "walked":
			walks++
		}
	}
	assert.Equal(t, 2, fills, "two orders filled, each written once")
	assert.Equal(t, 2, walks, "the same order walked twice, both kept")
}
