package execution

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

type brokerDouble struct {
	mu        sync.Mutex
	orders    []marketdata.Order
	quotes    map[string]marketdata.Quote
	replaced  map[string]float64
	names     map[string]string
	cancelled []string
	positions []marketdata.Position
	// replacements counts how many ids this double has minted, so each one differs.
	replacements int
	// now is the clock the replacement's submission time is stamped from. Nil
	// falls back to the wall clock, which is right for a test that runs one pass.
	now func() time.Time
}

func (b *brokerDouble) at() time.Time {
	if b.now != nil {
		return b.now()
	}

	return time.Now()
}

func (b *brokerDouble) Positions(context.Context) ([]marketdata.Position, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]marketdata.Position(nil), b.positions...), nil
}

// The broker answers with the NEWEST orders up to the limit it is given, so a
// working order can be missing from a pass. A double that ignored the limit
// could not show what the ladder does when one is.
func (b *brokerDouble) Orders(_ context.Context, reads int) ([]marketdata.Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	shown := append([]marketdata.Order(nil), b.orders...)
	if reads > 0 && len(shown) > reads {
		shown = shown[len(shown)-reads:]
	}

	return shown, nil
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

// The broker gives a replacement a NEW id and keeps the old one only as history.
// The double does the same: one that answered with the id it was given would let
// a ladder that loses the link still pass every test here.
func (b *brokerDouble) ReplaceOrder(_ context.Context, id string, limit float64, name string) (string, error) {
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
	b.replacements++
	fresh := fmt.Sprintf("%s-r%d", id, b.replacements)

	// The replacement is a NEW order and the broker stamps it with a new
	// submission time - verified against Alpaca on 31 August, where the interval
	// between one order's steps came out at 90 seconds against a configured 45.
	// A double that left the old order in place could not show that at all: the
	// ladder's own freshness check reads exactly this field.
	for i := range b.orders {
		if b.orders[i].ID != id {
			continue
		}
		submitted := b.at()
		b.orders[i].ID = fresh
		b.orders[i].LimitPrice = limit
		b.orders[i].ClientID = name
		b.orders[i].SubmittedAt = &submitted
	}

	return fresh, nil
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

// A book standing better than our own limit is left alone. Walking toward it
// means asking for more credit than we already asked for, on an order that
// should have filled - which is how three orders spent an afternoon walking from
// -0.60 to -0.65 and never filling.
func TestABookBetterThanOurLimitMovesNothing(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.60, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.36)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(2.60, 2.70),
			"QQQ260826P00700000": quote(0.05, 0.10),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.Empty(t, replaced, "the book pays 2.50 against our 0.60: there is nothing to concede")
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

// The session hears about an order that never happened, because only the session
// can decide what to do instead. It does NOT hear about a fill: a fill is what it
// already planned for, and a turn spent acknowledging one changes nothing.
func TestACancelledOrderTellsTheSession(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-11*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	var told []string
	rungs := ladder(broker, at, t)
	rungs.Wake = func(_ context.Context, cause string) { told = append(told, cause) }
	rungs.step(context.Background())

	require.Len(t, told, 1)
	assert.Contains(t, told[0], "o-1")
	assert.Contains(t, told[0], "QQQ260826P00701000", "the session is told the structure, not a count")
}

func TestSeveralOrdersDyingTogetherAreOneTelling(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	first := spread("o-1", -0.12, "new", at.Add(-11*time.Minute))
	second := spread("o-2", -0.20, "new", at.Add(-12*time.Minute))
	broker := &brokerDouble{
		orders: []marketdata.Order{first, second},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	var told []string
	rungs := ladder(broker, at, t)
	rungs.Wake = func(_ context.Context, cause string) { told = append(told, cause) }
	rungs.step(context.Background())

	require.Len(t, told, 1, "two orders dying in one pass is one situation, not two")
	assert.Contains(t, told[0], "o-1")
	assert.Contains(t, told[0], "o-2")
}

// A pass that cancels nothing says nothing. Without this the session would be
// woken every forty-five seconds by a ladder that did its job quietly.
func TestAPassThatCancelsNothingTellsNobody(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.35, "new", at.Add(-2*time.Minute))
	order.ClientID = NameFor(-0.25)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	told := 0
	rungs := ladder(broker, at, t)
	rungs.Wake = func(context.Context, string) { told++ }
	rungs.step(context.Background())

	replaced, _ := broker.seen()
	assert.NotEmpty(t, replaced, "this pass walked an order, so it did do something")
	assert.Zero(t, told)
}

// Part of an order can fill before the rest is cancelled. Telling the session it
// did not fill would send it to re-open a position it already holds.
func TestAPartlyFilledOrderIsNotCalledUnfilled(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.12, "partially_filled", at.Add(-11*time.Minute))
	order.Quantity = 50
	order.FilledQuantity = 20
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	var told []string
	rungs := ladder(broker, at, t)
	rungs.Wake = func(_ context.Context, cause string) { told = append(told, cause) }
	rungs.step(context.Background())

	require.Len(t, told, 1)
	assert.Contains(t, told[0], "20 of 50")
	assert.NotContains(t, told[0], "did not fill at all")
}

// A fill reaches the room once, however many times the ladder meets it. It polls
// every forty-five seconds and forgets everything it holds when the process
// restarts, so the record is what makes a fill new - not this process's memory.
func TestAFillIsSaidOnceHoweverOftenItIsSeen(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "filled", at.Add(-2*time.Minute))
	order.Quantity, order.FilledQuantity, order.FilledPrice = 50, 50, -0.28
	filled := at.Add(-time.Minute)
	order.FilledAt = &filled
	broker := &brokerDouble{orders: []marketdata.Order{order}}

	var said []string
	kept := record.NewMemory()
	for range 3 {
		rungs := ladder(broker, at, t)
		rungs.Record = kept
		rungs.watching = at.Add(-time.Hour)
		rungs.Say = func(_ context.Context, line string) { said = append(said, line) }
		rungs.step(context.Background())
	}

	require.Len(t, said, 1, "three passes over one filled order is one line")
	assert.Equal(t, "✔ QQQ 701/700 put ×50, credit 0.28", said[0])

	state, err := kept.Read(context.Background())
	require.NoError(t, err)
	fills := 0
	for _, step := range state.Steps {
		if step.Action == "filled" {
			fills++
			require.NotNil(t, step.Became)
			assert.InDelta(t, -0.28, *step.Became, 1e-9, "the record keeps the price it filled at")
			require.NotNil(t, step.Quantity, "and how many contracts, or the price is not money")
			assert.InDelta(t, 50, *step.Quantity, 1e-9)
		}
	}
	assert.Equal(t, 1, fills)
}

