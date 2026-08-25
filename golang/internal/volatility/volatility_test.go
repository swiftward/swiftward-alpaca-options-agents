package volatility

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

type marketDouble struct {
	open      bool
	price     float64
	contracts []marketdata.Contract
	quotes    map[string]marketdata.Quote
	asked     []string
}

func (m *marketDouble) MarketOpen(context.Context) (bool, error) { return m.open, nil }

func (m *marketDouble) LastTrades(_ context.Context, symbols []string) (map[string]float64, error) {
	prices := map[string]float64{}
	for _, symbol := range symbols {
		if m.price > 0 {
			prices[symbol] = m.price
		}
	}
	return prices, nil
}

func (m *marketDouble) ContractsAround(_ context.Context, _ string, _, _ float64, _ time.Time, _ int) ([]marketdata.Contract, error) {
	return m.contracts, nil
}

func (m *marketDouble) Quotes(_ context.Context, symbols []string) (map[string]marketdata.Quote, error) {
	m.asked = append(m.asked, symbols...)
	return m.quotes, nil
}

type storeDouble struct {
	mu      sync.Mutex
	samples []Sample
}

func (s *storeDouble) Append(_ context.Context, sample Sample) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, sample)
	return nil
}

func (s *storeDouble) Summarise(context.Context, string, time.Time) (Summary, error) {
	return Summary{}, fmt.Errorf("not asked for in this test")
}

func (s *storeDouble) written() []Sample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Sample(nil), s.samples...)
}

func expiring(day string) time.Time {
	at, err := time.Parse(time.DateOnly, day)
	if err != nil {
		panic(err)
	}
	return at
}

func iv(value float64) *float64 { return &value }

func TestItRecordsTheContractClosestToTheMoney(t *testing.T) {
	market := &marketDouble{
		open:  true,
		price: 763.65,
		contracts: []marketdata.Contract{
			{Symbol: "SPY260825C00762000", Expiration: expiring("2026-08-25"), Strike: 762, Type: "call"},
			{Symbol: "SPY260825C00764000", Expiration: expiring("2026-08-25"), Strike: 764, Type: "call"},
			{Symbol: "SPY260825P00764000", Expiration: expiring("2026-08-25"), Strike: 764, Type: "put"},
			{Symbol: "SPY260901C00764000", Expiration: expiring("2026-09-01"), Strike: 764, Type: "call"},
		},
		quotes: map[string]marketdata.Quote{
			"SPY260825P00764000": {Symbol: "SPY260825P00764000", Bid: 1.4, Ask: 1.5, ImpliedVolatility: iv(0.1201)},
		},
	}
	store := &storeDouble{}
	recorder := &Recorder{
		Market: market, Store: store, Underlyings: []string{"SPY"},
		Every: time.Hour, Now: func() time.Time { return time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC) },
		Log: zaptest.NewLogger(t),
	}

	recorder.readOnce(context.Background())

	written := store.written()
	require.Len(t, written, 1, "one reading per underlying, and always the same kind of contract")
	assert.Equal(t, "SPY260825P00764000", written[0].Contract)
	assert.Equal(t, 763.65, written[0].UnderlyingPrice)
	assert.Equal(t, expiring("2026-08-25"), written[0].Expiration)
}

// A one-sided quote carries no implied volatility. Storing a zero would put a
// price in the history that the market never charged.
func TestAMissingVolatilityIsNotRecordedAsZero(t *testing.T) {
	market := &marketDouble{
		open:  true,
		price: 763.65,
		contracts: []marketdata.Contract{
			{Symbol: "SPY260825P00764000", Expiration: expiring("2026-08-25"), Strike: 764, Type: "put"},
		},
		quotes: map[string]marketdata.Quote{
			"SPY260825P00764000": {Symbol: "SPY260825P00764000", Bid: 0, Ask: 3.4},
		},
	}
	store := &storeDouble{}
	recorder := &Recorder{
		Market: market, Store: store, Underlyings: []string{"SPY"},
		Every: time.Hour, Now: time.Now, Log: zaptest.NewLogger(t),
	}

	recorder.readOnce(context.Background())

	assert.Empty(t, store.written())
}

func TestNothingIsRecordedWhileTheMarketIsShut(t *testing.T) {
	market := &marketDouble{open: false, price: 763.65}
	store := &storeDouble{}
	recorder := &Recorder{
		Market: market, Store: store, Underlyings: []string{"SPY"},
		Every: time.Hour, Now: time.Now, Log: zaptest.NewLogger(t),
	}

	recorder.readOnce(context.Background())

	assert.Empty(t, store.written())
	assert.Empty(t, market.asked, "a shut market is not asked for quotes")
}

