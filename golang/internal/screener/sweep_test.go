package screener

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

type brokerDouble struct {
	open      bool
	prices    map[string]float64
	contracts map[string][]marketdata.Contract
	quotes    map[string]marketdata.Quote
	calls     int
	priceAsks [][]string
}

func (b *brokerDouble) MarketOpen(context.Context) (bool, error) { return b.open, nil }

func (b *brokerDouble) LastTrades(_ context.Context, symbols []string) (map[string]float64, error) {
	b.calls++
	b.priceAsks = append(b.priceAsks, symbols)
	out := map[string]float64{}
	for _, symbol := range symbols {
		if price, known := b.prices[symbol]; known {
			out[symbol] = price
		}
	}
	return out, nil
}

// Chain answers with the contracts inside the strike window and only their
// quotes, in ONE call - the same bargain the real broker offers, which is what
// makes the call count in these tests mean anything.
func (b *brokerDouble) Chain(_ context.Context, underlying string, low, high float64,
	until time.Time, _ int) ([]marketdata.Contract, map[string]marketdata.Quote, error) {

	b.calls++
	var contracts []marketdata.Contract
	quotes := map[string]marketdata.Quote{}
	for _, contract := range b.contracts[underlying] {
		if contract.Strike < low || contract.Strike > high {
			continue
		}
		if contract.Expiration.After(until) {
			continue
		}
		contracts = append(contracts, contract)
		if quote, known := b.quotes[contract.Symbol]; known {
			quotes[contract.Symbol] = quote
		}
	}

	return contracts, quotes, nil
}

type keeperDouble struct{ kept []Candidate }

func (k *keeperDouble) ReplaceCandidates(_ context.Context, _ time.Time, found []Candidate) error {
	k.kept = found
	return nil
}

func sweeping(broker Broker, kept Keeper, now func() time.Time, t *testing.T) *Sweep {
	return &Sweep{
		Broker: broker, Universe: []string{"QQQ"}, Wanted: anything(),
		Every: time.Minute, Record: kept, PerMinute: 200, Expirations: 5,
		Now: now, Log: zaptest.NewLogger(t),
	}
}

func TestASweepLeavesWhatItFound(t *testing.T) {
	at := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	broker := &brokerDouble{
		open:      true,
		prices:    map[string]float64{"QQQ": 710},
		contracts: map[string][]marketdata.Contract{"QQQ": {put(700), put(701)}},
		quotes: map[string]marketdata.Quote{
			put(701).Symbol: quote(0.71, 0.79),
			put(700).Symbol: quote(0.51, 0.59),
		},
	}
	kept := &keeperDouble{}

	sweeping(broker, kept, func() time.Time { return at }, t).once(context.Background())

	require.Len(t, kept.kept, 1)
	assert.Equal(t, "QQQ", kept.kept[0].Underlying)
	assert.InDelta(t, 25, kept.kept[0].CreditToRisk, 0.01)
}

// A shut market is not swept: the quotes would be yesterday's, and a session
// reading them would act on prices nobody will trade at.
func TestAShutMarketIsNotSwept(t *testing.T) {
	at := time.Date(2026, 8, 26, 2, 0, 0, 0, time.UTC)
	broker := &brokerDouble{open: false, prices: map[string]float64{"QQQ": 710}}
	kept := &keeperDouble{}

	sweeping(broker, kept, func() time.Time { return at }, t).once(context.Background())

	assert.Zero(t, broker.calls, "not one request goes out")
	assert.Nil(t, kept.kept)
}

// Prices are asked for in batches. One request per underlying would spend the
// whole rate limit on prices and leave nothing for the structures.
func TestPricesAreAskedForInBatches(t *testing.T) {
	at := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	universe := make([]string, 0, 45)
	prices := map[string]float64{}
	for i := range 45 {
		symbol := string(rune('A'+i/26)) + string(rune('A'+i%26))
		universe = append(universe, symbol)
		prices[symbol] = 100
	}

	broker := &brokerDouble{open: true, prices: prices}
	sweep := sweeping(broker, &keeperDouble{}, func() time.Time { return at }, t)
	sweep.Universe = universe
	sweep.once(context.Background())

	assert.Len(t, broker.priceAsks, 3, "45 names in batches of 20 is three requests")
	assert.Len(t, broker.priceAsks[0], 20)
	assert.Len(t, broker.priceAsks[2], 5)
}

// The broker's limit is respected by waiting, not by skipping: a refusal loses
// the name, a wait only postpones it.
func TestTheSweepWaitsRatherThanExceedTheLimit(t *testing.T) {
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	sweep := sweeping(&brokerDouble{open: true}, nil, func() time.Time { return now }, t)
	sweep.PerMinute = 3

	for range 3 {
		sweep.wait(context.Background())
	}
	assert.Equal(t, 3, sweep.spent, "three spent, none waited for")

	// The fourth would exceed it; with a cancelled context it returns at once and
	// starts a fresh minute rather than going over.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sweep.wait(ctx)
	assert.Equal(t, 1, sweep.spent, "the count restarts in the new minute")
}

// A minute passing resets the allowance without any waiting at all.
func TestTheAllowanceRefillsEachMinute(t *testing.T) {
	now := time.Date(2026, 8, 26, 14, 0, 0, 0, time.UTC)
	sweep := sweeping(&brokerDouble{open: true}, nil, func() time.Time { return now }, t)
	sweep.PerMinute = 2

	sweep.wait(context.Background())
	sweep.wait(context.Background())
	require.Equal(t, 2, sweep.spent)

	now = now.Add(time.Minute)
	sweep.wait(context.Background())
	assert.Equal(t, 1, sweep.spent, "a new minute is a fresh allowance")
}

func TestASweepWithoutItsSettingsRefusesToRun(t *testing.T) {
	for _, broken := range []*Sweep{
		{},
		{Broker: &brokerDouble{}},
		{Broker: &brokerDouble{}, Universe: []string{"QQQ"}},
		{Broker: &brokerDouble{}, Universe: []string{"QQQ"}, Every: time.Minute},
		{Broker: &brokerDouble{}, Universe: []string{"QQQ"}, Every: time.Minute, PerMinute: 200},
	} {
		broken.Log = zaptest.NewLogger(t)
		assert.Error(t, broken.Run(context.Background()))
	}
}

// What a sweep costs the broker, counted rather than assumed.
//
// The rate limit is the sweep's only real constraint - 180 requests a minute -
// so the number of requests per underlying IS the reach of the whole screener.
// It was three: one batch of prices, then a contract list and a snapshot for
// each name. The chain brings the last two back together, so it is two, and the
// same limit reaches twice as far.
func TestOneSweepSpendsTwoRequestsPerUnderlying(t *testing.T) {
	broker := &brokerDouble{
		open:   true,
		prices: map[string]float64{"QQQ": 710, "SPY": 765},
		contracts: map[string][]marketdata.Contract{
			"QQQ": {put(700), put(701)},
			"SPY": {put(755), put(756)},
		},
		quotes: map[string]marketdata.Quote{
			put(701).Symbol: with(quote(0.71, 0.79), -0.14),
			put(700).Symbol: with(quote(0.51, 0.59), -0.12),
			put(756).Symbol: with(quote(0.71, 0.79), -0.14),
			put(755).Symbol: with(quote(0.51, 0.59), -0.12),
		},
	}
	keeper := &keeperDouble{}
	sweeping(t, broker, keeper, []string{"QQQ", "SPY"})

	// One batch of prices for both names, then one chain each.
	assert.Equal(t, 3, broker.calls,
		"a third request per underlying is a third of the universe given up")
}
