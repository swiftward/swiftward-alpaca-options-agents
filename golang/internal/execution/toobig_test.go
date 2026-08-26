package execution

import (
	"testing"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A credit spread's worst case is the width it cannot cross, less the credit
// already taken - times a hundred shares, times the sets.
func TestTheWorstCaseOfACreditSpread(t *testing.T) {
	// Sell the 701 put, buy the 700, fifty sets, for a credit of 0.20. A credit
	// is quoted negative.
	worst, known := WorstCase(marketdata.Order{
		Quantity: 50, LimitPrice: -0.20,
		Legs: []marketdata.Order{
			{Symbol: "QQQ260828P00701000", Side: "sell", Quantity: 50},
			{Symbol: "QQQ260828P00700000", Side: "buy", Quantity: 50},
		},
	})

	require.True(t, known)
	// A dollar of width less twenty cents of credit, a hundred shares, fifty sets.
	assert.InDelta(t, -4000, worst, 1e-6)
}

// The whole reason this exists: the order that was cancelled by hand on
// 26 August. Nothing between the session and the market disagreed with it.
func TestTheOrderThatShouldHaveBeenStopped(t *testing.T) {
	// DIA 540/541 call, 906 sets, resting for a credit of 0.16.
	worst, known := WorstCase(marketdata.Order{
		Quantity: 906, LimitPrice: -0.16,
		Legs: []marketdata.Order{
			{Symbol: "DIA260828C00540000", Side: "sell", Quantity: 906},
			{Symbol: "DIA260828C00541000", Side: "buy", Quantity: 906},
		},
	})

	require.True(t, known)
	assert.InDelta(t, -76104, worst, 1)
	assert.Greater(t, -worst, 15000.0, "and fifteen thousand was the limit")
}

// A backspread buys two legs against one sold, and the ratio lives on the legs.
// Reading the order's quantity for every leg would price it as a vertical and
// report a loss it cannot have.
func TestTheRatioOfABackspreadIsRead(t *testing.T) {
	// Sell one 200 put, buy two 195 puts, seven sets, for a debit of 0.50.
	worst, known := WorstCase(marketdata.Order{
		Quantity: 7, LimitPrice: 0.50,
		Legs: []marketdata.Order{
			{Symbol: "NVDA260828P00200000", Side: "sell", Quantity: 7},
			{Symbol: "NVDA260828P00195000", Side: "buy", Quantity: 14},
		},
	})

	require.True(t, known)
	// Worst at the bought strike: the sold leg is five dollars in the money and
	// the bought ones are worth nothing. Plus the debit paid.
	assert.InDelta(t, -3850, worst, 1)
}

// An order this code cannot read is left alone, not cancelled. Unknown is not
// the same as too large, and a symbol we failed to parse is our fault, not the
// structure's.
func TestAnUnreadableOrderIsNotJudged(t *testing.T) {
	_, known := WorstCase(marketdata.Order{
		Quantity: 10, LimitPrice: -0.20,
		Legs: []marketdata.Order{{Symbol: "not-a-contract", Side: "sell", Quantity: 10}},
	})
	assert.False(t, known)

	_, known = WorstCase(marketdata.Order{Quantity: 10, LimitPrice: -0.20})
	assert.False(t, known, "an order with no legs says nothing about its risk")
}