// Closing a position costs money, and the line has to say so: the same number
// with the wrong word turns a loss into a gain on the screen.
func TestAFillThatCostMoneyIsCalledADebit(t *testing.T) {
	at := time.Date(2026, 8, 25, 15, 30, 0, 0, time.UTC)
	order := spread("o-1", 0.09, "filled", at.Add(-2*time.Minute))
	order.Quantity, order.FilledQuantity, order.FilledPrice = 1, 1, 0.07
	filled := at.Add(-time.Minute)
	order.FilledAt = &filled
	broker := &brokerDouble{orders: []marketdata.Order{order}}

	var said []string
	rungs := ladder(broker, at, t)
	rungs.Record = record.NewMemory()
	rungs.watching = at.Add(-time.Hour)
	rungs.Say = func(_ context.Context, line string) { said = append(said, line) }
	rungs.step(context.Background())

	require.Len(t, said, 1)
	assert.Contains(t, said[0], "debit 0.07")
}

// A fill from before this ladder started looking is written down and NOT said.
// Every redeployment would otherwise read the whole day back into the room as
// news, which is what one did on 25 August: eighteen lines at once, none of them
// newer than six hours.
func TestAFillOlderThanTheLadderIsKeptButNotSaid(t *testing.T) {
	at := time.Date(2026, 8, 25, 20, 12, 0, 0, time.UTC)
	earlier := at.Add(-6 * time.Hour)
	order := spread("o-1", -0.30, "filled", earlier)
	order.Quantity, order.FilledQuantity, order.FilledPrice = 50, 50, -0.28
	order.FilledAt = &earlier
	broker := &brokerDouble{orders: []marketdata.Order{order}}

	said := 0
	kept := record.NewMemory()
	rungs := ladder(broker, at, t)
	rungs.Record = kept
	rungs.watching = at
	rungs.Say = func(context.Context, string) { said++ }
	rungs.step(context.Background())

	assert.Zero(t, said, "six hours old is not news")

	state, err := kept.Read(context.Background())
	require.NoError(t, err)
	fills := 0
	for _, step := range state.Steps {
		if step.Action == "filled" {
			fills++
		}
	}
	assert.Equal(t, 1, fills, "it is still written down: the record is worth having")
}

