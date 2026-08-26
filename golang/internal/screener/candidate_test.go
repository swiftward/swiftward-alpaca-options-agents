package screener

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

var expiry = time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)

// now is a fixed clock two days before the fixtures expire.
var now = time.Date(2026, 8, 26, 17, 15, 0, 0, time.UTC)

func put(strike float64) marketdata.Contract {
	return marketdata.Contract{Symbol: name("P", strike), Expiration: expiry, Strike: strike, Type: "put"}
}

// putOn is the same put on another expiration, so a test can hold two series at
// once and see how they compete.
func putOn(strike float64, day time.Time) marketdata.Contract {
	c := put(strike)
	c.Expiration = day
	c.Symbol = "X" + c.Symbol

	return c
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

func with(q marketdata.Quote, delta float64) marketdata.Quote {
	q.Delta = &delta
	return q
}

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

	found := Best("QQQ", 710, contracts, quotes, now, anything(), Refused{})
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

	found := Best("QQQ", 710, contracts, quotes, now, anything(), Refused{})
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
		assert.Empty(t, Best("QQQ", 710, contracts, broken, now, anything(), Refused{}))
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
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, now, tooDear, Refused{}), "80% of the credit is dearer than 50%")

	tooCheap := anything()
	tooCheap.MinCreditToRisk = 30
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, now, tooCheap, Refused{}), "25% pays less than 30%")

	tooNear := anything()
	tooNear.MinOutOfTheMoney = 2
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, now, tooNear, Refused{}), "1.27% is nearer than 2%")
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

	found := Best("QQQ", 710, contracts, quotes, now, anything(), Refused{})
	require.Len(t, found, 2, "one put and one call")
	assert.GreaterOrEqual(t, found[0].CreditToRisk, found[1].CreditToRisk, "richest first")

	kinds := map[string]bool{found[0].Type: true, found[1].Type: true}
	assert.True(t, kinds["put"] && kinds["call"])
}

func TestAnUnknownPriceOffersNothing(t *testing.T) {
	assert.Empty(t, Best("QQQ", 0, []marketdata.Contract{put(700), put(701)}, nil, now, anything(), Refused{}))
}

// A structure that pays more than it risks is not an opportunity, it is a broken
// quote - and the list is ranked by exactly the number bad data inflates, so
// without this the worst data arrives first. The first live sweep offered a TSLA
// call paying 468 percent: two and a half dollars wide, quoted at a credit of
// 2.06, with the sold strike out of the money.
func TestAStructurePayingMoreThanItRisksIsReadAsBrokenData(t *testing.T) {
	// Three dollars wide, quoted at a credit of 1.98: the risk left is 1.02, so it
	// claims to pay 194 percent. Positive risk, so nothing else rejects it - only
	// the bound does, which is what makes this test worth having.
	contracts := []marketdata.Contract{call(350), call(353)}
	quotes := map[string]marketdata.Quote{
		call(350).Symbol: quote(1.98, 2.02),
		call(353).Symbol: quote(0.01, 0.03),
	}

	want := anything()
	loose := want
	loose.MostCreditToRisk = 0
	require.Len(t, Best("TSLA", 349, contracts, quotes, now, loose, Refused{}), 1,
		"without the bound this garbage is offered, and offered first")

	want.MostCreditToRisk = 100
	assert.Empty(t, Best("TSLA", 349, contracts, quotes, now, want, Refused{}),
		"a three-dollar-wide spread quoted at a credit of 1.98 is not a gift")

	// The same filter must not throw away what is merely generous.
	honest := []marketdata.Contract{put(700), put(701)}
	honestQuotes := map[string]marketdata.Quote{
		put(701).Symbol: quote(0.71, 0.79),
		put(700).Symbol: quote(0.51, 0.59),
	}
	assert.Len(t, Best("QQQ", 710, honest, honestQuotes, now, want, Refused{}), 1, "25 percent is ordinary and stays")
}

