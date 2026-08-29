package takeprofit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

type brokerDouble struct {
	held   []marketdata.Position
	orders []marketdata.Order
	quotes map[string]marketdata.Quote
	sent   []closed
	fail   error
}

type closed struct {
	legs  []marketdata.Leg
	sets  int
	limit float64
}

func (b *brokerDouble) Positions(context.Context) ([]marketdata.Position, error) { return b.held, nil }
func (b *brokerDouble) Orders(context.Context, int) ([]marketdata.Order, error)  { return b.orders, nil }
func (b *brokerDouble) Quotes(_ context.Context, _ []string) (map[string]marketdata.Quote, error) {
	return b.quotes, nil
}

// The broker answers a close with the new order's id, and the double does the
// same: one that answered with an empty string would let a watch that never
// writes the order down pass every test here.
func (b *brokerDouble) CloseStructure(_ context.Context, legs []marketdata.Leg, sets int, limit float64, _ string) (string, error) {
	if b.fail != nil {
		return "", b.fail
	}
	b.sent = append(b.sent, closed{legs: legs, sets: sets, limit: limit})
	return fmt.Sprintf("order-%d", len(b.sent)), nil
}

func leg(symbol string, qty, entry float64) marketdata.Position {
	return marketdata.Position{
		Symbol: symbol, AssetClass: "us_option", Quantity: qty, AverageEntryPrice: entry,
	}
}

func quote(bid, ask float64) marketdata.Quote { return marketdata.Quote{Bid: bid, Ask: ask} }

// The 725/726 spread, 170 sets, taken at a credit of 0.19 - the one that on
// 28 August gave back 74% of its credit and was noticed by nobody.
func theQQQSpread() []marketdata.Position {
	return []marketdata.Position{
		leg("QQQ260828C00725000", -170, 0.50),
		leg("QQQ260828C00726000", 170, 0.31),
	}
}

func watching(b *brokerDouble, at float64) *Watch {
	newYork, _ := time.LoadLocation("America/New_York")
	return &Watch{
		Broker: b, At: at, Every: time.Second,
		// Midday on 28 August in New York: the QQQ spread's expiry day is still running.
		Now:   func() time.Time { return time.Date(2026, 8, 28, 16, 0, 0, 0, time.UTC) },
		Where: newYork,
		Log:   zap.NewNop(), sent: map[string]time.Time{},
	}
}

// An expired structure cannot be closed: it is waiting to settle. Without this
// check the watch sent FIVE orders on 29 August on a QQQ spread that had expired
// the day before, one every ten minutes, and the ladder cancelled each on
// patience.
//
// The credit condition did not save it but urged it on: on an expired structure
// the buy-back goes to zero, so it looks perfectly ripe.
func TestAnExpiredStructureIsNotClosed(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.01, 0.02),
		"QQQ260828C00726000": quote(0.01, 0.02),
	}}
	w := watching(b, 0.5)
	w.Where = newYork
	// The next day on the exchange.
	w.Now = func() time.Time { return time.Date(2026, 8, 29, 16, 0, 0, 0, time.UTC) }

	w.step(context.Background())
	assert.Empty(t, b.sent, "what expired waits to settle, not for an order")
}

// On the expiry day ITSELF, closing is allowed right up to the bell.
func TestOnTheDayOfExpiryItStillCloses(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	watching(b, 0.5).step(context.Background())
	assert.Len(t, b.sent, 1)
}

func TestItReadsTheStructureOutOfWhatIsHeld(t *testing.T) {
	got, ambiguous := Group(theQQQSpread())
	require.Len(t, got, 1)
	assert.Empty(t, ambiguous)

	s := got[0]
	assert.Equal(t, "QQQ", s.Underlying)
	assert.Equal(t, "2026-08-28", s.Expiration)
	assert.Equal(t, "call", s.Kind)
	assert.Equal(t, 170, s.Sets, "170 short and 170 long is 170 sets, one to one")
	assert.InDelta(t, 0.19, s.Credit, 1e-9, "sold at 0.50, bought at 0.31: a credit of 0.19 per set")
}

