//go:build broker

package marketdata

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What a same-day expiry pays right now, by distance from the price.
//
// The engine left same-day expiry because at midday on 25 August it paid 3% of
// its risk one percent out, against 10-17% on the next expiration - and because
// the broker computes no greeks on expiry day. But premium burns through the
// day: the same structure at the open may pay several times what it pays at
// noon. This measures it, so the decision to trade the day or only the night is
// made from numbers taken at both hours rather than from one.
//
// Run it at the open and again later; it places nothing.
func TestWhatSameDayExpiryPaysNow(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	open, err := broker.MarketOpen(ctx)
	require.NoError(t, err)
	if !open {
		t.Skip("the market is shut: same-day quotes are yesterday's")
	}

	now := time.Now()
	t.Logf("measured at %s UTC", now.Format("15:04"))

	for _, underlying := range []string{"SPY", "QQQ"} {
		prices, err := broker.LastTrades(ctx, []string{underlying})
		require.NoError(t, err)
		price := prices[underlying]
		require.Positive(t, price)

		contracts, err := broker.ContractsAround(ctx, underlying, price*0.99, price*0.012, now, 120)
		require.NoError(t, err)

		today := now.Format(time.DateOnly)
		var puts []Contract
		for _, contract := range contracts {
			if contract.Type == "put" && contract.Expiration.Format(time.DateOnly) == today {
				puts = append(puts, contract)
			}
		}
		if len(puts) < 2 {
			t.Logf("%s %.2f: nothing expiring today within reach", underlying, price)
			continue
		}
		sort.Slice(puts, func(i, j int) bool { return puts[i].Strike < puts[j].Strike })

		symbols := make([]string, 0, len(puts))
		for _, put := range puts {
			symbols = append(symbols, put.Symbol)
		}
		quotes, err := broker.Quotes(ctx, symbols)
		require.NoError(t, err)

		t.Logf("=== %s at %.2f, expiry today", underlying, price)
		for i := 1; i < len(puts); i++ {
			short, long := puts[i], puts[i-1]
			shortQuote, hasShort := quotes[short.Symbol]
			longQuote, hasLong := quotes[long.Symbol]
			if !hasShort || !hasLong || shortQuote.Bid <= 0 || longQuote.Ask <= 0 {
				continue
			}

			width := short.Strike - long.Strike
			credit := (shortQuote.Bid+shortQuote.Ask)/2 - (longQuote.Bid+longQuote.Ask)/2
			risk := width - credit
			if risk <= 0 {
				continue
			}
			cost := (shortQuote.Ask - shortQuote.Bid) + (longQuote.Ask - longQuote.Bid)

			// Only the distances the engine would consider selling.
			out := (price - short.Strike) / price * 100
			if out < 0.15 || out > 1.2 {
				continue
			}

			t.Logf("%6.1f/%-6.1f %.2f%% out  credit %.3f risk %.3f = %5.1f%%  cost %.3f (%.0f%% of credit)",
				short.Strike, long.Strike, out, credit, risk, credit/risk*100,
				cost, cost/credit*100)
		}
	}
}
