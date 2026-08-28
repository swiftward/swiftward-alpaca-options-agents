package placement

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// book is a chain built to order, so a test can say what the market looks like
// and then ask what the scorer makes of it.
type book struct {
	spot     float64
	closes   []float64
	expiry   time.Time
	strikes  []float64
	kind     string
	priceFor func(strike float64) (bid, ask float64)
	asked    int
}

func (b *book) LastTrades(context.Context, []string) (map[string]float64, error) {
	return map[string]float64{"SPY": b.spot}, nil
}

func (b *book) DailyCloses(context.Context, string, int) ([]float64, error) {
	return b.closes, nil
}

func (b *book) Chain(_ context.Context, _ string, low, high float64, _ time.Time, _ int) ([]marketdata.Contract, map[string]marketdata.Quote, error) {
	b.asked++
	var contracts []marketdata.Contract
	quotes := map[string]marketdata.Quote{}
	for _, strike := range b.strikes {
		if strike < low || strike > high {
			continue
		}
		symbol := fmt.Sprintf("SPY%s%s%08.0f", b.expiry.Format("060102"), map[string]string{"call": "C", "put": "P"}[b.kind], strike*1000)
		contracts = append(contracts, marketdata.Contract{
			Symbol: symbol, Expiration: b.expiry, Strike: strike, Type: b.kind,
		})
		bid, ask := b.priceFor(strike)
		quotes[symbol] = marketdata.Quote{Symbol: symbol, Bid: bid, Ask: ask}
	}

	return contracts, quotes, nil
}

// walk builds a price history with a known daily volatility, so sigma has an
// answer the test can check against rather than accept.
func walk(days int, start, dailyVol float64) []float64 {
	source := rand.New(rand.NewSource(7))
	out := make([]float64, days)
	price := start
	for i := range out {
		price *= math.Exp(source.NormFloat64() * dailyVol)
		out[i] = price
	}

	return out
}

func aBook(kind string) *book {
	expiry := time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC)
	strikes := make([]float64, 0, 120)
	for strike := 720.0; strike <= 840.0; strike++ {
		strikes = append(strikes, strike)
	}

	return &book{
		spot: 771.0, closes: walk(900, 700, 0.0066), expiry: expiry,
		strikes: strikes, kind: kind,
		// A price that falls off with distance, with a cent of spread. The shape
		// matters and the model behind it does not: the scorer never asks what
		// the option is worth, only what the book will pay.
		priceFor: func(strike float64) (float64, float64) {
			away := math.Abs(strike-771.0) / 10
			mid := 6 * math.Exp(-away*away/2)
			if mid < 0.02 {
				mid = 0.02
			}
			return mid - 0.005, mid + 0.005
		},
	}
}

func anAsk() Ask {
	return Ask{
		Underlying: "SPY", Expiration: time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC),
		Kind: "call", Bought: 2,
		ShortLeastSigma: 1.5, ValleyLeastSigma: 2.5, ShortMostSigma: 4,
		WorstCaseMost: 2000, Most: 5,
	}
}

func aScorer(with *book) Scorer {
	return Scorer{
		Market: with, History: 900,
		Now: func() time.Time { return time.Date(2026, 8, 31, 15, 0, 0, 0, time.UTC) },
	}
}

// The valley is where this whole package earns its place: nothing else the agent
// can call knows where the worst case sits.
func TestEveryPlacementRespectsTheLimitsItWasGiven(t *testing.T) {
	held := aBook("call")
	answer, err := aScorer(held).Score(context.Background(), anAsk())
	require.NoError(t, err)
	require.NotEmpty(t, answer.Placements, "a chain this wide has permitted placements in it")

	for _, at := range answer.Placements {
		assert.GreaterOrEqual(t, at.ShortSigma, 1.5, "a sold leg closer than asked would be the mistake of 28 August")
		assert.GreaterOrEqual(t, at.ValleySigma, 2.5, "the valley is the whole reason to ask")
		assert.LessOrEqual(t, at.ShortSigma, 4.0)
		assert.Greater(t, at.LongStrike, at.ShortStrike, "the bought leg of a call backspread sits further out")
		assert.LessOrEqual(t, -at.WorstCase, 2000.0, "the ceiling is what sizes it")
		assert.GreaterOrEqual(t, at.Sets, 1)
	}
}

// The ranking is by expectation, and a caller reading the first row must be able
// to trust that nothing below it is better.
func TestTheBestPlacementComesFirst(t *testing.T) {
	answer, err := aScorer(aBook("call")).Score(context.Background(), anAsk())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(answer.Placements), 2)

	for i := 1; i < len(answer.Placements); i++ {
		assert.GreaterOrEqual(t, answer.Placements[i-1].Expected, answer.Placements[i].Expected)
	}
	assert.LessOrEqual(t, len(answer.Placements), 5, "Most is a promise about the size of the answer")
	assert.Greater(t, answer.Considered, len(answer.Placements),
		"a caller is entitled to know how much was looked at, not only what came back")
}