// A backspread: six sold against twelve bought is SIX sets one to two, not twelve
// halves. An error here would close twice what we hold.
func TestSetsAreCountedByTheGreatestCommonDivisor(t *testing.T) {
	got, _ := Group([]marketdata.Position{
		leg("SPY260902C00777000", -6, 1.34),
		leg("SPY260902C00780000", 12, 0.63),
	})
	require.Len(t, got, 1)
	assert.Equal(t, 6, got[0].Sets)
	assert.InDelta(t, 1.34-2*0.63, got[0].Credit, 1e-9)
}

// Two structures on one underlying, expiry and type are NOT one structure.
//
// The broker forgets which legs arrived together, so a premium call spread and a
// convexity backspread on the same expiry come back as four holdings that look
// like one. Closing "it" would price a structure nobody opened and take the hedge
// away with the winner - and the hedge exists for the day the winner loses.
func TestTwoStructuresOnOneExpiryAreDeclinedRatherThanMerged(t *testing.T) {
	got, ambiguous := Group([]marketdata.Position{
		// The premium spread: sold 640, bought 645.
		leg("SPY260904C00640000", -10, 1.20),
		leg("SPY260904C00645000", 10, 0.55),
		// The convexity layer beside it: sold 650, bought two 660.
		leg("SPY260904C00650000", -5, 0.30),
		leg("SPY260904C00660000", 10, 0.12),
	})

	assert.Empty(t, got, "nothing is closed on a guess about which leg belonged to which")
	assert.Equal(t, []string{"SPY-2026-09-04-call"}, ambiguous,
		"and the holding is named, so it is declined out loud rather than skipped")
}

// One leg on its own is half of something whose other half has gone - assigned,
// expired or closed by hand. There is no credit to measure a share of.
func TestALoneLegIsNotAStructure(t *testing.T) {
	got, ambiguous := Group([]marketdata.Position{leg("SPY260904C00640000", -10, 1.20)})

	assert.Empty(t, got)
	assert.Equal(t, []string{"SPY-2026-09-04-call"}, ambiguous)
}

// The buy-back is computed on the sides of the book the order will cross: what
// was sold is bought back at the ask, what was bought is sold at the bid. At the
// midpoints it would come out cheaper, and the close would fire where it actually
// cannot.
func TestTheBuyBackIsPricedAtTheSidesAnOrderCrosses(t *testing.T) {
	held, _ := Group(theQQQSpread())
	s := held[0]
	cost, ok := BuyBack(s, map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	})
	require.True(t, ok)
	assert.InDelta(t, 0.08, cost, 1e-9, "buy back 725 at 0.10, sell 726 at 0.02")
}

func TestItClosesWhenEnoughOfTheCreditIsBack(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	// A buy-back of 0.08 against a credit of 0.19 is 42%: the 0.5 threshold is met.
	watching(b, 0.5).step(context.Background())

	require.Len(t, b.sent, 1)
	sent := b.sent[0]
	assert.Equal(t, 170, sent.sets)
	// POSITIVE. This assertion said the opposite until 29 August 2026, and the
	// live account settled it: every close the watch sent carried a negative
	// limit and every one was cancelled unfilled, while every close the session
	// sent carried a positive one and filled. A debit is priced positive.
	assert.InDelta(t, 0.08, sent.limit, 1e-9, "buying a structure back costs money, and a debit is positive")
	require.Len(t, sent.legs, 2)
	for _, l := range sent.legs {
		assert.Equal(t, 1, l.Ratio)
		assert.Equal(t, l.Symbol == "QQQ260828C00725000", l.Buy,
			"the sold leg is bought back, the bought leg is sold")
	}
}

