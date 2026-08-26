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

func (b *brokerDouble) ContractsAround(_ context.Context, underlying string, _, _ float64, _ time.Time, _ int) ([]marketdata.Contract, error) {
	b.calls++
	return b.contracts[underlying], nil
}

func (b *brokerDouble) Quotes(_ context.Context, symbols []string) (map[string]marketdata.Quote, error) {
	b.calls++
	out := map[string]marketdata.Quote{}
	for _, symbol := range symbols {
		if quote, known := b.quotes[symbol]; known {
			out[symbol] = quote
		}
	}
	return out, nil
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
