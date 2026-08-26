package screener

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

var expiry = time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)

func put(strike float64) marketdata.Contract {
	return marketdata.Contract{Symbol: name("P", strike), Expiration: expiry, Strike: strike, Type: "put"}
}

func call(strike float64) marketdata.Contract {
	return marketdata.Contract{Symbol: name("C", strike), Expiration: expiry, Strike: strike, Type: "call"}
}

func name(kind string, strike float64) string {
	return "QQQ260828" + kind + string(rune('0'+int(strike)%10)) + fmtStrike(strike)
}

func fmtStrike(strike float64) string {
	return time.Unix(int64(strike*1000), 0).UTC().Format("150405")
}

func quote(bid, ask float64) marketdata.Quote { return marketdata.Quote{Bid: bid, Ask: ask} }

func anything() Wanted {
	return Wanted{MinOutOfTheMoney: 0.1, MaxOutOfTheMoney: 5, MinCreditToRisk: 0, MaxCostShare: 1000}
}

// The credit is what the book would give, and the risk is what is left of the
// width. Both are read off the two legs, not guessed from one.
func TestAPutSpreadIsPricedFromBothLegs(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol: quote(0.71, 0.79),
		put(700).Symbol: quote(0.51, 0.59),
	}

	found := Best("QQQ", 710, contracts, quotes, anything())
	require.Len(t, found, 1)

	c := found[0]
	assert.Equal(t, "put", c.Type)
	assert.InDelta(t, 701, c.ShortStrike, 1e-9, "a put spread sells the strike nearer the money")
	assert.InDelta(t, 700, c.LongStrike, 1e-9)
	assert.InDelta(t, 0.20, c.Credit, 1e-9)
	assert.InDelta(t, 0.80, c.Risk, 1e-9)
	assert.InDelta(t, 25, c.CreditToRisk, 0.01)
	assert.InDelta(t, 0.16, c.Cost, 1e-9)
	assert.InDelta(t, 80, c.CostShare, 0.01)
	assert.InDelta(t, 1.27, c.OutOfTheMoney, 0.01)
}

// A call spread sells the strike nearer the money too - which is the LOWER one.
// Getting this backwards prices the structure as a debit and hides it entirely.
func TestACallSpreadSellsTheLowerStrike(t *testing.T) {
	contracts := []marketdata.Contract{call(720), call(721)}
	quotes := map[string]marketdata.Quote{
		call(720).Symbol: quote(0.60, 0.70),
		call(721).Symbol: quote(0.40, 0.50),
	}

	found := Best("QQQ", 710, contracts, quotes, anything())
	require.Len(t, found, 1)
	assert.Equal(t, "call", found[0].Type)
	assert.InDelta(t, 720, found[0].ShortStrike, 1e-9)
	assert.InDelta(t, 0.20, found[0].Credit, 1e-9)
	assert.InDelta(t, 1.41, found[0].OutOfTheMoney, 0.01)
}

// A leg the book will not price two-sided is absent, never ranked low. Half a
// price is worse than no price: it reads as a bargain.
func TestALegWithoutATwoSidedQuoteIsNotOffered(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	for _, broken := range []map[string]marketdata.Quote{
		{put(701).Symbol: quote(0.71, 0.79)},
		{put(701).Symbol: quote(0.71, 0.79), put(700).Symbol: quote(0, 0.59)},
		{put(701).Symbol: quote(0, 0.79), put(700).Symbol: quote(0.51, 0.59)},
	} {
		assert.Empty(t, Best("QQQ", 710, contracts, broken, anything()))
	}
}

// The filters are what make the list short enough to read.
func TestWhatDoesNotClearTheFiltersIsLeftOut(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol: quote(0.71, 0.79),
		put(700).Symbol: quote(0.51, 0.59),
	}

	tooDear := anything()
	tooDear.MaxCostShare = 50
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, tooDear), "80% of the credit is dearer than 50%")

	tooCheap := anything()
	tooCheap.MinCreditToRisk = 30
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, tooCheap), "25% pays less than 30%")

	tooNear := anything()
	tooNear.MinOutOfTheMoney = 2
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, tooNear), "1.27% is nearer than 2%")
}

// Among several strikes the best is the one paying most for its risk, and both
// sides are offered so the session can choose a direction.
func TestTheBestOfEachSideIsOffered(t *testing.T) {
	contracts := []marketdata.Contract{put(698), put(699), put(700), put(701), call(720), call(721)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol:  quote(0.71, 0.79),
		put(700).Symbol:  quote(0.51, 0.59),
		put(699).Symbol:  quote(0.36, 0.44),
		put(698).Symbol:  quote(0.06, 0.14),
		call(720).Symbol: quote(0.60, 0.70),
		call(721).Symbol: quote(0.40, 0.50),
	}

	found := Best("QQQ", 710, contracts, quotes, anything())
	require.Len(t, found, 2, "one put and one call")
	assert.GreaterOrEqual(t, found[0].CreditToRisk, found[1].CreditToRisk, "richest first")

	kinds := map[string]bool{found[0].Type: true, found[1].Type: true}
	assert.True(t, kinds["put"] && kinds["call"])
}

func TestAnUnknownPriceOffersNothing(t *testing.T) {
	assert.Empty(t, Best("QQQ", 0, []marketdata.Contract{put(700), put(701)}, nil, anything()))
}