func TestItLeavesAStructureThatHasNotGivenEnoughBack(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.30, 0.32),
		"QQQ260828C00726000": quote(0.14, 0.16),
	}}
	// A buy-back of 0.18 against a credit of 0.19: almost nothing has been given back.
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// A structure taken at a DEBIT is not governed by this rule: there is no share of
// a credit to count, and closing it on this threshold means closing the convexity
// layer at an arbitrary moment.
func TestAStructureBoughtForADebitIsLeftAlone(t *testing.T) {
	b := &brokerDouble{
		held: []marketdata.Position{
			leg("SPY260902C00777000", -6, 0.30),
			leg("SPY260902C00780000", 12, 0.40),
		},
		quotes: map[string]marketdata.Quote{
			"SPY260902C00777000": quote(0.01, 0.02),
			"SPY260902C00780000": quote(0.01, 0.02),
		},
	}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// An order already walking to the book is the same structure in the middle of
// closing. A second one would close twice and leave the account short something it
// never held.
func TestItDoesNotCloseWhatIsAlreadyBeingClosed(t *testing.T) {
	b := &brokerDouble{
		held:   theQQQSpread(),
		orders: []marketdata.Order{{Symbol: "QQQ260828C00725000", Status: "new"}},
		quotes: map[string]marketdata.Quote{
			"QQQ260828C00725000": quote(0.09, 0.10),
			"QQQ260828C00726000": quote(0.02, 0.03),
		},
	}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// The same, for the shape every close this watch sends actually has: MULTI-LEG,
// whose own symbol is empty and whose contracts are on the legs. Matched by the
// order's symbol alone, the guard held one empty string and matched nothing, so
// after a restart - which is when it matters, the memory of what was sent being
// gone - a second close would go out on a structure already being closed.
func TestItDoesNotCloseWhatAMultiLegOrderIsAlreadyClosing(t *testing.T) {
	b := &brokerDouble{
		held: theQQQSpread(),
		orders: []marketdata.Order{{
			Status: "new", Class: "mleg", Quantity: 170,
			Legs: []marketdata.Order{
				{Symbol: "QQQ260828C00725000", Side: "buy", Quantity: 170},
				{Symbol: "QQQ260828C00726000", Side: "sell", Quantity: 170},
			},
		}},
		quotes: map[string]marketdata.Quote{
			"QQQ260828C00725000": quote(0.09, 0.10),
			"QQQ260828C00726000": quote(0.02, 0.03),
		},
	}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent, "the close in flight is the close; a second one closes twice")
}

func TestItDoesNotSendTheSameCloseTwice(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	w := watching(b, 0.5)
	w.step(context.Background())
	w.step(context.Background())
	assert.Len(t, b.sent, 1)
}

// A leg whose book stands on one side has no closing price. An order against half
// a quote is a gift, not a trade.
func TestALegQuotedOnOneSideStopsTheClose(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	watching(b, 0.5).step(context.Background())
	assert.Empty(t, b.sent)
}

// A share of zero switches the watch off entirely and says so, rather than running empty.
func TestWithoutAShareItDoesNotRun(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread()}
	w := &Watch{Broker: b, At: 0, Log: zap.NewNop(), Now: time.Now}
	require.NoError(t, w.Run(context.Background()))
	assert.Empty(t, b.sent)
}

func TestARefusedCloseIsNotRememberedAsSent(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), fail: errors.New("the broker said no"),
		quotes: map[string]marketdata.Quote{
			"QQQ260828C00725000": quote(0.09, 0.10),
			"QQQ260828C00726000": quote(0.02, 0.03),
		}}
	w := watching(b, 0.5)
	w.step(context.Background())
	b.fail = nil
	w.step(context.Background())
	assert.Len(t, b.sent, 1, "a refusal must not lock a structure out forever")
}

// An order the watch sends is written down at the moment it is sent. Until it
// was, a closing order existed only at the broker until the ladder happened to
// notice it on its next pass, and one cancelled before that pass was in the
// record nowhere at all - measured 29 August by reconciling against the
// broker's own list: 106 of 114, and every one of the eight holes was an order
// this watch had sent.
func TestAnOrderTheWatchSendsIsWrittenDownAtOnce(t *testing.T) {
	b := &brokerDouble{held: theQQQSpread(), quotes: map[string]marketdata.Quote{
		"QQQ260828C00725000": quote(0.09, 0.10),
		"QQQ260828C00726000": quote(0.02, 0.03),
	}}
	kept := record.NewMemory()
	w := watching(b, 0.5)
	w.Record = kept
	w.step(context.Background())

	require.Len(t, b.sent, 1, "the structure gave back its credit and should be closed")

	state, err := kept.Read(context.Background())
	require.NoError(t, err)

	placed := 0
	for _, step := range state.Steps {
		if step.Action == "placed" {
			placed++
			assert.NotEmpty(t, step.OrderRef,
				"an order with no id cannot be reconciled against the broker's list")
		}
	}
	assert.Equal(t, 1, placed, "one order sent, one order written down")
}
