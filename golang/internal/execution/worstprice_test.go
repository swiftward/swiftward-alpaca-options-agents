package execution

import (
	"context"
	"errors"
	"strings"
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

// An exit is not held to an entry rule. A close has no edge to measure - it is
// leaving a position, not taking one - and a session that writes any edge on one
// would otherwise have its exit stranded here and cancelled by patience.
func TestAClosingOrderIsNotHeldToTheEntryRule(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	order.ClientID = NameStating(-0.28, 0.00)
	for i := range order.Legs {
		order.Legs[i].PositionIntent = "sell_to_close"
	}
	broker := awayFromTheBook(order)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.NotEmpty(t, replaced, "the exit walks whatever its edge says")
}

// The name is the session's own text, so a field is matched whole. A search for
// the prefix anywhere in the string reads `min_edge=3` as the edge and walks to a
// floor this exists to refuse.
func TestAFieldIsMatchedWholeAndNotFoundInsideAnother(t *testing.T) {
	edge, stated := EdgeAt(marketdata.Order{ClientID: "worst=-0.28;min_edge=3;edge=2.53;turn=tu-7"})
	require.True(t, stated)
	assert.InDelta(t, 2.53, edge, 1e-9, "the field named edge, not the one ending in it")

	_, stated = EdgeAt(marketdata.Order{ClientID: "worst=-0.28;min_edge=3;turn=tu-7"})
	assert.False(t, stated, "a name with no edge field states no edge")
}

// "NaN" and "Inf" parse. A NaN then compares false against every bound, so it
// would not pass a check - it would fail all of them, and strand the order until
// patience over a word in its own name.
func TestANumberThatIsNotFiniteIsNoNumber(t *testing.T) {
	for _, name := range []string{"worst=-0.28;edge=NaN", "worst=-0.28;edge=+Inf", "worst=-0.28;edge=-Inf"} {
		_, stated := EdgeAt(marketdata.Order{ClientID: name})
		assert.False(t, stated, name)
	}
	for _, name := range []string{"worst=NaN;edge=3.62", "worst=Inf"} {
		_, named := Reservation(marketdata.Order{ClientID: name})
		assert.False(t, named, name)
	}
}

// The turn in the name is the only thing joining a filled order back to the
// intent behind it. A replacement rebuilt from the fields this package knows
// dropped it at the first step, and every step after kept it dropped.
func TestAReplacementKeepsEverythingTheSessionWrote(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := marketdata.Order{ClientID: "worst=-0.28;edge=3.62;turn=tu-7;SPY772-773-0909"}

	first := NameCarrying(order, at)
	assert.Contains(t, first, "turn=tu-7")
	assert.Contains(t, first, "SPY772-773-0909")
	assert.NotEqual(t, order.ClientID, first, "the broker refuses a name it has seen")

	// And it does not grow: a walk of twenty steps must not carry twenty stamps
	// past what the broker will hold.
	second := NameCarrying(marketdata.Order{ClientID: first}, at.Add(time.Second))
	assert.Len(t, strings.Split(second, ";"), len(strings.Split(first, ";")))
	assert.NotEqual(t, first, second)

	floor, named := Reservation(marketdata.Order{ClientID: second})
	require.True(t, named)
	assert.InDelta(t, -0.28, floor, 1e-9)
	edge, stated := EdgeAt(marketdata.Order{ClientID: second})
	require.True(t, stated)
	assert.InDelta(t, 3.62, edge, 1e-9)
}