// An order that would lose more than one position may is cancelled before it can
// fill, and the session is told to recount.
//
// This is where the declared limit stops being advice. On 26 August a session
// worked its own size out wrong, the broker refused 17 884 sets for want of
// buying power, and the session came back with 906 - a maximum loss near 76 000
// against a limit of 15 000. Nothing between it and the market disagreed,
// because the envelope discloses and does not enforce.
func TestAnOrderTooLargeForOnePositionIsCancelled(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	huge := spread("o-huge", -0.16, "new", at.Add(-2*time.Minute))
	huge.Quantity = 906
	huge.Legs[0].Quantity, huge.Legs[1].Quantity = 906, 906
	// The floor this order declared, which is what it is judged at.
	huge.ClientID = NameFor(-0.16)

	broker := &brokerDouble{
		orders: []marketdata.Order{huge},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	told := make(chan string, 1)
	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 15000, nil }
	l.Wake = func(_ context.Context, cause string) { told <- cause }
	l.step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Equal(t, []string{"o-huge"}, cancelled)
	assert.Empty(t, replaced, "a cancelled order must not also be walked toward the book")

	select {
	case cause := <-told:
		assert.Contains(t, cause, "76", "the session is told what it actually risked")
	default:
		t.Fatal("the session was not told its order was taken away")
	}
}

// A structure left net short calls is cancelled whatever number the ceiling says.
//
// Its loss grows with the price and stops nowhere, so the sampled "worst case" is
// only the loss at the last price anybody sampled: two calls sold against one
// bought priced out as a finite figure that a ceiling could pass. There is no
// ceiling this fits under.
func TestAnOrderWhoseLossHasNoFloorIsCancelled(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	ratio := spread("o-ratio", -0.40, "new", at.Add(-2*time.Minute))
	ratio.Quantity = 1
	ratio.ClientID = NameFor(-0.40)
	ratio.Legs = []marketdata.Order{
		{Symbol: "SPY260904C00650000", Side: "sell", Quantity: 2},
		{Symbol: "SPY260904C00640000", Side: "buy", Quantity: 1},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{ratio},
		quotes: map[string]marketdata.Quote{
			"SPY260904C00650000": quote(0.30, 0.34),
			"SPY260904C00640000": quote(0.60, 0.66),
		},
	}

	told := make(chan string, 1)
	l := ladder(broker, at, t)
	// Deliberately generous: the point is that no ceiling makes this order legal.
	l.Ceiling = func(context.Context) (float64, error) { return 1_000_000, nil }
	l.Wake = func(_ context.Context, cause string) { told <- cause }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-ratio"}, cancelled)

	select {
	case cause := <-told:
		assert.Contains(t, cause, "more calls sold than bought")
	default:
		t.Fatal("the session was not told why its order was taken away")
	}
}

// Our own convexity layer is the same shape the other way up - one call sold
// against two bought - and it must pass: its loss is bounded by construction, and
// it is the hedge for the day the sold premium loses.
func TestABackspreadIsNotMistakenForUnboundedRisk(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	backspread := spread("o-back", -0.10, "new", at.Add(-2*time.Minute))
	backspread.Quantity = 1
	backspread.ClientID = NameFor(-0.10)
	backspread.Legs = []marketdata.Order{
		{Symbol: "SPY260904C00640000", Side: "sell", Quantity: 1},
		{Symbol: "SPY260904C00650000", Side: "buy", Quantity: 2},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{backspread},
		quotes: map[string]marketdata.Quote{
			"SPY260904C00640000": quote(0.60, 0.66),
			"SPY260904C00650000": quote(0.30, 0.34),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 8000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled)
}

// A CLOSING order walks the other way: it pays MORE, not less.
//
// One convention carries both directions - a credit is negative, a debit
// positive, and conceding is always the larger number - so an opening order
// walks down from -0.12 to -0.11 and a close walks up from 0.05 to 0.06. It is
// worth pinning because the sign is where this codebase has been wrong before:
// the profit watch sent its closes negative for two days and none of them
// filled.
func TestAClosingOrderWalksTowardPayingMore(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	closing := spread("o-close", 0.05, "new", at.Add(-2*time.Minute))
	closing.ClientID = NameFor(0.20)
	closing.Legs = []marketdata.Order{
		{Symbol: "QQQ260826P00701000", Side: "buy", Quantity: 1, PositionIntent: "buy_to_close"},
		{Symbol: "QQQ260826P00700000", Side: "sell", Quantity: 1, PositionIntent: "sell_to_close"},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{closing},
		quotes: map[string]marketdata.Quote{
			// Buying the 701 back costs its ask; selling the 700 pays its bid.
			"QQQ260826P00701000": quote(0.10, 0.12),
			"QQQ260826P00700000": quote(0.02, 0.04),
		},
	}

	l := ladder(broker, at, t)
	l.step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, cancelled)
	assert.InDelta(t, 0.06, replaced["o-close"], 1e-9,
		"one cent MORE than it was resting at, because paying more is what fills a close")
}

