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
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
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
		Ref: "turn-1", ThreadRef: "thread-1", StartedAt: started, Model: "gpt-5.6",
	}
	require.NoError(t, kept.TurnStarted(ctx, turn, "entry", "declaration: entry"))
	require.NoError(t, kept.TurnStarted(ctx, turn, "entry", "declaration: entry"),
		"the same turn twice is one row, and one opening cause")
	require.NoError(t, kept.TurnFinished(ctx, "turn-1", started.Add(90*time.Second), ""))
	// No underlying price on purpose: a session states one when it read one, and
	// the column is numeric, so an empty string has to reach Postgres as NULL. It
	// reached it as "" until 29 August 2026 and the whole row was refused - on
	// the one call this system asks the agent to make before every order.
	require.NoError(t, kept.AppendIntent(ctx, Intent{
		At: started.Add(time.Minute), TurnRef: "turn-1",
		Thesis: "premium is rich into the close", Structure: "put spread on SPY expiring today",
		MaxLoss: "1% of capital",
	}))
	checked := true
	require.NoError(t, kept.AppendIntent(ctx, Intent{
		At: started.Add(2 * time.Minute), TurnRef: "turn-1",
		Thesis: "and one that did read the price", Structure: "put spread on QQQ",
		MaxLoss: "1% of capital", UnderlyingPrice: "701.245000", EnvelopeChecked: &checked,
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
	require.NoError(t, kept.AppendSaid(ctx, Said{
		TurnRef: "turn-1", At: started.Add(50 * time.Second),
		Text: "No positions are open; no defense action taken.",
	}))

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 1)
	require.Len(t, state.Causes, 1, "written twice, opened once")
	assert.Equal(t, "entry", state.Causes[0].WokenBy)
	assert.Equal(t, started.UTC(), state.Turns[0].StartedAt.UTC())
	require.NotNil(t, state.Turns[0].FinishedAt)
	assert.Equal(t, started.Add(90*time.Second).UTC(), state.Turns[0].FinishedAt.UTC())
	// Newest first, so the one carrying a price is [0] and the one without is [1].
	require.Len(t, state.Intents, 2)
	assert.Equal(t, "701.245000", state.Intents[0].UnderlyingPrice, "a price that was read comes back")
	assert.Empty(t, state.Intents[1].UnderlyingPrice, "and one that was not stays absent rather than zero")
	require.NotNil(t, state.Intents[0].EnvelopeChecked)
	assert.True(t, *state.Intents[0].EnvelopeChecked, "an intent says whether it was checked against the envelope")
	assert.Nil(t, state.Intents[1].EnvelopeChecked, "and absent is the third answer, for a row that never said")
	assert.Equal(t, "1% of capital", state.Intents[1].MaxLoss)
	assert.Equal(t, "turn-1", state.Intents[1].TurnRef, "an intent belongs to the turn that produced it")
	// The agent's words are read like everything else. This check stands here
	// because its absence already cost us a divergence: the rows were written, Read
	// never asked for them, and the page showed the agent silent while it was
	// telling the room in Telegram what it was doing.
	require.Len(t, state.Said, 1, "what the agent said is read back, not only written")
	assert.Equal(t, "No positions are open; no defense action taken.", state.Said[0].Text)
	assert.Equal(t, "turn-1", state.Said[0].TurnRef, "a word belongs to the turn that produced it")

	// The page carries the newest rows, and only as many as it was told to show.
	for i := range 3 {
		require.NoError(t, kept.TurnStarted(ctx, Turn{
			Ref: fmt.Sprintf("turn-later-%d", i), ThreadRef: "thread-1",
			StartedAt: started.Add(time.Duration(i+1) * time.Hour),
		}, "clock", "declaration: defend"))
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

// Everything the screener worked out survives the round trip, field for field.
//
// Asserting one field at a time is what let this break twice. A column added to
// the writer and not the reader compiles, runs, and stores NULL; on 26 August a
// named argument was left out of the map and pgx put NULL in for it without a
// word, so `edge_from` was empty on every row while every build was green. The
// whole struct is compared here precisely so the next added field cannot pass
// unless somebody carried it through both halves.
func TestPostgresKeepsEverythingTheScreenerWorkedOut(t *testing.T) {
	pool, err := db.Open(context.Background(), dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	kept, err := NewPostgres(pool, 20)
	require.NoError(t, err)

	ctx := context.Background()
	at := time.Date(2026, 8, 26, 15, 0, 0, 0, time.UTC)
	// Expirations are kept as dates, so the fixture states them as dates.
	soon := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	delta, edge, fromVolatility := -0.1432, 3.1, -2.5

	measured := screener.Candidate{
		Underlying: "QQQ", Type: "put", Expiration: soon,
		Short: "QQQ260828P00701000", Long: "QQQ260828P00700000",
		ShortStrike: 701, LongStrike: 700, Price: 710, OutOfTheMoney: 1.27,
		Credit: 0.20, Risk: 0.80, CreditToRisk: 25, Cost: 0.16, CostShare: 80,
		CreditAfterCost: 0.12, Delta: &delta, Edge: &edge, EdgeFrom: screener.FromDelta,
	}
	// Expiry day: no delta, and the edge read off the price of volatility
	// instead. Absent must survive as absent rather than as zero.
	blind := screener.Candidate{
		Underlying: "SPY", Type: "call", Expiration: today,
		Short: "SPY260826C00770000", Long: "SPY260826C00771000",
		ShortStrike: 770, LongStrike: 771, Price: 765, OutOfTheMoney: 0.65,
		Credit: 0.10, Risk: 0.90, CreditToRisk: 11, Cost: 0.04, CostShare: 40,
		CreditAfterCost: 0.08, Edge: &fromVolatility, EdgeFrom: screener.FromBorrowedVolatility,
	}

	require.NoError(t, kept.RecordCandidates(ctx, at, []screener.Candidate{measured, blind}))

	found, takenAt, err := kept.Candidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, found, 2)
	// When the sweep was taken comes back with it: rows outlive the sweep that
	// wrote them, and a reader that cannot see the age cannot judge the list.
	assert.True(t, takenAt.Equal(at), "the sweep's own time, not the read's")

	by := map[string]screener.Candidate{}
	for _, one := range found {
		by[one.Underlying] = one
	}

	assert.Equal(t, measured, by["QQQ"])
	assert.Equal(t, blind, by["SPY"])
	assert.Nil(t, by["SPY"].Delta, "no delta is not a delta of zero")
}

// Two things have to hold at once, and they pull in opposite directions: the
// agent must never be offered a price from an older sweep, and an older sweep
// must not be destroyed - it is the only record of what the option book offered,
// since the broker publishes no history of two-sided option quotes.
func TestAnOlderSweepSurvivesButIsNotOffered(t *testing.T) {
	pool, err := db.Open(context.Background(), dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	kept, err := NewPostgres(pool, 20)
	require.NoError(t, err)

	ctx := context.Background()
	today := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	later := earlier.Add(10 * time.Minute)

	sweep := func(underlying string) screener.Candidate {
		return screener.Candidate{
			Underlying: underlying, Type: "put", Expiration: today,
			Short: underlying + "260826P00700000", Long: underlying + "260826P00699000",
			ShortStrike: 700, LongStrike: 699, Price: 710, OutOfTheMoney: 1.41,
			Credit: 0.20, Risk: 0.80, CreditToRisk: 25, Cost: 0.16, CostShare: 80,
			CreditAfterCost: 0.12, EdgeFrom: screener.FromDelta,
		}
	}

	require.NoError(t, kept.RecordCandidates(ctx, earlier, []screener.Candidate{sweep("QQQ")}))
	require.NoError(t, kept.RecordCandidates(ctx, later, []screener.Candidate{sweep("SPY")}))

	found, takenAt, err := kept.Candidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, found, 1, "only the newest sweep is offered")
	assert.Equal(t, "SPY", found[0].Underlying)
	assert.True(t, takenAt.Equal(later))

	// The older sweep is still there. Reading it through the purge is what
	// proves it: a purge that removes nothing would pass on an empty table.
	gone, err := kept.PurgeCandidates(ctx, later)
	require.NoError(t, err)
	assert.Equal(t, int64(1), gone, "the earlier sweep was kept until the purge took it")

	found, _, err = kept.Candidates(ctx, 10)
	require.NoError(t, err)
	require.Len(t, found, 1, "the purge took the old sweep and left the new one")
	assert.Equal(t, "SPY", found[0].Underlying)
}

// Attribution survives the process that made it.
//
// Everything here is written through one keeper and read back through another,
// built on a second pool with nothing shared between them. That is what says the
// answer lives in the database rather than in a field of the harness: the harness
// is not in this test at all, and neither is the pool that did the writing.
func TestWhatCausedAnIntentOutlivesTheProcess(t *testing.T) {
	dsn := dbtest.Fresh(t)
	ctx := context.Background()
	started := time.Date(2026, 9, 3, 18, 20, 0, 0, time.UTC)

	writing, err := db.Open(ctx, dsn)
	require.NoError(t, err)
	wrote, err := NewPostgres(writing, 10)
	require.NoError(t, err)

	require.NoError(t, wrote.TurnStarted(ctx,
		Turn{Ref: "turn-1", ThreadRef: "thread-1", StartedAt: started}, "entry", "trying an entry"))
	require.NoError(t, wrote.AppendIntent(ctx, Intent{
		At: started.Add(time.Minute), TurnRef: "turn-1", Structure: "before", Thesis: "t", MaxLoss: "m",
	}))
	defended, err := wrote.AppendTurnCause(ctx, TurnCause{
		TurnRef: "turn-1", At: started.Add(2 * time.Minute),
		WokenBy: "defend", Cause: "checking the defence rules",
	})
	require.NoError(t, err)
	require.NoError(t, wrote.AppendIntent(ctx, Intent{
		At: started.Add(3 * time.Minute), TurnRef: "turn-1", Structure: "after", Thesis: "t", MaxLoss: "m",
	}))
	// The writer is gone before anything is read, which is the point.
	writing.Close()

	reading, err := db.Open(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(reading.Close)
	read, err := NewPostgres(reading, 10)
	require.NoError(t, err)

	state, err := read.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Intents, 2)
	require.Len(t, state.Causes, 2)

	byStructure := map[string]int64{}
	for _, intent := range state.Intents {
		require.NotNil(t, intent.CauseID, intent.Structure)
		byStructure[intent.Structure] = *intent.CauseID
	}
	assert.Equal(t, state.Causes[0].ID, byStructure["before"], "written before the steer")
	assert.Equal(t, defended, byStructure["after"], "written after the steer")

	causes, err := read.CausesOfTurn(ctx, "turn-1")
	require.NoError(t, err)
	require.Len(t, causes, 2)
	assert.Equal(t, []string{"entry", "defend"}, []string{causes[0].WokenBy, causes[1].WokenBy},
		"oldest first, by id and never by time")
}
