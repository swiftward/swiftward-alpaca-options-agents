package execution

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// A structure is priced per SET, and the legs of a multi-leg order carry the
// absolute quantity beside the ratio. Pricing by quantity multiplies the answer
// by the number of sets.
//
// The numbers are the real fill of 27 August: SPY 765/755 put backspread, two
// sets, sold leg qty 2 ratio 1, bought leg qty 4 ratio 2, filled at 1.93 and
// 0.62 against a limit of -0.69. By ratio the arithmetic lands exactly on the
// limit; by quantity it lands on -1.38, and the ladder read that as a book
// standing better than our own price - so it conceded nothing and waited out
// its whole eight minutes of patience before the order was cancelled.
func TestAStructureIsPricedPerSetNotPerContract(t *testing.T) {
	order := marketdata.Order{
		ID:         "the backspread",
		Quantity:   2,
		LimitPrice: -0.69,
		Legs: []marketdata.Order{
			{Symbol: "SPY260902P00765000", Side: "sell", Quantity: 2, Ratio: 1},
			{Symbol: "SPY260902P00755000", Side: "buy", Quantity: 4, Ratio: 2},
		},
	}
	quotes := map[string]marketdata.Quote{
		"SPY260902P00765000": {Bid: 1.93, Ask: 1.95},
		"SPY260902P00755000": {Bid: 0.60, Ask: 0.62},
	}

	showing, known := Showing(order, quotes)

	require.True(t, known)
	require.InDelta(t, -0.69, showing, 0.001,
		"priced per set: -1.93 + 2*0.62. By quantity it would be -1.38, twice the truth")
}

// An order with no ratio - anything that is not multi-leg - keeps behaving as
// it did: there the leg IS the set.
func TestWithoutARatioTheQuantityIsTheRatio(t *testing.T) {
	order := marketdata.Order{
		ID:         "a plain vertical",
		Quantity:   5,
		LimitPrice: -0.12,
		Legs: []marketdata.Order{
			{Symbol: "SPY260827P00759000", Side: "sell", Quantity: 1},
			{Symbol: "SPY260827P00758000", Side: "buy", Quantity: 1},
		},
	}
	quotes := map[string]marketdata.Quote{
		"SPY260827P00759000": {Bid: 0.64, Ask: 0.66},
		"SPY260827P00758000": {Bid: 0.50, Ask: 0.52},
	}

	showing, known := Showing(order, quotes)

	require.True(t, known)
	require.InDelta(t, -0.12, showing, 0.001)
}