// A single-set order is judged in full.
//
// The allowance below the per-position check forgives a breach smaller than one
// set, because the session cannot size finer than that. At one set the allowance
// was the whole worst case, so ANY single-set order passed whatever it risked -
// 100 sets of a one-dollar spread would be stopped and one set of a 200-dollar
// spread would not.
func TestASingleSetOrderIsStillJudged(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	wide := marketdata.Order{
		ID: "o-wide", Class: "mleg", Status: "new", Quantity: 1, LimitPrice: -1.00,
		ClientID: NameFor(-1.00), SubmittedAt: &[]time.Time{at.Add(-2 * time.Minute)}[0],
		Legs: []marketdata.Order{
			{Symbol: "QQQ260826P00701000", Side: "sell", Quantity: 1},
			{Symbol: "QQQ260826P00500000", Side: "buy", Quantity: 1},
		},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{wide},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(1.20, 1.30),
			"QQQ260826P00500000": quote(0.10, 0.15),
		},
	}

	l := ladder(broker, at, t)
	// One set of a 201-dollar-wide spread risks 20,000 against a ceiling of 8,000.
	l.Ceiling = func(context.Context) (float64, error) { return 8000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-wide"}, cancelled,
		"one set that breaches cannot be made smaller, so it is not rounding")
}

// A breach of EXACTLY one set is a sizing error, not rounding.
//
// The allowance forgives a breach smaller than one set, because the session
// cannot size finer than that. At exactly one set there is a whole set it could
// have left out - and two sets risking the ceiling each passed a ceiling they
// doubled.
func TestABreachOfAWholeSetIsNotForgiven(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	two := marketdata.Order{
		ID: "o-two", Class: "mleg", Status: "new", Quantity: 2, LimitPrice: -1.00,
		ClientID: NameFor(-1.00), SubmittedAt: &[]time.Time{at.Add(-2 * time.Minute)}[0],
		Legs: []marketdata.Order{
			{Symbol: "QQQ260826P00701000", Side: "sell", Quantity: 2},
			{Symbol: "QQQ260826P00500000", Side: "buy", Quantity: 2},
		},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{two},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(1.20, 1.30),
			"QQQ260826P00500000": quote(0.10, 0.15),
		},
	}

	l := ladder(broker, at, t)
	// Two sets of a 201-dollar-wide spread risk 40,000; one set risks 20,000, so
	// a ceiling of 20,000 is breached by exactly one set.
	l.Ceiling = func(context.Context) (float64, error) { return 20000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-two"}, cancelled)
}

// A CLOSING order is never cancelled for its size, however large the structure
// it gives back.
//
// Both size checks price an order as if it were new exposure, and a closing
// order read that way is judged by the shape it resembles: buying back one sold
// leg and selling two bought ones prices out as a ratio spread short two calls -
// 640,200 dollars of worst case against a ceiling of 8,000. The guard would then
// cancel the one order reducing the account's risk and tell the session to take
// less, which it cannot do: the position is already there.
func TestAClosingOrderIsNotCancelledForItsSize(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	order := spread("o-close", 0.20, "new", at.Add(-2*time.Minute))
	order.Quantity = 10
	order.Legs = []marketdata.Order{
		{Symbol: "SPY260904C00640000", Side: "buy", Quantity: 10, PositionIntent: "buy_to_close"},
		{Symbol: "SPY260904C00650000", Side: "sell", Quantity: 20, PositionIntent: "sell_to_close"},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"SPY260904C00640000": quote(0.30, 0.34),
			"SPY260904C00650000": quote(0.04, 0.06),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 8000, nil }
	l.Book = func(context.Context) (float64, error) { return 80000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled, "the order that gives the position back must survive both checks")
}

