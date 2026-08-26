package execution

import (
	"context"
	"errors"
	"fmt"
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
	assert.Contains(t, told[0], "20 из 50")
	assert.NotContains(t, told[0], "не исполнилась вовсе")
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
	assert.Equal(t, "✔ QQQ 701/700 put ×50, кредит 0.28", said[0])

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
	assert.Contains(t, said[0], "дебет 0.07")
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
