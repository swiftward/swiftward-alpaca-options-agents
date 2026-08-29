package placement

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Found in review on 28 August 2026. Windows were selected by a volatility that
// ended on the window's OWN last day - that is, a window was judged by its own
// outcome.
//
// The history here is built so the difference is visible by eye: quiet, one jump
// in the middle, quiet. The jump happened in quiet weather, so the windows that
// caught it must fall into today's quiet-regime sample. Under the old indexing
// they were thrown out - their closing volatility spiked - and the tail a
// backspread is bought for disappeared from the sample it is judged by.
func TestTheRegimeIsJudgedAtTheWindowsOpeningNotItsEnd(t *testing.T) {
	const days = 3

	quiet := func(n int, from float64) []float64 {
		out := make([]float64, n)
		price := from
		for i := range out {
			// A small saw: volatility clearly above zero, but the same throughout.
			if i%2 == 0 {
				price *= 1.001
			} else {
				price *= 0.999
			}
			out[i] = price
		}
		return out
	}

	closes := quiet(300, 700)
	jumpAt := len(closes) / 2
	// One day that moves ten percent up, and everything after it continues from the
	// new price.
	for i := jumpAt; i < len(closes); i++ {
		closes[i] *= 1.10
	}

	moves, vol, err := windows(closes, days)
	require.NoError(t, err)
	require.Greater(t, vol, 0.0)

	var caught int
	for _, move := range moves {
		if move > 1.05 {
			caught++
		}
	}
	assert.Positive(t, caught,
		"a window opened in quiet weather that caught the jump belongs to the quiet-weather sample")
}

// Trading days are counted on the exchange's calendar. The team's machine lives
// in UTC+8: Thursday evening in New York is already Friday morning there, and by
// the local date not a day remains before Friday's expiry.
func TestTheTradingDaysAreCountedOnTheExchangesCalendar(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Thursday 3 September, 20:00 in New York. Locally that is already Friday 08:00.
	evening := time.Date(2026, 9, 4, 8, 0, 0, 0, time.FixedZone("UTC+8", 8*3600))
	friday := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, 1, tradingDaysUntil(evening, friday, newYork),
		"in New York it is still Thursday, and a whole Friday session lies ahead")
	assert.Equal(t, 0, tradingDaysUntil(evening, friday, time.UTC),
		"while by the machine clock the day is gone - exactly the refusal this catches")
}

// A quote whose credit almost equals the width is broken. The worst case per set
// goes to zero, hundreds of sets fit, and the expectation is linear in sets: a row
// built on one stale lot would head the list.
func TestABrokenQuoteDoesNotLeadTheList(t *testing.T) {
	held := aBook("call")
	// Everything as usual, but one pair is quoted so the credit eats the width.
	held.priceFor = func(strike float64) (float64, float64) {
		switch {
		case strike == 795:
			return 4.90, 4.95 // an outsized bid on the sold leg
		case strike == 800:
			return 0.004, 0.005 // and nearly free on the bought ones
		}
		return 0.02, 0.03
	}

	answer, err := aScorer(held).Score(context.Background(), anAsk())
	require.NoError(t, err)

	for _, at := range answer.Placements {
		assert.False(t, at.ShortStrike == 795 && at.LongStrike == 800,
			"a pair with a broken quote does not reach the answer at all")
		assert.LessOrEqual(t, at.Credit, (at.LongStrike-at.ShortStrike)/2,
			"a credit above half the width is not a trade, it is a typo in the book")
	}
}

// Windows overlap: a step of one day against a term of several. The reader needs
// to know how many of them are independent, or "the best percent of the history"
// turns out to be one episode counted five times.
func TestItSaysHowManyWindowsAreIndependent(t *testing.T) {
	answer, err := aScorer(aBook("call")).Score(context.Background(), anAsk())
	require.NoError(t, err)

	assert.Positive(t, answer.Independent)
	assert.Less(t, answer.Independent, answer.Windows,
		"there are always more overlapping windows than independent ones")
	assert.Equal(t, answer.Windows/answer.TradingDays, answer.Independent)
}