// The exemption is for what an order DOES, not for how it looks: the same
// structure opening a position is still judged and still cancelled.
func TestTheSameStructureOpeningIsStillCancelled(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	opening := spread("o-open", 0.20, "new", at.Add(-2*time.Minute))
	opening.Quantity = 10
	opening.ClientID = NameFor(0.20)
	opening.Legs = []marketdata.Order{
		{Symbol: "SPY260904C00640000", Side: "buy", Quantity: 10, PositionIntent: "buy_to_open"},
		{Symbol: "SPY260904C00650000", Side: "sell", Quantity: 20, PositionIntent: "sell_to_open"},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{opening},
		quotes: map[string]marketdata.Quote{
			"SPY260904C00640000": quote(0.30, 0.34),
			"SPY260904C00650000": quote(0.04, 0.06),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 8000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-open"}, cancelled)
}

// A leg that says nothing about what it does is judged, not exempted. Absent is
// not the same as closing, and a guard that reads it as closing would be turned
// off by a broker that stopped sending the field.
func TestAnOrderThatDoesNotSayItClosesIsStillJudged(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	silent := spread("o-silent", 0.20, "new", at.Add(-2*time.Minute))
	silent.Quantity = 10
	silent.ClientID = NameFor(0.20)
	silent.Legs = []marketdata.Order{
		{Symbol: "SPY260904C00640000", Side: "buy", Quantity: 10},
		{Symbol: "SPY260904C00650000", Side: "sell", Quantity: 20, PositionIntent: "sell_to_close"},
	}

	broker := &brokerDouble{
		orders: []marketdata.Order{silent},
		quotes: map[string]marketdata.Quote{
			"SPY260904C00640000": quote(0.30, 0.34),
			"SPY260904C00650000": quote(0.04, 0.06),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 8000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-silent"}, cancelled)
}

// An order within the limit is left to the ladder, and the check costs it
// nothing.
func TestAnOrderWithinTheLimitIsWalkedAsBefore(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 15000, nil }
	l.step(context.Background())

	replaced, cancelled := broker.seen()
	assert.Empty(t, cancelled)
	assert.InDelta(t, -0.11, replaced["o-1"], 1e-9)
}

// A ceiling that cannot be read leaves orders alone rather than cancelling them.
// Losing the limit is a reason to say so, never a reason to take the account's
// working orders away.
func TestAnUnreadableCeilingCancelsNothing(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	huge := spread("o-huge", -0.16, "new", at.Add(-2*time.Minute))
	huge.Quantity = 906
	huge.Legs[0].Quantity, huge.Legs[1].Quantity = 906, 906
	huge.ClientID = NameFor(-0.16)

	broker := &brokerDouble{
		orders: []marketdata.Order{huge},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) {
		return 0, errors.New("the ruleset is unreadable")
	}
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled)
}

// A breach smaller than one set is not a sizing error, and cancelling for it
// spends every entry window without taking a position.
//
// The limit is a share of equity, and equity moves with every tick of the open
// book. So the number a session sized against and the number read a minute later
// are never quite the same - while the session cannot express a position finer
// than one set. Having taken the largest whole number that fits, it has already
// sized as accurately as the instrument allows.
//
// Live on 26 August: 518 sets refused for twelve dollars and thirty-four cents
// against a limit of 15 009, where one set was worth twenty-nine.
func TestABreachSmallerThanOneSetIsNotCancelled(t *testing.T) {
	at := time.Date(2026, 8, 26, 19, 7, 0, 0, time.UTC)
	// 518 sets of a spread whose worst case is 15 022 - twelve dollars over a
	// limit of 15 009.66, and one set is worth twenty-nine.
	edge := spread("o-edge", -0.71, "new", at.Add(-2*time.Minute))
	edge.Quantity = 518
	edge.Legs[0].Quantity, edge.Legs[1].Quantity = 518, 518
	edge.ClientID = NameFor(-0.71)

	broker := &brokerDouble{
		orders: []marketdata.Order{edge},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Ceiling = func(context.Context) (float64, error) { return 15009.66, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled, "twelve dollars is less than one set: the size is already the finest that fits")

	// A breach of many sets is still a sizing error and still cancelled.
	l.Ceiling = func(context.Context) (float64, error) { return 10000, nil }
	l.step(context.Background())
	_, cancelled = broker.seen()
	assert.Equal(t, []string{"o-edge"}, cancelled)
}

// Twenty structures, each inside its own ceiling, still put the whole account at
// risk. On 26 August the portfolio limit was the one number nothing enforced:
// the session was told it and could get it wrong, and nothing between it and the
// market disagreed.
func TestAnOrderThatFillsTheBookIsCancelled(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	one := spread("o-one", -0.16, "new", at.Add(-2*time.Minute))
	one.Quantity, one.Legs[0].Quantity, one.Legs[1].Quantity = 100, 100, 100
	one.ClientID = NameFor(-0.16)

	broker := &brokerDouble{
		orders: []marketdata.Order{one},
		// Already open: a sold spread risking most of what the book allows.
		positions: []marketdata.Position{
			{Symbol: "SPY260828P00700000", Quantity: -50, CostBasis: -6000},
			{Symbol: "SPY260828P00690000", Quantity: 50, CostBasis: 4500},
		},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	told := make(chan string, 1)
	l := ladder(broker, at, t)
	l.Book = func(context.Context) (float64, error) { return 50000, nil }
	l.Wake = func(_ context.Context, cause string) { told <- cause }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Equal(t, []string{"o-one"}, cancelled)

	select {
	case cause := <-told:
		assert.Contains(t, cause, "no room left",
			"the session is told the book is full, not that its order is too large")
	default:
		t.Fatal("the session was not told why its order went away")
	}
}

// With room to spare the order is left alone, and the check costs it nothing.
func TestAnOrderThatFitsTheBookIsWalkedAsBefore(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders:    []marketdata.Order{spread("o-1", -0.16, "new", at.Add(-2*time.Minute))},
		positions: []marketdata.Position{{Symbol: "SPY260828P00700000", Quantity: 1, CostBasis: 300}},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Book = func(context.Context) (float64, error) { return 50000, nil }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled)
}

// A book this code cannot read leaves orders alone: unknown is not the same as
// full, and cancelling on a number we failed to get would take out sound
// structures for a reason that is ours.
func TestAnUnreadableBookCancelsNothing(t *testing.T) {
	at := time.Date(2026, 8, 26, 18, 5, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.16, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	l := ladder(broker, at, t)
	l.Book = func(context.Context) (float64, error) { return 0, errors.New("the ruleset is unreadable") }
	l.step(context.Background())

	_, cancelled := broker.seen()
	assert.Empty(t, cancelled)
}

// A broker answer can carry no legs at all, and the line is built by walking
// them. Measured 28 August: three fills in one minute reached the room as
// "✔    ×33, debit 1.26" - money moved, with nothing to say what was closed.
// The fallback here is the one already used for a symbol that will not parse:
// say less rather than say nothing.
func TestAFillWithNoLegsStillNamesQuantityAndPrice(t *testing.T) {
	at := time.Date(2026, 8, 28, 15, 31, 0, 0, time.UTC)
	order := spread("o-legless", -1.26, "filled", at.Add(-2*time.Minute))
	order.Legs = nil
	order.Quantity, order.FilledQuantity, order.FilledPrice = 33, 33, 1.26
	filled := at.Add(-time.Minute)
	order.FilledAt = &filled
	broker := &brokerDouble{orders: []marketdata.Order{order}}

	var said []string
	rungs := ladder(broker, at, t)
	rungs.Record = record.NewMemory()
	rungs.watching = at.Add(-time.Hour)
	rungs.Say = func(_ context.Context, line string) { said = append(said, line) }
	rungs.step(context.Background())

	require.Len(t, said, 1)
	assert.Equal(t, "✔ filled 33 at 1.26", said[0])
	assert.NotContains(t, said[0], "  ", "a line with empty parts is a line that names nothing")
}

// The chain has to survive a price move. The broker gives a replacement a new id,
// so a fill arrives under an id the earlier steps never mention; without the link
// nothing joins them. Measured 28 August: 19 of 33 fills could not be traced back
// to the call that started them, and this is the link that was missing.
func TestWalkingRecordsTheIdTheReplacementGot(t *testing.T) {
	at := time.Date(2026, 8, 28, 15, 0, 0, 0, time.UTC)
	order := spread("o-1", -0.30, "new", at.Add(-2*time.Minute))
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	kept := record.NewMemory()
	rungs := ladder(broker, at, t)
	rungs.Record = kept
	rungs.step(context.Background())

	state, err := kept.Read(context.Background())
	require.NoError(t, err)

	walked := 0
	for _, step := range state.Steps {
		if step.Action != "walked" {
			continue
		}
		walked++
		require.NotNil(t, step.ReplacedBy, "a step that moved the price must name the id it moved to")
		assert.NotEqual(t, step.OrderRef, *step.ReplacedBy, "the broker mints a new id; the same one back is a lost link")
	}
	require.Equal(t, 1, walked, "one price move, one step")
}

// An order gets a step every `Every`, and it is cancelled `Patience` after it was
// PLACED - not after it last moved.
//
// Both readings came from one field. `age` was taken from the order the broker is
// showing now, and a replacement is a new order with a new submission time, so
// after every step the order was young again: it was skipped on the next tick and
// stepped on the one after. Measured on the live market on 31 August, on both
// judged accounts, chaining `execution_steps` through `replaced_by`: the median
// interval between one order's steps was 90.0 seconds against a configured 45.
// Over eight minutes of patience that is five steps offered instead of ten.
//
// The same field made patience mean "how long it has stood still" rather than
// "how long it has been unfilled", so an order that keeps stepping never ages out
// at all.
func TestAnOrderStepsEveryIntervalAndAgesFromItsFirstPlacement(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)

	// Far from the book on purpose, so the walk is never stopped by arriving.
	book := map[string]marketdata.Quote{
		"QQQ260826P00701000": quote(0.10, 0.80),
		"QQQ260826P00700000": quote(0.05, 0.70),
	}

	// ticks runs the ladder for n intervals of one minute and reports how many of
	// them produced a step, and how long after the order was placed it was
	// cancelled - or -1 for never.
	ticks := func(t *testing.T, patience time.Duration, n int) (int, time.Duration) {
		t.Helper()
		clock := at
		broker := &brokerDouble{
			orders: []marketdata.Order{spread("o-1", -0.30, "new", at)},
			quotes: book,
		}
		// The replacement is stamped a moment AFTER the tick that made it, because
		// that is when it happens: the pass is already running. On the next tick
		// the order is therefore a hair YOUNGER than the interval, which is the
		// whole mechanism - a freshness check reading that field skips it.
		broker.now = func() time.Time { return clock.Add(10 * time.Millisecond) }

		rung := ladder(broker, at, t)
		rung.Every = time.Minute
		rung.Patience = patience
		rung.Now = func() time.Time { return clock }

		steps, cancelledAfter := 0, time.Duration(-1)
		for tick := 1; tick <= n; tick++ {
			clock = at.Add(time.Duration(tick) * time.Minute)
			before, _ := broker.seen()
			was := len(before)
			rung.step(context.Background())
			after, cancelled := broker.seen()
			if len(after) > was {
				steps++
			}
			if len(cancelled) > 0 && cancelledAfter < 0 {
				cancelledAfter = clock.Sub(at)
			}
		}

		return steps, cancelledAfter
	}

	// Patience far out of the way, so this measures the stepping alone.
	t.Run("a step on every interval", func(t *testing.T) {
		steps, _ := ticks(t, time.Hour, 6)
		assert.Equal(t, 6, steps,
			"six intervals, six steps - not one step every other interval")
	})

	// Cancelled on the first tick where the age is PAST patience, not level with
	// it, so five minutes of patience ends on the sixth.
	t.Run("patience runs from the placement", func(t *testing.T) {
		_, cancelledAfter := ticks(t, 5*time.Minute, 10)
		assert.Equal(t, 6*time.Minute, cancelledAfter,
			"an order that keeps stepping still ages out, because it is the same order")
	})
}

// The walk arrives by the time patience ends, however far the book has gone.
//
// A fixed tick per step is a walk that loses ground. Measured on the live market
// on 31 August, following one order through its life: our price and the book were
// 0.18 apart, the ladder conceded a cent at each step, and nine steps later they
// were 0.32 apart - it had given up nine cents, ended further from a fill than it
// started, and died on patience. The book moves several cents in an interval.
func TestTheWalkClosesTheDistanceItStillHasTimeFor(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	placed := at.Add(-6 * time.Minute)

	// The book pays 0.01 for the structure - 0.71 bid on the leg we sell against
	// 0.70 asked for the one we buy - and the order still asks 0.30: a gap of
	// twenty-nine cents. Patience is ten minutes and four remain, so four steps.
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.30, "new", placed)},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.65, 0.70),
		},
	}

	rung := ladder(broker, at, t)
	rung.Every = time.Minute
	rung.Patience = 10 * time.Minute
	rung.step(context.Background())

	replaced, cancelled := broker.seen()
	require.Empty(t, cancelled)
	// Twenty-nine cents over four steps is seven cents a step, not one.
	assert.InDelta(t, -0.23, replaced["o-1"], 1e-9,
		"a cent a step would leave this order twenty-five cents away when patience ends")
}

