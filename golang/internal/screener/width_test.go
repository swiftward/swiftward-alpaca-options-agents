package screener

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// A spread too narrow for its edge to be measured is not listed.
//
// Edge is counted in points of the width and credits move on a one-cent grid, so
// one tick is two points of edge on a half-wide spread and a fifth of a point on
// a five-wide one. Measured on the sweep of 31 August: every one of the 119
// structures clearing +3 was a dollar wide or narrower, and the session's own
// re-quote turned them negative. The threshold was reading a number the quote
// could not carry.
func TestASpreadTooNarrowToMeasureIsNotListed(t *testing.T) {
	expiry := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 9, 1, 15, 0, 0, 0, time.UTC)
	delta := -0.20

	// A put spread on a 700 underlying, sold at 690, paying 0.20 after the
	// crossing. The two cases differ in the bought strike and in nothing else.
	priced := func(t *testing.T, longStrike, leastWidth float64) (Candidate, bool) {
		t.Helper()
		short := marketdata.Contract{Symbol: "QQQ_short", Strike: 690, Expiration: expiry}
		long := marketdata.Contract{Symbol: "QQQ_long", Strike: longStrike, Expiration: expiry}
		quotes := map[string]marketdata.Quote{
			"QQQ_short": {Bid: 0.95, Ask: 1.00, Delta: &delta},
			"QQQ_long":  {Bid: 0.75, Ask: 0.80},
		}
		refused := Refused{}

		return priceOneSpread("QQQ", "put", 700, short, long, quotes, now, nil, Wanted{
			MinOutOfTheMoney: 0.1, MaxOutOfTheMoney: 5,
			MinCreditToRisk: 1, MostCreditToRisk: 100, MaxCostShare: 200,
			MostDelta: 0.30, LeastWidth: leastWidth,
		}, refused)
	}

	t.Run("a dollar wide is listed", func(t *testing.T) {
		found, ok := priced(t, 689, 1.0)
		require.True(t, ok, "a spread whose edge one tick moves by one point is measurable")
		assert.Equal(t, 690.0, found.ShortStrike)
	})

	t.Run("half a dollar wide is not", func(t *testing.T) {
		_, ok := priced(t, 689.5, 1.0)
		assert.False(t, ok, "one tick is two points of edge here, so the number cannot be ranked")
	})

	t.Run("without the bound the narrow one is listed again", func(t *testing.T) {
		_, ok := priced(t, 689.5, 0)
		assert.True(t, ok, "zero leaves width unchecked, which is what every other bound does")
	})
}