// Sigma is the unit everything else is quoted in. If it is wrong, every distance
// is wrong and the limits mean nothing.
func TestSigmaIsTheMoveExpectedByExpiry(t *testing.T) {
	answer, err := aScorer(aBook("call")).Score(context.Background(), anAsk())
	require.NoError(t, err)

	assert.Equal(t, 4, answer.TradingDays, "Monday afternoon to Friday is four trading days")
	// The walk was built at 0.66% a day, which is about 10.5% a year.
	assert.InDelta(t, 0.105, answer.Volatility, 0.03)
	assert.InDelta(t, answer.Volatility*math.Sqrt(4.0/252)*answer.Price, answer.Sigma, 0.01)
	assert.Greater(t, answer.Windows, 60, "a number needs a sample under it")
}

// A put backspread is the mirror image. Measuring its strikes the way a call's
// are measured would make every distance negative and refuse the whole chain.
func TestAPutBackspreadIsMeasuredOnItsOwnSide(t *testing.T) {
	ask := anAsk()
	ask.Kind = "put"
	answer, err := aScorer(aBook("put")).Score(context.Background(), ask)
	require.NoError(t, err)
	require.NotEmpty(t, answer.Placements, "the mirror of a permitted call placement is a permitted put one")

	for _, at := range answer.Placements {
		assert.Less(t, at.LongStrike, at.ShortStrike, "the bought leg of a put backspread sits LOWER")
		assert.GreaterOrEqual(t, at.ValleySigma, 2.5)
	}
}

// A contract quoted on one side is not a price. Building on it would report a
// credit nobody pays.
func TestALegQuotedOnOneSideIsNotUsed(t *testing.T) {
	held := aBook("call")
	held.priceFor = func(strike float64) (float64, float64) {
		if int(strike)%2 == 0 {
			return 0, 1.0 // ничего на покупке: за этой ценой никто не стоит
		}
		away := math.Abs(strike-771.0) / 10
		mid := 6 * math.Exp(-away*away/2)
		if mid < 0.02 {
			mid = 0.02
		}
		return mid - 0.005, mid + 0.005
	}

	answer, err := aScorer(held).Score(context.Background(), anAsk())
	require.NoError(t, err)
	for _, at := range answer.Placements {
		assert.NotZero(t, int(at.ShortStrike)%2, "an even strike had no bid and must not appear")
		assert.NotZero(t, int(at.LongStrike)%2)
	}
}

// The refusals are as much of the answer as the placements. Each says which
// question the caller has to fix.
func TestItRefusesWhatItCannotAnswer(t *testing.T) {
	for name, broken := range map[string]func(*Ask){
		"no underlying":        func(a *Ask) { a.Underlying = "" },
		"neither call nor put": func(a *Ask) { a.Kind = "spread" },
		"one for one":          func(a *Ask) { a.Bought = 1 },
		"no ceiling":           func(a *Ask) { a.WorstCaseMost = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			ask := anAsk()
			broken(&ask)
			_, err := aScorer(aBook("call")).Score(context.Background(), ask)
			require.Error(t, err)
		})
	}
}

// An expiration already past has no window to replay, and answering anyway would
// be answering about nothing.
func TestAnExpirationWithNoWindowLeftIsRefused(t *testing.T) {
	held := aBook("call")
	scorer := Scorer{
		Market: held, History: 900,
		Now: func() time.Time { return time.Date(2026, 9, 5, 15, 0, 0, 0, time.UTC) },
	}
	_, err := scorer.Score(context.Background(), anAsk())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no window left")
}

// The measure that made this package worth writing: a placement whose expectation
// lives in the top one percent of history is a lottery ticket, and it must say so.
func TestItSaysHowMuchOfTheExpectationComesFromTheTail(t *testing.T) {
	answer, err := aScorer(aBook("call")).Score(context.Background(), anAsk())
	require.NoError(t, err)

	for _, at := range answer.Placements {
		if at.Expected <= 0 {
			continue
		}
		assert.False(t, math.IsNaN(at.FromTopPercent), "a placement in the black owes this number")
		assert.Greater(t, at.FromTopPercent, 0.0)
	}
}

// History a session cannot stand on must be refused rather than averaged over
// whatever happens to be there.
func TestTooLittleHistoryIsRefused(t *testing.T) {
	held := aBook("call")
	held.closes = walk(40, 700, 0.0066)
	_, err := aScorer(held).Score(context.Background(), anAsk())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "history")
}
