//go:build broker

package marketdata

import (
	"context"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The strategy rests on one fact about this broker: an option loses its greeks on
// the day it expires, and keeps them while it still has a day to live. Measured
// on 25 August 2026 at 11:25 New York - zero strikes with greeks at that day's
// expiry on SPY, QQQ and IWM, twenty on the next.
//
// If that ever changes, the entry rule can go back to same-day expiry and this
// test says so by failing. It places nothing and needs the market open.
func TestGreeksLiveOnlyBeyondExpiryDay(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL: this tier has nothing to say without a broker")

	ctx := context.Background()
	broker := NewBroker(url)

	open, err := broker.MarketOpen(ctx)
	require.NoError(t, err)
	if !open {
		t.Skip("the market is shut: quotes are yesterday's and greeks are absent for a reason " +
			"that has nothing to do with the expiration")
	}

	prices, err := broker.LastTrades(ctx, []string{"SPY"})
	require.NoError(t, err)
	price := prices["SPY"]
	require.Positive(t, price)

	contracts, err := broker.ContractsAround(ctx, "SPY", price*0.99, price*0.013, time.Now(), 200)
	require.NoError(t, err)

	today, next := expirationsFrom(contracts)
	require.NotEmpty(t, today, "no SPY put expiring today")
	require.NotEmpty(t, next, "no SPY put expiring after today")

	assert.Zero(t, greeksAmong(ctx, t, broker, today),
		"same-day options carry no greeks on this account; the entry rule reads delta and must not use them")
	assert.Positive(t, greeksAmong(ctx, t, broker, next),
		"the next expiration must carry greeks, or the entry rule has nothing to choose a strike by")
}

// The nearest structure the engine would sell, priced as the session prices it.
func TestTheNextExpirationPaysEnoughToSell(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	open, err := broker.MarketOpen(ctx)
	require.NoError(t, err)
	if !open {
		t.Skip("the market is shut")
	}

	for _, underlying := range []string{"SPY", "QQQ", "IWM"} {
		prices, err := broker.LastTrades(ctx, []string{underlying})
		require.NoError(t, err)
		price := prices[underlying]
		require.Positive(t, price)

		contracts, err := broker.ContractsAround(ctx, underlying, price*0.99, price*0.013, time.Now(), 200)
		require.NoError(t, err)
		_, next := expirationsFrom(contracts)
		require.NotEmpty(t, next)

		symbols := make([]string, 0, len(next))
		for _, put := range next {
			symbols = append(symbols, put.Symbol)
		}
		quotes, err := broker.Quotes(ctx, symbols)
		require.NoError(t, err)

		best, index := math.MaxFloat64, -1
		for i, put := range next {
			quote, answered := quotes[put.Symbol]
			if !answered || quote.Delta == nil || quote.Bid <= 0 {
				continue
			}
			if distance := math.Abs(*quote.Delta + 0.15); distance < best {
				best, index = distance, i
			}
		}
		require.Positive(t, index, "%s: no short leg near delta -0.15 on the next expiration", underlying)

		short, long := next[index], next[index-1]
		shortQuote, longQuote := quotes[short.Symbol], quotes[long.Symbol]
		require.Positive(t, longQuote.Ask, "%s: no long leg under %.1f", underlying, short.Strike)

		width := short.Strike - long.Strike
		credit := (shortQuote.Bid+shortQuote.Ask)/2 - (longQuote.Bid+longQuote.Ask)/2
		risk := width - credit

		t.Logf("%s %.2f: short %.1f (delta %.3f) / long %.1f expiring %s, credit %.3f risk %.3f = %.1f%%",
			underlying, price, short.Strike, *shortQuote.Delta, long.Strike,
			short.Expiration.Format(time.DateOnly), credit, risk, credit/risk*100)

		assert.Positive(t, credit, "%s: the structure the engine sells must pay something", underlying)
	}
}

// expirationsFrom splits the puts into the ones expiring today and the ones
// expiring on the first day after that.
func expirationsFrom(contracts []Contract) (today, next []Contract) {
	day := time.Now().Format(time.DateOnly)

	var after string
	for _, contract := range contracts {
		if contract.Type != "put" {
			continue
		}
		when := contract.Expiration.Format(time.DateOnly)
		if when > day && (after == "" || when < after) {
			after = when
		}
	}

	for _, contract := range contracts {
		if contract.Type != "put" {
			continue
		}
		switch contract.Expiration.Format(time.DateOnly) {
		case day:
			today = append(today, contract)
		case after:
			next = append(next, contract)
		}
	}
	sort.Slice(next, func(i, j int) bool { return next[i].Strike < next[j].Strike })

	return today, next
}

func greeksAmong(ctx context.Context, t *testing.T, broker *Broker, contracts []Contract) int {
	t.Helper()

	symbols := make([]string, 0, len(contracts))
	for _, contract := range contracts {
		symbols = append(symbols, contract.Symbol)
	}
	quotes, err := broker.Quotes(ctx, symbols)
	require.NoError(t, err)

	found := 0
	for _, quote := range quotes {
		if quote.Delta != nil {
			found++
		}
	}

	return found
}