// Distance in percent and likelihood are not the same measure: a strike one
// percent away is far on a quiet index and near on a share that moves five
// percent in a day. Ranking on distance alone offered the session structures at
// delta 0.47 against a rule that wants 0.15, and it threw every one away - a
// list nobody can act on costs a turn to reject and is worse than no list.
func TestAStrikeTooLikelyToBeCrossedIsNotOffered(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	near, far := -0.47, -0.14
	quotes := func(delta float64) map[string]marketdata.Quote {
		short := quote(0.71, 0.79)
		short.Delta = &delta
		return map[string]marketdata.Quote{
			put(701).Symbol: short,
			put(700).Symbol: quote(0.51, 0.59),
		}
	}

	want := anything()
	want.MostDelta = 0.25

	assert.Empty(t, Best("QQQ", 710, contracts, quotes(near), now, want, Refused{}), "0.47 is likelier than the rule accepts")

	found := Best("QQQ", 710, contracts, quotes(far), now, want, Refused{})
	require.Len(t, found, 1, "0.14 is what the rule wants")
	require.NotNil(t, found[0].Delta)
	assert.InDelta(t, -0.14, *found[0].Delta, 1e-9, "the session is told the delta, not left to ask again")
}

// The broker computes no delta on the day a contract expires, so a delta ceiling
// has nothing to apply and is skipped rather than read as a refusal. The whole
// expiry-day book arrives this way, and it is the book that pays most on the day.
//
// The ceiling still bites where there IS a delta: skipping is about absence, not
// about relaxing the rule.
func TestADeltaCeilingIsSkippedWhereThereIsNoDelta(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	blind := map[string]marketdata.Quote{
		put(701).Symbol: quote(0.71, 0.79),
		put(700).Symbol: quote(0.51, 0.59),
	}

	want := anything()
	want.MostDelta = 0.25
	found := Best("QQQ", 710, contracts, blind, now, want, Refused{})
	require.Len(t, found, 1)
	assert.Nil(t, found[0].Delta)

	seeing := map[string]marketdata.Quote{
		put(701).Symbol: with(quote(0.71, 0.79), -0.40),
		put(700).Symbol: with(quote(0.51, 0.59), -0.35),
	}
	refused := Refused{}
	assert.Empty(t, Best("QQQ", 710, contracts, seeing, now, want, refused),
		"0.40 is past the ceiling of 0.25")
	assert.Equal(t, 1, refused[RefusedDelta])
}

// Two legs of different expirations are a calendar spread, not a vertical, and
// pricing one as the other invents a risk that does not exist: the width between
// strikes bounds the loss only when both legs die on the same day. The session
// caught this on live data - "смешанные по срокам пары из списка не использую
// как вертикали" - which means the screener was offering them.
func TestLegsOfDifferentExpirationsAreNeverPaired(t *testing.T) {
	later := expiry.AddDate(0, 0, 3)
	near := marketdata.Contract{Symbol: "QQQ-NEAR-701", Expiration: expiry, Strike: 701, Type: "put"}
	far := marketdata.Contract{Symbol: "QQQ-FAR-700", Expiration: later, Strike: 700, Type: "put"}

	quotes := map[string]marketdata.Quote{
		near.Symbol: quote(0.71, 0.79),
		far.Symbol:  quote(0.51, 0.59),
	}

	assert.Empty(t, Best("QQQ", 710, []marketdata.Contract{far, near}, quotes, now, anything(), Refused{}),
		"one leg on each of two days is not a vertical and must not be offered as one")

	// The same two strikes on the SAME day are a vertical and are offered.
	sameDay := marketdata.Contract{Symbol: "QQQ-NEAR-700", Expiration: expiry, Strike: 700, Type: "put"}
	quotes[sameDay.Symbol] = quote(0.51, 0.59)
	found := Best("QQQ", 710, []marketdata.Contract{sameDay, near}, quotes, now, anything(), Refused{})
	require.Len(t, found, 1)
	assert.Equal(t, expiry, found[0].Expiration)
}

// Neither half decides alone. A delta ceiling keeps what is far and throws away
// what pays; a credit threshold keeps what pays and ignores how often it loses.
// On 26 August the delta ceiling of 0.25 rejected ORCL, TSLA and INTC - all three
// paying more than the same market said their risk was worth - while keeping
// nothing better.
func TestWhatPaysAboveWhatItMustSurviveIsRankedFirst(t *testing.T) {
	// Two structures at the SAME delta: one paying 50% of risk, one paying 25%.
	// At delta 0.30 the strike survives 70% of the time; 50% credit breaks even at
	// 66.7% and 25% credit at 80%. So the first has +3.3 points and the second
	// -10, and no delta rule can tell them apart.
	rich := []marketdata.Contract{put(700), put(701)}
	delta := -0.30
	short := quote(1.00, 1.00)
	short.Delta = &delta
	long := quote(0.665, 0.665)
	long.Delta = &delta

	want := anything()
	want.MostDelta = 0
	found := Best("QQQ", 710, rich, map[string]marketdata.Quote{
		put(701).Symbol: short, put(700).Symbol: long,
	}, now, want, Refused{})
	require.Len(t, found, 1)
	require.NotNil(t, found[0].Edge)
	assert.Greater(t, *found[0].Edge, 0.0, "paid 50 percent of risk at a 30 percent chance of losing")

	// A long leg priced NEARER the short one leaves less credit: 0.90 against 1.00
	// pays a tenth of the width, which breaks even at 90 percent while the strike
	// survives 70 - twenty points short.
	poor := quote(0.90, 0.90)
	poor.Delta = &delta
	thin := Best("QQQ", 710, rich, map[string]marketdata.Quote{
		put(701).Symbol: short, put(700).Symbol: poor,
	}, now, want, Refused{})
	require.Len(t, thin, 1)
	require.NotNil(t, thin[0].Edge)
	assert.Less(t, *thin[0].Edge, *found[0].Edge, "the same delta, less paid, less edge")
}

