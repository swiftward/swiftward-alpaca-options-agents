package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// The book stands far from any of these orders, so the only thing that can move
// one is the walk toward the floor - which is what each case below is about.
func awayFromTheBook(order marketdata.Order) *brokerDouble {
	return &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.62, 0.70),
			"QQQ260826P00700000": quote(0.55, 0.60),
		},
	}
}

func holdingTo(least float64, broker Broker, now time.Time, t *testing.T) *Ladder {
	l := ladder(broker, now, t)
	l.MinEdgePoints = func() (float64, error) { return least, nil }

	return l
}

// Measured on the account, 1 September 2026: a session entered on "edge at least
// +3" and named a worst price whose edge was +2.53. The ladder walked to it in
// forty-five seconds and nothing but a distant book stopped the fill.
func TestAWorstPriceBelowTheEntryRuleIsNotWalkedTo(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameStating(-0.28, 2.53)
	broker := awayFromTheBook(order)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced, "the concession is refused")
	assert.Empty(t, cancelled, "the price it was placed at cleared the rule, so the order stays")
}

// The same order, the same ladder, one number different. Without this the test
// above would pass on a ladder that walks nothing at all.
func TestAWorstPriceThatClearsTheEntryRuleIsWalkedTo(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameStating(-0.28, 3.62)
	broker := awayFromTheBook(order)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.29, replaced["o-1"], 1e-9, "one step toward the floor")
}

// An order carrying no edge is the session making no claim, and a claim nobody
// made is not one this can judge. Refusing those would stop an account trading
// over a missing word, so they walk as they did before the gate existed.
func TestAnOrderThatStatesNoEdgeStillWalks(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.28)
	broker := awayFromTheBook(order)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.29, replaced["o-1"], 1e-9)
}

// Losing the number is a reason to speak, never a reason to strand an order the
// session placed within the rules it could read at the time.
func TestAnUnreadableEdgeLeavesTheWalkAlone(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameStating(-0.28, 2.53)
	broker := awayFromTheBook(order)

	l := ladder(broker, at, t)
	l.MinEdgePoints = func() (float64, error) { return 0, errors.New("the declaration names no min_edge_points") }
	l.step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.29, replaced["o-1"], 1e-9)
}

// A replacement that kept the floor and dropped the edge would clear the gate on
// its first step and every step after it, which is the whole gate gone in one
// walk. The name a replacement gets carries both.
func TestAReplacementCarriesTheEdgeAndNotOnlyTheFloor(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameStating(-0.28, 3.62)
	broker := awayFromTheBook(order)

	holdingTo(3, broker, at, t).step(context.Background())

	broker.mu.Lock()
	defer broker.mu.Unlock()
	carried := marketdata.Order{ClientID: broker.names["o-1"]}
	floor, stated := Reservation(carried)
	require.True(t, stated)
	assert.InDelta(t, -0.28, floor, 1e-9)
	edge, stated := EdgeAt(carried)
	require.True(t, stated, "the edge survives the replacement")
	assert.InDelta(t, 3.62, edge, 1e-9)
}

// The session writes this name itself, so it is read out of its own text rather
// than out of a format of ours.
func TestTheEdgeIsReadOutOfWhateverTheSessionWrote(t *testing.T) {
	for _, name := range []string{
		"worst=-0.28;edge=3.62",
		"entry-1; worst=-0.28; edge=3.62; SPY772-773",
		"edge=3.62,worst=-0.28",
	} {
		edge, stated := EdgeAt(marketdata.Order{ClientID: name})
		require.True(t, stated, name)
		assert.InDelta(t, 3.62, edge, 1e-9, name)
	}
	for _, name := range []string{"worst=-0.28", "edge=", "edge=soon", ""} {
		_, stated := EdgeAt(marketdata.Order{ClientID: name})
		assert.False(t, stated, name)
	}
}
