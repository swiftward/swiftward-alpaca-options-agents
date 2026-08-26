//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What convexity on an event costs, priced rather than assumed.
//
// A backspread sells one contract near the money and buys two further out. It is
// the mandate's structure for a day the market moves hard: the loss is capped and
// known, the gain is not. Before an earnings report the question is only what the
// market charges for it, and that is a number, not a judgement.
//
// Prints, for each expiration within a week and each pair of strikes, what one
// set costs and what it loses at worst. Places nothing.
func TestWhatConvexityOnAnEventCosts(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")
	underlying := os.Getenv("EVENT_UNDERLYING")
	if underlying == "" {
		underlying = "NVDA"
	}

	ctx := context.Background()
	broker := NewBroker(url)

	open, err := broker.MarketOpen(ctx)
	require.NoError(t, err)
	if !open {
		t.Skip("the market is shut")
	}

	prices, err := broker.LastTrades(ctx, []string{underlying})
	require.NoError(t, err)
	price := prices[underlying]
	require.Positive(t, price)
	t.Logf("%s at %.2f", underlying, price)

	contracts, err := broker.ContractsAround(ctx, underlying, price, price*0.20, time.Now(), 500)
	require.NoError(t, err)

	byDay := map[string][]Contract{}
	for _, contract := range contracts {
		byDay[contract.Expiration.Format(time.DateOnly)] = append(
			byDay[contract.Expiration.Format(time.DateOnly)], contract)
	}
	days := make([]string, 0, len(byDay))
	for day := range byDay {
		days = append(days, day)
	}
	sort.Strings(days)

	for _, day := range days[:min(len(days), 2)] {
		for _, kind := range []string{"call", "put"} {
			legs := make([]Contract, 0)
			for _, contract := range byDay[day] {
				if contract.Type == kind {
					legs = append(legs, contract)
				}
			}
			sort.Slice(legs, func(i, j int) bool { return legs[i].Strike < legs[j].Strike })
			if len(legs) < 4 {
				continue
			}

			symbols := make([]string, 0, len(legs))
			for _, leg := range legs {
				symbols = append(symbols, leg.Symbol)
			}
			quotes, err := broker.Quotes(ctx, symbols)
			if err != nil {
				continue
			}

			t.Logf("=== %s %s expiring %s", underlying, kind, day)
			for i, near := range legs {
				out := (near.Strike - price) / price * 100
				if kind == "put" {
					out = (price - near.Strike) / price * 100
				}
				if out < 1 || out > 6 {
					continue
				}
				step := 1
				if kind == "put" {
					step = -1
				}
				far := i + step*2
				if far < 0 || far >= len(legs) {
					continue
				}
				nearQuote, haveNear := quotes[near.Symbol]
				farQuote, haveFar := quotes[legs[far].Symbol]
				if !haveNear || !haveFar || nearQuote.Bid <= 0 || farQuote.Ask <= 0 {
					continue
				}

				// Sell one near, buy two far. Negative is a credit.
				cost := 2*farQuote.Ask - nearQuote.Bid
				width := legs[far].Strike - near.Strike
				if width < 0 {
					width = -width
				}
				worst := width + cost
				money := "стоит"
				if cost < 0 {
					money = "платит"
				}
				t.Logf("  sell %.0f buy 2x%.0f (%.1f%% out): %s %.2f за набор, худший случай %.2f (%s)",
					near.Strike, legs[far].Strike, out, money, cost*100, worst*100,
					fmt.Sprintf("на %.0f", legs[far].Strike))
			}
		}
	}
}