// A floor on it keeps out what the same market prices as not worth its risk.
func TestAStructurePayingLessThanItsRiskIsWorthIsLeftOut(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	delta := -0.30
	short := quote(1.00, 1.00)
	short.Delta = &delta
	thin := quote(0.80, 0.80)

	want := anything()
	want.MostDelta = 0
	want.LeastEdge = 0.1

	// A fifth of the width at a 30 percent chance of loss: breaks even at 80
	// percent while the strike survives 70, so ten points short.
	assert.Empty(t, Best("QQQ", 710, contracts, map[string]marketdata.Quote{
		put(701).Symbol: short, put(700).Symbol: thin,
	}, now, want, Refused{}))

	// Without delta there is nothing to weigh against, so the structure is listed
	// with no edge rather than dropped. That is the expiry-day book, and dropping
	// it removed the best-paying structures of the day from view.
	noDelta := quote(1.00, 1.00)
	blind := Best("QQQ", 710, contracts, map[string]marketdata.Quote{
		put(701).Symbol: noDelta, put(700).Symbol: quote(0.50, 0.50),
	}, now, want, Refused{})
	require.Len(t, blind, 1)
	assert.Nil(t, blind[0].Edge, "no delta, no edge - and the absence is what the session reads")
	assert.Nil(t, blind[0].Delta)
}

// A structure with no edge to show ranks below every structure that has one,
// however well it pays. The session sees the measured ones first and reads the
// unmeasured tail knowing what it is.
func TestAnUnmeasuredStructureRanksBelowAMeasuredOne(t *testing.T) {
	measured := with(quote(0.60, 0.60), -0.10)
	contracts := []marketdata.Contract{put(700), put(701), call(720), call(721)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol: measured, put(700).Symbol: with(quote(0.50, 0.50), -0.08),
		// Pays far more, and has nothing to weigh it against.
		call(720).Symbol: quote(0.90, 0.90), call(721).Symbol: quote(0.10, 0.10),
	}

	want := anything()
	want.MostDelta = 0.45
	found := Best("QQQ", 710, contracts, quotes, now, want, Refused{})
	require.Len(t, found, 2)
	require.NotNil(t, found[0].Edge, "the measured one comes first")
	assert.Nil(t, found[1].Edge)
}

// The tally has to name the filter that stopped a structure, not merely count
// that something was stopped. A sweep over hundreds of names returning one
// candidate is read completely differently depending on which of these is large,
// and a tally that mislabels its reasons would point at the wrong threshold.
func TestTheTallyNamesTheFilterThatRefused(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol: quote(0.71, 0.79),
		put(700).Symbol: quote(0.51, 0.59),
	}

	dear := anything()
	dear.MaxCostShare = 50
	refused := Refused{}
	require.Empty(t, Best("QQQ", 710, contracts, quotes, now, dear, refused))
	assert.Equal(t, 1, refused[RefusedCost])
	assert.Zero(t, refused[RefusedPaysTooLittle], "one refusal, one reason")

	cheap := anything()
	cheap.MinCreditToRisk = 30
	refused = Refused{}
	require.Empty(t, Best("QQQ", 710, contracts, quotes, now, cheap, refused))
	assert.Equal(t, 1, refused[RefusedPaysTooLittle])

	near := anything()
	near.MinOutOfTheMoney = 2
	refused = Refused{}
	require.Empty(t, Best("QQQ", 710, contracts, quotes, now, near, refused))
	assert.Equal(t, 1, refused[RefusedDistance])

	// A structure that clears everything leaves the tally empty: an instrument
	// that counts a refusal on a candidate it accepted would read as a tight
	// filter forever.
	refused = Refused{}
	require.Len(t, Best("QQQ", 710, contracts, quotes, now, anything(), refused), 1)
	assert.Empty(t, refused)
}

