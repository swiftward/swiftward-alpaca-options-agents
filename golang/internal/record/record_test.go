package record

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page reading null cannot tell "nothing happened yet" from "this field does
// not work", so an empty state serializes as empty lists.
func TestEmptyStateSerializesAsLists(t *testing.T) {
	state, err := NewMemory().Read(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	assert.JSONEq(t, `{"turns":[],"causes":[],"calls":[],"steps":[],"intents":[],"said":[]}`, string(raw))
}

func TestReadReturnsACopy(t *testing.T) {
	m := NewMemory()
	require.NoError(t, m.AppendIntent(context.Background(), Intent{At: time.Unix(0, 0).UTC()}))

	first, err := m.Read(context.Background())
	require.NoError(t, err)
	first.Intents[0].Thesis = "changed by the caller"

	again, err := m.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "", again.Intents[0].Thesis)
}

// An intent belongs to what the turn was told last, not to what opened it.
//
// This is the whole point of the shape. A turn is woken by one session and then
// steered by others while it runs; before the causes were their own rows, every
// intent carried the opener and the record answered "who defended this position"
// confidently and wrongly.
func TestAnIntentTakesTheCauseInForceAndNotTheOpener(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

	require.NoError(t, m.TurnStarted(ctx, Turn{Ref: "t1", StartedAt: at}, "entry", "trying an entry"))
	require.NoError(t, m.AppendIntent(ctx, Intent{At: at.Add(time.Minute), TurnRef: "t1", Structure: "before"}))

	defended, err := m.AppendTurnCause(ctx, TurnCause{
		TurnRef: "t1", At: at.Add(2 * time.Minute), WokenBy: "defend", Cause: "checking the defence rules",
	})
	require.NoError(t, err)
	require.NoError(t, m.AppendIntent(ctx, Intent{At: at.Add(3 * time.Minute), TurnRef: "t1", Structure: "after"}))

	state, err := m.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Intents, 2)
	require.Len(t, state.Causes, 2)

	opener := state.Causes[0].ID
	require.NotNil(t, state.Intents[0].CauseID)
	assert.Equal(t, opener, *state.Intents[0].CauseID, "written before the steer, so it answers the opener")
	require.NotNil(t, state.Intents[1].CauseID)
	assert.Equal(t, defended, *state.Intents[1].CauseID, "written after the steer, so it answers the steer")
}

// Ten causes, and each intent takes the one in force at its own moment rather
// than the last one of the whole turn.
//
// Ten because two is where a wrong design still looks right: keeping "the last
// cause" in a field of the process passes with one steer and files everything
// under the tenth with ten. Nothing here touches a clock - the ids carry the
// order, which is why this test cannot flake.
func TestTenCausesAndEachIntentTakesItsOwn(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

	require.NoError(t, m.TurnStarted(ctx, Turn{Ref: "t1", StartedAt: at}, "entry", "trying an entry"))

	want := map[string]int64{}
	for i := range 10 {
		name := fmt.Sprintf("cause-%d", i)
		id, err := m.AppendTurnCause(ctx, TurnCause{TurnRef: "t1", At: at, WokenBy: name, Cause: name})
		require.NoError(t, err)
		structure := fmt.Sprintf("structure-%d", i)
		require.NoError(t, m.AppendIntent(ctx, Intent{At: at, TurnRef: "t1", Structure: structure}))
		want[structure] = id
	}

	state, err := m.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Intents, 10)
	for _, intent := range state.Intents {
		require.NotNil(t, intent.CauseID, intent.Structure)
		assert.Equal(t, want[intent.Structure], *intent.CauseID, intent.Structure)
	}
}

// Every cause shares one instant here, which is what the schedule actually does:
// the clock ticks once a minute, so a window every ten minutes and one every
// fifteen carry the same stamp when they meet. Ordering by time would answer
// differently on different reads; ordering by id cannot.
func TestCausesInOneInstantStillHaveAnOrder(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

	require.NoError(t, m.TurnStarted(ctx, Turn{Ref: "t1", StartedAt: at}, "entry", "trying an entry"))
	second, err := m.AppendTurnCause(ctx, TurnCause{TurnRef: "t1", At: at, WokenBy: "defend", Cause: "defence"})
	require.NoError(t, err)
	require.NoError(t, m.AppendIntent(ctx, Intent{At: at, TurnRef: "t1"}))

	state, err := m.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Intents, 1)
	require.NotNil(t, state.Intents[0].CauseID)
	assert.Equal(t, second, *state.Intents[0].CauseID)
}
