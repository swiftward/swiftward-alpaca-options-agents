package screener

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// A strike at the price is a coin: half the time it ends on each side.
func TestAStrikeAtThePriceIsACoin(t *testing.T) {
	chance, ok := Survival(710, 710, 0.15, 3*time.Hour)
	require.True(t, ok)
	assert.InDelta(t, 0.5, chance, 1e-9)
}

// Distance is only ever distance in the name's OWN movement, which is the whole
// reason this exists. Half a percent is far on an index and near on a share that
// moves five percent a day, and a rule written in percent cannot tell them apart.
func TestTheSameDistanceIsFarOnOneNameAndNearOnAnother(t *testing.T) {
	left := 2*time.Hour + 45*time.Minute

	// An index at fifteen volatility, half a percent away.
	quiet, ok := Survival(710, 706.45, 0.15, left)
	require.True(t, ok)

	// A share at fifty-five volatility, the same half percent away.
	restless, ok := Survival(349, 347.25, 0.55, left)
	require.True(t, ok)

	assert.Greater(t, quiet, 0.9, "the index barely reaches it")
	assert.Less(t, restless, 0.75, "the share reaches it often")
	assert.Greater(t, quiet, restless)
}

// Time is the other half. The same strike is a different bet with two hours left
// and with ten minutes left, and on expiry day that is most of what changes.
func TestLessTimeLeftMeansTheStrikeIsReachedLessOften(t *testing.T) {
	far, ok := Survival(710, 707, 0.20, 3*time.Hour)
	require.True(t, ok)
	near, ok := Survival(710, 707, 0.20, 10*time.Minute)
	require.True(t, ok)

	assert.Greater(t, near, far, "ten minutes cannot cover what three hours can")
	assert.Greater(t, near, 0.99)
}

// Nothing to compute from is answered with "cannot", never with a number: a
// survival of zero and an unknown survival lead to opposite decisions.
func TestSurvivalRefusesWhereThereIsNothingToComputeFrom(t *testing.T) {
	for _, missing := range []struct {
		what                      string
		price, strike, volatility float64
		left                      time.Duration
	}{
		{"no volatility", 710, 707, 0, time.Hour},
		{"no time left", 710, 707, 0.2, 0},
		{"the clock already past expiry", 710, 707, 0.2, -time.Hour},
		{"no price", 0, 707, 0.2, time.Hour},
		{"no strike", 710, 0, 0.2, time.Hour},
	} {
		t.Run(missing.what, func(t *testing.T) {
			_, ok := Survival(missing.price, missing.strike, missing.volatility, missing.left)
			assert.False(t, ok)
		})
	}
}

// Expiry is the close of the day the contract dies, not midnight. Getting this
// wrong on expiry day is eight hours of movement that is not there, which reads
// as every strike being safe.
func TestWhatIsLeftIsCountedToTheClose(t *testing.T) {
	day := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 8, 26, 17, 15, 0, 0, time.UTC)

	assert.Equal(t, 2*time.Hour+45*time.Minute, leftUntil(day, at))
}

// The point of the whole file: the expiry-day book gets a real edge instead of
// an absent one, and the edge separates what the market pays fairly from what it
// does not. Both structures below pay well and only one is worth selling.
func TestTheExpiryDayBookIsMeasuredFromVolatility(t *testing.T) {
	today := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	at := time.Date(2026, 8, 26, 17, 15, 0, 0, time.UTC)

	// A put spread a third of a percent under a quiet index, paying a third of
	// its risk. No delta, because the broker computes none on expiry day.
	quiet := []marketdata.Contract{putOn(707, today), putOn(708, today)}
	quietQuotes := map[string]marketdata.Quote{
		quiet[1].Symbol: volatile(quote(0.25, 0.25), 0.16),
		quiet[0].Symbol: volatile(quote(0.00001, 0.00001), 0.16),
	}

	want := anything()
	want.MinOutOfTheMoney = 0.1
	found := Best("QQQ", 710, quiet, quietQuotes, at, want, Refused{})
	require.Len(t, found, 1)
	require.NotNil(t, found[0].Edge, "no delta, and still measured")
	assert.Equal(t, FromVolatility, found[0].EdgeFrom)
	assert.Positive(t, *found[0].Edge)

	// The same shape on a share that moves four times as much. It pays the same
	// and is reached far more often, so the edge has to turn negative.
	restless := []marketdata.Contract{putOn(345, today), putOn(346, today)}
	restlessQuotes := map[string]marketdata.Quote{
		restless[1].Symbol: volatile(quote(0.25, 0.25), 0.60),
		restless[0].Symbol: volatile(quote(0.00001, 0.00001), 0.60),
	}

	want.LeastEdge = 0
	risky := Best("TSLA", 347, restless, restlessQuotes, at, want, Refused{})
	require.Len(t, risky, 1)
	require.NotNil(t, risky[0].Edge)
	assert.Negative(t, *risky[0].Edge,
		"paid the same for a strike the price reaches far more often")
}

func volatile(q marketdata.Quote, impliedVolatility float64) marketdata.Quote {
	q.ImpliedVolatility = &impliedVolatility

	return q
}