func TestARecorderWatchingNothingRefusesToRun(t *testing.T) {
	recorder := &Recorder{
		Market: &marketDouble{}, Store: &storeDouble{},
		Every: time.Minute, Now: time.Now, Log: zaptest.NewLogger(t),
	}

	err := recorder.Run(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "VOLATILITY_UNDERLYINGS")
}

func TestTheSummaryPlacesTheLatestReadingInItsOwnHistory(t *testing.T) {
	base := time.Date(2026, 8, 25, 13, 30, 0, 0, time.UTC)
	summary := Summarise("SPY", base, []Reading{
		{At: base, ImpliedVolatility: 0.10},
		{At: base.Add(time.Hour), ImpliedVolatility: 0.20},
		{At: base.Add(2 * time.Hour), ImpliedVolatility: 0.14},
		{At: base.Add(3 * time.Hour), ImpliedVolatility: 0.15},
	})

	assert.Equal(t, 4, summary.Samples)
	assert.InDelta(t, 0.15, summary.Latest, 1e-9)
	assert.InDelta(t, 0.10, summary.Lowest, 1e-9)
	assert.InDelta(t, 0.20, summary.Highest, 1e-9)
	assert.InDelta(t, 0.145, summary.Median, 1e-9)
	// 0.15 stands above two of the four readings and is one of them: 62.5 out of
	// a hundred, a place in the series rather than a place between its ends.
	assert.InDelta(t, 62.5, summary.Rank, 1e-9)
}

// A series that never moved says nothing about high or low, and answering 0 or
// 100 would read as a signal the market never gave.
func TestAFlatHistoryRanksInTheMiddle(t *testing.T) {
	at := time.Date(2026, 8, 25, 13, 30, 0, 0, time.UTC)
	summary := Summarise("SPY", at, []Reading{
		{At: at, ImpliedVolatility: 0.12},
		{At: at.Add(time.Minute), ImpliedVolatility: 0.12},
	})

	assert.InDelta(t, 50, summary.Rank, 1e-9)
}

func TestAnEmptyHistorySaysSo(t *testing.T) {
	summary := Summarise("SPY", time.Now(), nil)

	assert.Zero(t, summary.Samples)
	assert.Zero(t, summary.Latest)
}

// The contract this project trades expires the same day, and its implied
// volatility swings with the hour. A series built from it measures the clock, so
// the recorder looks three weeks out - and takes the same kind of contract every
// time, because a series that mixes calls with puts moves when the skew moves.
func TestItLooksPastTodaysExpiration(t *testing.T) {
	market := &marketDouble{
		open:  true,
		price: 763.65,
		contracts: []marketdata.Contract{
			{Symbol: "SPY260915P00764000", Expiration: expiring("2026-09-15"), Strike: 764, Type: "put"},
			{Symbol: "SPY260915C00764000", Expiration: expiring("2026-09-15"), Strike: 764, Type: "call"},
			{Symbol: "SPY261015P00763000", Expiration: expiring("2026-10-15"), Strike: 763, Type: "put"},
		},
		quotes: map[string]marketdata.Quote{
			"SPY260915P00764000": {Symbol: "SPY260915P00764000", Bid: 8.4, Ask: 8.6, ImpliedVolatility: iv(0.1502)},
			"SPY260915C00764000": {Symbol: "SPY260915C00764000", Bid: 9.1, Ask: 9.3, ImpliedVolatility: iv(0.1301)},
			"SPY261015P00763000": {Symbol: "SPY261015P00763000", Bid: 13.4, Ask: 13.7, ImpliedVolatility: iv(0.1601)},
		},
	}
	store := &storeDouble{}
	now := time.Date(2026, 8, 25, 14, 0, 0, 0, time.UTC)
	recorder := &Recorder{
		Market: market, Store: store, Underlyings: []string{"SPY"},
		Every: time.Hour, Now: func() time.Time { return now }, Log: zaptest.NewLogger(t),
	}

	recorder.readOnce(context.Background())

	written := store.written()
	require.Len(t, written, 1)
	assert.Equal(t, "SPY260915P00764000", written[0].Contract)
	assert.Equal(t, "put", written[0].OptionType)
	assert.InDelta(t, 0.1502, written[0].ImpliedVolatility, 1e-9)
}
