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

func quoteWithDelta(bid, ask, delta float64) marketdata.Quote {
	q := quote(bid, ask)
	q.Delta = &delta

	return q
}

// An opening credit spread whose short leg carries a delta, with the book far
// enough away that the only thing which can move the order is the walk toward its
// floor - which is what every case below is about.
func openingSpread(id string, limit, floor, delta float64, at time.Time) (marketdata.Order, *brokerDouble) {
	order := spread(id, limit, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(floor)
	for i := range order.Legs {
		order.Legs[i].PositionIntent = "sell_to_open"
	}

	return order, &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quoteWithDelta(0.62, 0.70, delta),
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
// forty-five seconds and nothing but a distant book stopped the fill. The strikes
// here are a dollar apart, so 28 cents of credit against a delta of 0.2547 is
// exactly that number.
func TestAWorstPriceBelowTheEntryRuleIsNotWalkedTo(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	_, broker := openingSpread("o-1", -0.30, -0.28, 0.2547, at)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced, "the concession is refused")
	assert.Empty(t, cancelled, "the price it was placed at cleared the rule, so the order stays")
}

// The same order, the same ladder, one delta different. Without this the test
// above would pass on a ladder that walks nothing at all.
func TestAWorstPriceThatClearsTheEntryRuleIsWalkedTo(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	_, broker := openingSpread("o-1", -0.30, -0.28, 0.2438, at)

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.29, replaced["o-1"], 1e-9, "at 0.28 against delta 0.2438 the edge is +3.62")
}

// An exit is never held to an entry rule, and neither is an order whose legs do
// not say what they do. A rule that can cost a fill judges nothing it is unsure
// of - the opposite of the size check, which judges everything it is unsure of.
func TestOnlyAnOrderThatPlainlyOpensIsHeldToTheEntryRule(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	for _, intent := range []string{"sell_to_close", ""} {
		order, broker := openingSpread("o-1", -0.30, -0.28, 0.2547, at)
		for i := range order.Legs {
			order.Legs[i].PositionIntent = intent
		}
		broker.orders = []marketdata.Order{order}

		holdingTo(3, broker, at, t).step(context.Background())

		replaced, _ := broker.seen()
		assert.NotEmpty(t, replaced, "intent %q must not strand the order", intent)
	}
}

// A short leg the broker gave no delta for cannot be measured, and an unmeasured
// structure is not a refused one.
func TestAStructureWithNoDeltaIsNotJudged(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order, broker := openingSpread("o-1", -0.30, -0.28, 0.2547, at)
	broker.quotes["QQQ260826P00701000"] = quote(0.62, 0.70)
	broker.orders = []marketdata.Order{order}

	holdingTo(3, broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.NotEmpty(t, replaced)
}

// Losing the declared number is a reason to speak, never a reason to strand an
// order the session placed within the rules it could read at the time.
func TestAnUnreadableMinimumLeavesTheWalkAlone(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	_, broker := openingSpread("o-1", -0.30, -0.28, 0.2547, at)

	l := ladder(broker, at, t)
	l.MinEdgePoints = func() (float64, error) { return 0, errors.New("the declaration names no min_edge_points") }
	l.step(context.Background())

	replaced, _ := broker.seen()
	assert.NotEmpty(t, replaced)
}

// The measure is the screener's, so a session and the ladder cannot mean
// different things by the same word. A shape it is not defined for gets no
// answer rather than a wrong one.
func TestTheEdgeIsCountedInPointsOfTheWidth(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order, broker := openingSpread("o-1", -0.30, -0.28, 0.2547, at)

	edge, measurable := EdgeAt(order, -0.28, broker.quotes)
	require.True(t, measurable)
	assert.InDelta(t, 2.53, edge, 1e-6)

	// A debit is not a credit spread's floor, and there is nothing to measure.
	_, measurable = EdgeAt(order, 0.28, broker.quotes)
	assert.False(t, measurable)

	// A backspread's risk is not the width between two strikes.
	order.Legs[1].Ratio = 2
	_, measurable = EdgeAt(order, -0.28, broker.quotes)
	assert.False(t, measurable, "two bought against one sold is a different shape")
}

// The turn in the name is the only thing joining a filled order back to the
// intent behind it. A replacement rebuilt from the fields this package knows
// dropped it at the first step, and every step after kept it dropped.
func TestAReplacementKeepsEverythingTheSessionWrote(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	order := marketdata.Order{ClientID: "worst=-0.28;turn=tu-7;SPY772-773-0909"}

	first := NameCarrying(order, at)
	assert.Contains(t, first, "turn=tu-7")
	assert.Contains(t, first, "SPY772-773-0909")
	assert.NotEqual(t, order.ClientID, first, "the broker refuses a name it has seen")

	// And it does not grow: a walk of twenty steps must not carry twenty stamps.
	second := NameCarrying(marketdata.Order{ClientID: first}, at.Add(time.Second))
	assert.Len(t, strings.Split(second, ";"), len(strings.Split(first, ";")))
	assert.NotEqual(t, first, second)

	floor, named := Reservation(marketdata.Order{ClientID: second})
	require.True(t, named)
	assert.InDelta(t, -0.28, floor, 1e-9)
}

// A session's own trailing field can be a date. Only the stamp this package added
// is dropped, and only one of them.
func TestOnlyTheStampThisPackageAddedIsDropped(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	carried := NameCarrying(marketdata.Order{ClientID: "worst=-0.28;turn=tu-7;20260904"}, at)
	assert.Contains(t, carried, "turn=tu-7", "the turn survives")
	assert.NotContains(t, carried, "20260904;", "the one bare number at the end is taken as the stamp")
}

// A name already at the broker's limit becomes too long when a stamp is added,
// and a refused replacement reads as an order that will not walk. What survives
// the truncation is the floor: nothing else is read, and an order without one
// stops walking altogether.
func TestALongNameIsCutBackToTheFloorRatherThanRefused(t *testing.T) {
	at := time.Date(2026, 9, 1, 14, 46, 0, 0, time.UTC)
	long := "worst=-0.28;turn=tu-7;" + strings.Repeat("x", nameLimit)

	carried := NameCarrying(marketdata.Order{ClientID: long}, at)
	assert.LessOrEqual(t, len(carried), nameLimit)

	floor, named := Reservation(marketdata.Order{ClientID: carried})
	require.True(t, named, "the floor is what must survive")
	assert.InDelta(t, -0.28, floor, 1e-9)
}

// The name is the session's own text, so a field is matched whole. A search for
// the prefix anywhere in the string reads `min_worst=` as the floor.
func TestAFieldIsMatchedWholeAndNotFoundInsideAnother(t *testing.T) {
	floor, named := Reservation(marketdata.Order{ClientID: "min_worst=9;worst=-0.28;turn=tu-7"})
	require.True(t, named)
	assert.InDelta(t, -0.28, floor, 1e-9)

	for _, name := range []string{"worst=NaN", "worst=+Inf", "worst=soon", ""} {
		_, named := Reservation(marketdata.Order{ClientID: name})
		assert.False(t, named, name)
	}
}