// A walk that is comfortably on schedule still moves one tick, because the
// distance divided by the steps left is smaller than a tick and a price cannot
// move by less than one.
func TestAWalkWithTimeToSpareStillMovesOneTick(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-1", -0.12, "new", at.Add(-2*time.Minute))},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.61, 0.65),
		},
	}

	ladder(broker, at, t).step(context.Background())

	replaced, _ := broker.seen()
	assert.InDelta(t, -0.11, replaced["o-1"], 1e-9)
}

// The tactic is declared, and the one we have been running is still available.
//
// The proportional step has arithmetic behind it and no live day behind it. The
// account that is submitted is whichever of the two stands higher, so the honest
// way to try it is on ONE account while the other keeps what we know - the
// comparison costs nothing and answers by Wednesday.
func TestTheStrideIsDeclaredAndTheOldOneStillWalksATick(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	placed := at.Add(-6 * time.Minute)

	walk := func(t *testing.T, stride string) float64 {
		t.Helper()
		broker := &brokerDouble{
			orders: []marketdata.Order{spread("o-1", -0.30, "new", placed)},
			quotes: map[string]marketdata.Quote{
				"QQQ260826P00701000": quote(0.71, 0.76),
				"QQQ260826P00700000": quote(0.65, 0.70),
			},
		}
		rung := ladder(broker, at, t)
		rung.Every = time.Minute
		rung.Patience = 10 * time.Minute
		rung.Stride = stride
		rung.step(context.Background())
		replaced, _ := broker.seen()

		return replaced["o-1"]
	}

	assert.InDelta(t, -0.29, walk(t, StrideByTick), 1e-9,
		"one tick a step, whatever the distance - what this ladder did until 1 September")
	assert.InDelta(t, -0.23, walk(t, StrideToArrive), 1e-9,
		"twenty-nine cents over the four steps patience still allows")
	assert.InDelta(t, -0.23, walk(t, ""), 1e-9,
		"empty is the arriving one, so a deployment that names nothing gets the measured fix")
}

