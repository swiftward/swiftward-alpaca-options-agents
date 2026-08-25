package execution

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

type brokerDouble struct {
	mu        sync.Mutex
	orders    []marketdata.Order
	quotes    map[string]marketdata.Quote
	replaced  map[string]float64
	names     map[string]string
	cancelled []string
}

func (b *brokerDouble) Orders(context.Context, int) ([]marketdata.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]marketdata.Order(nil), b.orders...), nil
}

func (b *brokerDouble) Quotes(_ context.Context, symbols []string) (map[string]marketdata.Quote, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	answer := map[string]marketdata.Quote{}
	for _, symbol := range symbols {
		if quote, known := b.quotes[symbol]; known {
			answer[symbol] = quote
		}
	}
	return answer, nil
}

func (b *brokerDouble) ReplaceOrder(_ context.Context, id string, limit float64, name string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.replaced == nil {
		b.replaced = map[string]float64{}
	}
	if b.names == nil {
		b.names = map[string]string{}
	}
	b.replaced[id] = limit
	b.names[id] = name
	return nil
}

func (b *brokerDouble) CancelOrder(_ context.Context, id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelled = append(b.cancelled, id)
	return nil
}

func (b *brokerDouble) seen() (map[string]float64, []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.replaced, append([]string(nil), b.cancelled...)
}

func quote(bid, ask float64) marketdata.Quote {
	return marketdata.Quote{Bid: bid, Ask: ask}
}

// A credit spread: one leg sold, one bought, sent as a single order. The credit
// is a negative limit price, the way the broker states it.
func spread(id string, limit float64, status string, submitted time.Time) marketdata.Order {
	return marketdata.Order{
		ID: id, Class: "mleg", Status: status, LimitPrice: limit, SubmittedAt: &submitted,
		// Every structure this project sends names the worst price it accepts; a
		// wide one here keeps these cases about the walk itself.
		ClientID: NameFor(-0.01),
		Legs: []marketdata.Order{
			{Symbol: "QQQ260826P00701000", Side: "sell", Quantity: 1},
			{Symbol: "QQQ260826P00700000", Side: "buy", Quantity: 1},
		},
	}
}

func ladder(broker Broker, now time.Time, t *testing.T) *Ladder {
	return &Ladder{
		Broker: broker, Every: time.Minute, Step: 0.01, Patience: 10 * time.Minute,
		Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}
}

func TestAnUnfilledOrderWalksOneStepTowardTheBook(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, cancelled)
	// The book shows 0.71 for the leg we sell and asks 0.65 for the one we buy:
	// a credit of six cents. From twelve, one step is eleven.
	assert.InDelta(t, -0.11, replaced["o-1"], 1e-9)
}

// The book's own price is the worst the ladder ever asks for. Walking past it
// would pay the spread twice and buy nothing.
func TestTheWalkStopsAtWhatTheBookShows(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.065, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.06, replaced["o-1"], 1e-9)

	// Standing at the book's price already, it does not move again.
	broker.orders = []marketdata.Order{spread("o-2", -0.06, "new", at.Add(-2*time.Minute))}
	ladder(broker, at, t).step(context.Background())
	replaced, _ = broker.seen()
	_, moved := replaced["o-2"]
	assert.False(t, moved, "an order already at the book's price has nowhere to walk")
}

func TestAnOrderTheBookWillNotTakeIsCancelled(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-11*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Equal(t, []string{"o-1"}, cancelled)
	assert.Empty(t, replaced, "a cancelled order is not also re-priced")
}

func TestAFreshOrderIsLeftAlone(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-20*time.Second))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced)
	assert.Empty(t, cancelled)
}

// Everything that is not one of our waiting structures is none of the ladder's
// business: a filled order, a cancelled one, and any single-leg order, which
// this project never sends for an entry.
func TestOnlyOurWaitingStructuresAreTouched(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	old := at.Add(-2 * time.Minute)
	single := marketdata.Order{
		ID: "o-single", Class: "", Status: "new", LimitPrice: 1.42, SubmittedAt: &old,
	}
	broker := &brokerDouble{
		orders: []marketdata.Order{
			spread("o-filled", -0.12, "filled", old),
			spread("o-cancelled", -0.12, "canceled", old),
			single,
		},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced)
	assert.Empty(t, cancelled)
}

// A leg the broker will not price cannot be walked toward anything. Standing
// still is the honest answer; patience ends the order later.
func TestAnOrderWithoutTwoSidedQuotesStandsStill(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced)
	assert.Empty(t, cancelled)
}

func TestShowingPricesEveryLegAtWhatTheBookWouldPay(t *testing.T) {
	at := time.Now()
	order := spread("o-1", -0.12, "new", at)
	order.Legs[0].Quantity = 2

	showing, known := Showing(order, map[string]marketdata.Quote{
		"QQQ260826P00701000": quote(0.71, 0.76),
		"QQQ260826P00700000": quote(0.61, 0.65),
	})

	require.True(t, known)
	// Two sold at 0.71 pays 1.42; one bought at 0.65 costs 0.65; a credit of 0.77.
	assert.InDelta(t, -0.77, showing, 1e-9)
}