// The whole point of taking the crossing out of the credit: between two
// structures the cheap one wins even when the screen says the dear one pays
// more. A measure computed on the displayed midpoint ranks them the other way
// round, and that is the ranking the sweep of 26 August was handing out.
func TestTheDearStructureLosesToTheCheapOne(t *testing.T) {
	// Same strikes, same delta, same width. One is quoted eight cents wide on the
	// short leg, the other one cent.
	dearContracts := []marketdata.Contract{put(700), put(701)}
	dear := map[string]marketdata.Quote{
		put(701).Symbol: with(quote(0.86, 1.02), -0.15),
		put(700).Symbol: with(quote(0.70, 0.74), -0.12),
	}
	cheapContracts := []marketdata.Contract{put(600), put(601)}
	cheap := map[string]marketdata.Quote{
		put(601).Symbol: with(quote(0.75, 0.77), -0.15),
		put(600).Symbol: with(quote(0.60, 0.62), -0.12),
	}

	want := anything()
	want.MaxCostShare = 200
	want.MinOutOfTheMoney = 0
	want.MaxOutOfTheMoney = 100

	dearFound := Best("QQQ", 710, dearContracts, dear, now, want, Refused{})
	cheapFound := Best("QQQ", 710, cheapContracts, cheap, now, want, Refused{})
	require.Len(t, dearFound, 1)
	require.Len(t, cheapFound, 1)

	assert.Greater(t, dearFound[0].Credit, cheapFound[0].Credit,
		"on the screen the dear one pays more")
	assert.Less(t, dearFound[0].CreditAfterCost, cheapFound[0].CreditAfterCost,
		"after the crossing it pays less")
	require.NotNil(t, dearFound[0].Edge)
	require.NotNil(t, cheapFound[0].Edge)
	assert.Less(t, *dearFound[0].Edge, *cheapFound[0].Edge,
		"so it must also measure worse, which is the only reason the field exists")
}

// A structure whose crossing eats the whole credit is not ranked last, it is
// absent: the arithmetic below it would be computed on a credit that cannot be
// received.
func TestAStructureTheCrossingEatsIsRefused(t *testing.T) {
	contracts := []marketdata.Contract{put(700), put(701)}
	quotes := map[string]marketdata.Quote{
		put(701).Symbol: with(quote(0.10, 1.10), -0.15),
		put(700).Symbol: with(quote(0.05, 0.95), -0.12),
	}
	want := anything()
	want.MaxCostShare = 10000
	want.MinOutOfTheMoney = 0
	want.MaxOutOfTheMoney = 100

	refused := Refused{}
	assert.Empty(t, Best("QQQ", 710, contracts, quotes, now, want, refused))
	assert.Positive(t, refused[RefusedEatenByCost])
	assert.Zero(t, refused[RefusedCost],
		"the sanity bound and the crossing eating the credit are different findings")
}

// Measured and unmeasured structures do not compete for the same slot. Making
// them compete drops one for a reason that has nothing to do with what it pays:
// an unmeasured structure always ranks below a measured one, so a single
// measured candidate anywhere in the underlying would hide the whole expiry-day
// book - which is the book that pays most on the day.
func TestTheUnmeasuredBookKeepsItsOwnSlot(t *testing.T) {
	measured := []marketdata.Contract{put(700), put(701)}
	// Same series list, a different expiration with no delta on either leg.
	sameDay := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	blind := []marketdata.Contract{putOn(700, sameDay), putOn(701, sameDay)}

	quotes := map[string]marketdata.Quote{
		put(701).Symbol: with(quote(0.55, 0.65), -0.10),
		put(700).Symbol: with(quote(0.45, 0.55), -0.08),
		// Pays far more and carries no delta at all.
		blind[1].Symbol: quote(0.85, 0.87),
		blind[0].Symbol: quote(0.45, 0.47),
	}

	want := anything()
	want.MostDelta = 0.45
	found := Best("QQQ", 710, append(measured, blind...), quotes, now, want, Refused{})

	require.Len(t, found, 2, "the measured put and the unmeasured put, not one of them")
	require.NotNil(t, found[0].Edge, "the measured one is shown first")
	require.Nil(t, found[1].Edge)
	assert.Greater(t, found[1].CreditToRisk, found[0].CreditToRisk,
		"and the one that pays more is the one that would have been dropped")
}