// The chain is followed to its end: it stops AT the floor the session named, and
// it is cancelled when patience runs out.
//
// The earlier tests asserted the first replacement only, which is a test that the
// step happened rather than that the walk ended where it should. A large stride
// makes the difference matter: it is the step that could overshoot.
func TestAnArrivingWalkStopsAtTheFloorAndIsThenCancelled(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	clock := at

	// The floor is 0.20 of credit and the book pays only 0.01, so the walk runs
	// out of room at the floor rather than at the book.
	order := spread("o-1", -0.30, "new", at)
	order.ClientID = NameFor(-0.20)
	broker := &brokerDouble{
		orders: []marketdata.Order{order},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.65, 0.70),
		},
	}
	broker.now = func() time.Time { return clock.Add(10 * time.Millisecond) }

	rung := ladder(broker, at, t)
	rung.Every = time.Minute
	rung.Patience = 5 * time.Minute
	rung.Now = func() time.Time { return clock }

	for tick := 1; tick <= 8; tick++ {
		clock = at.Add(time.Duration(tick) * time.Minute)
		rung.step(context.Background())
	}

	replaced, cancelled := broker.seen()
	require.NotEmpty(t, replaced, "the walk has to have happened for its end to mean anything")
	worst := math.Inf(-1)
	for _, price := range replaced {
		worst = math.Max(worst, price)
	}
	assert.InDelta(t, -0.20, worst, 1e-9,
		"it ends AT the floor the session named and never past it, however large a step it takes")
	assert.NotEmpty(t, cancelled, "and patience ends an order the book never took")
}