func TestTowardMovesOneStepAndNeverPasses(t *testing.T) {
	assert.InDelta(t, -0.11, Toward(-0.12, -0.06, 0.01), 1e-9)
	assert.InDelta(t, -0.06, Toward(-0.065, -0.06, 0.01), 1e-9)
	assert.InDelta(t, -0.06, Toward(-0.06, -0.06, 0.01), 1e-9)
	// A limit already better for us than the book stays put rather than walking
	// backwards into a worse price.
	assert.InDelta(t, -0.07, Toward(-0.06, -0.08, 0.01), 1e-9)
}

func TestALadderWithoutItsSettingsRefusesToRun(t *testing.T) {
	for _, missing := range []*Ladder{
		{Broker: &brokerDouble{}, Step: 0.01, Patience: time.Minute, Now: time.Now},
		{Broker: &brokerDouble{}, Every: time.Minute, Patience: time.Minute, Now: time.Now},
		{Broker: &brokerDouble{}, Every: time.Minute, Step: 0.01, Now: time.Now},
	} {
		missing.Log = zaptest.NewLogger(t)
		require.Error(t, missing.Run(context.Background()), fmt.Sprintf("%+v", missing))
	}
}

// The book is not a bound on what we accept. When it moves away from us, the
// ladder stops at the price the session named and waits there - otherwise a book
// drifting all afternoon takes the whole credit one tick at a time.
func TestTheWalkStopsAtThePriceTheSessionNamed(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.13, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.11)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			// The book would only pay two cents for this structure now.
			"QQQ260826P00701000": quote(0.62, 0.70),
			"QQQ260826P00700000": quote(0.55, 0.60),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.12, replaced["o-1"], 1e-9, "one step toward the floor, not toward the book")

	// Standing on the floor it does not move again, however far the book drifts.
	order.LimitPrice = -0.11
	broker.orders = []marketdata.Order{order}
	broker.replaced = nil
	ladder(broker, at, t).step(context.Background())
	replaced, _ = broker.seen()
	assert.Empty(t, replaced, "the session's worst price is where the walk ends")
}

// A book standing closer than the floor is a gift: stop there and keep the rest.
func TestAGenerousBookIsTakenRatherThanTheFloor(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.13, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.05)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.12, replaced["o-1"], 1e-9)

	// The book shows a credit of six cents, so the walk ends there and the four
	// cents the session was willing to give up are not given up.
	order.LimitPrice = -0.06
	broker.orders = []marketdata.Order{order}
	broker.replaced = nil
	ladder(broker, at, t).step(context.Background())
	replaced, _ = broker.seen()
	assert.Empty(t, replaced)
}

// An order that names no worst price is left at the price it was placed at.
// Inventing that number here is what a share-of-the-credit rule would do, and
// recomputed after each move it ratchets: the whole credit goes, a cent at a
// time, and every step looks reasonable.
func TestAnOrderNamingNoPriceIsNotWalked(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.11, "new", at.Add(-2*time.Minute))
	order.ClientID = "entry-without-a-price"
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.62, 0.70),
			"QQQ260826P00700000": quote(0.55, 0.60),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, replaced)
	assert.Empty(t, cancelled, "patience still ends it later; this run simply leaves it alone")
}

func TestTheNamedPriceIsReadFromTheOrdersName(t *testing.T) {
	order := marketdata.Order{ClientID: NameFor(-0.11)}
	worst, named := Reservation(order)
	require.True(t, named)
	assert.InDelta(t, -0.11, worst, 1e-9)

	// A name carrying other things as well still yields the price.
	worst, named = Reservation(marketdata.Order{ClientID: "entry-1; " + NameFor(-0.09) + "; qqq"})
	require.True(t, named)
	assert.InDelta(t, -0.09, worst, 1e-9)

	for _, name := range []string{"", "entry-1", "worst=", "worst=abc"} {
		_, named := Reservation(marketdata.Order{ClientID: name})
		assert.False(t, named, "a name without a readable price names nothing: %q", name)
	}
}

// The broker replaces an order by making a new one, and names the new one itself
// unless told otherwise. Measured on the account: the session's floor, written
// into the order's name, was gone after the first move - and with it the only
// bound the ladder obeys. The name is carried forward.
func TestTheNameTravelsWithEveryReplacement(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.13, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.09)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	broker.mu.Lock()
	defer broker.mu.Unlock()
	carried := broker.names["o-1"]
	assert.Contains(t, carried, NameFor(-0.09),
		"the replacement keeps the floor, or it is lost at the first step")
	assert.NotEqual(t, NameFor(-0.09), carried,
		"and it is not the same name: the broker refuses a name it has already seen")

	floor, named := Reservation(marketdata.Order{ClientID: carried})
	require.True(t, named, "the floor must still be readable from the name the replacement carries")
	assert.InDelta(t, -0.09, floor, 1e-9)
}
