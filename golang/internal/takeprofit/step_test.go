package takeprofit

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// A backspread is not a credit vertical, and this watch must not touch one.
//
// It sells one and buys two, so it has a sold leg and a bought leg: counting legs
// alone calls it a vertical, and the watch then buys it back as soon as its small
// credit is recovered. That is the whole structure destroyed for a few dollars -
// a backspread is not paid by decay, it is paid by a move that has not happened
// yet, and whoever opened it said how long to hold it. Live on 31 August: SPY
// 744/726 opened at 13:55 for a credit of 0.03, closing order out at 13:58.
func TestABackspreadIsNotAVerticalAndIsLeftAlone(t *testing.T) {
	held := []marketdata.Position{
		{Symbol: "SPY260903P00744000", AssetClass: "us_option", Quantity: -1, AverageEntryPrice: 0.15},
		{Symbol: "SPY260903P00726000", AssetClass: "us_option", Quantity: 2, AverageEntryPrice: 0.06},
	}

	structures, ambiguous := Group(held)
	assert.Empty(t, structures, "a one-for-two is nobody's credit vertical")
	assert.Equal(t, []string{"SPY-2026-09-03-put"}, ambiguous,
		"and it is named as declined rather than passed over in silence")
}

// The ordinary case still works, or the change above would simply have switched
// the watch off.
func TestAOneForOneVerticalIsStillGrouped(t *testing.T) {
	held := []marketdata.Position{
		{Symbol: "TSLA260831P00352500", AssetClass: "us_option", Quantity: -39, AverageEntryPrice: 0.63},
		{Symbol: "TSLA260831P00350000", AssetClass: "us_option", Quantity: 39, AverageEntryPrice: 0.36},
	}

	structures, ambiguous := Group(held)
	require.Len(t, structures, 1)
	assert.Empty(t, ambiguous)
	assert.Equal(t, 39, structures[0].Sets)
	assert.InDelta(t, 0.27, structures[0].Credit, 1e-9)
}