// A working order missing from one bounded read still ages out.
//
// The ladder keeps chain ages in memory, and the first version dropped a chain
// whenever the order was absent from the broker's answer. That answer is bounded
// by Reads and returns the newest, so a working order can be missing from a pass.
// Its chain would then be rebuilt from the REPLACEMENT's submission time - fresh,
// because the ladder had already walked it - patience would restart, and an order
// whose patience keeps restarting is never cancelled. It also holds its underlying
// out of the entry list for as long as it lives, which on 31 August was what
// emptied twenty-six entry windows across the two accounts.
func TestAnOrderMissedByOneBoundedReadStillAgesOut(t *testing.T) {
	at := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	clock := at

	broker := &brokerDouble{
		orders: []marketdata.Order{spread("o-old", -0.30, "new", at)},
		quotes: map[string]marketdata.Quote{
			"QQQ260826P00701000": quote(0.71, 0.76),
			"QQQ260826P00700000": quote(0.65, 0.70),
		},
	}
	broker.now = func() time.Time { return clock.Add(10 * time.Millisecond) }

	rung := ladder(broker, at, t)
	rung.Every = time.Minute
	rung.Patience = 3 * time.Minute
	rung.Reads = 1
	rung.Now = func() time.Time { return clock }

	// One pass alone, so the order is WALKED and the broker stamps its
	// replacement with a fresh submission time. That fresh stamp is the thing a
	// rebuilt chain would take patience from.
	clock = at.Add(time.Minute)
	rung.step(context.Background())
	replaced, _ := broker.seen()
	require.NotEmpty(t, replaced, "the order has to have been walked for this to test anything")

	// A newer order now hides it: the broker shows the newest one only.
	broker.mu.Lock()
	broker.orders = append(broker.orders, spread("o-new", -0.30, "new", clock))
	broker.mu.Unlock()
	for tick := 2; tick <= 3; tick++ {
		clock = at.Add(time.Duration(tick) * time.Minute)
		rung.step(context.Background())
	}

	// The newer one is gone and the older is visible again, four minutes after it
	// was placed and one minute past its patience.
	broker.mu.Lock()
	broker.orders = broker.orders[:1]
	broker.mu.Unlock()
	clock = at.Add(4 * time.Minute)
	rung.step(context.Background())

	_, cancelled := broker.seen()
	assert.NotEmpty(t, cancelled,
		"placed four minutes ago against three minutes of patience: cancelled, not handed a fresh life by the replacement's own timestamp")
}
