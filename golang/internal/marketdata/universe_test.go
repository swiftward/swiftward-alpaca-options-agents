//go:build broker

package marketdata

import (
	"context"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What each candidate underlying actually pays for the structure this project
// sells: the nearest expiration that still carries greeks, the 1-wide spread at
// the short leg nearest delta -0.15, and what the book charges to get in and out.
// Places nothing; needs the market open.
//
// The engine trades ETFs because they expire daily. This asks whether that is
// still the right choice once the price of the structure is measured rather than
// assumed.
func TestWhatEachUnderlyingPays(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	open, err := broker.MarketOpen(ctx)
	require.NoError(t, err)
	if !open {
		t.Skip("the market is shut: quotes are yesterday's and greeks are absent")
	}

	candidates := []string{
		"SPY", "QQQ", "IWM", "DIA",
		"NVDA", "TSLA", "AAPL", "AMZN", "META", "MSFT", "GOOGL", "AMD", "NFLX", "AVGO",
	}

	prices, err := broker.LastTrades(ctx, candidates)
	require.NoError(t, err)

	type finding struct {
		underlying string
		expiration string
		days       int
		strike     float64
		delta      float64
		credit     float64
		risk       float64
		spread     float64
	}
	var found []finding

	today := time.Now().Truncate(24 * time.Hour)

	for _, underlying := range candidates {
		price, known := prices[underlying]
		if !known || price <= 0 {
			t.Logf("%-6s no price", underlying)
			continue
		}

		contracts, err := broker.ContractsAround(ctx, underlying, price*0.985, price*0.02, time.Now(), 300)
		if err != nil {
			t.Logf("%-6s could not list contracts: %v", underlying, err)
			continue
		}

		byDay := map[string][]Contract{}
		for _, contract := range contracts {
			if contract.Type != "put" {
				continue
			}
			day := contract.Expiration.Format(time.DateOnly)
			if day <= today.Format(time.DateOnly) {
				continue
			}
			byDay[day] = append(byDay[day], contract)
		}
		if len(byDay) == 0 {
			t.Logf("%-6s %.2f: no put expiring after today within reach", underlying, price)
			continue
		}

		days := make([]string, 0, len(byDay))
		for day := range byDay {
			days = append(days, day)
		}
		sort.Strings(days)

		// The nearest expiration whose contracts the broker actually prices with
		// greeks - that is what the entry rule needs to choose a strike.
		for _, day := range days[:min(len(days), 3)] {
			puts := byDay[day]
			sort.Slice(puts, func(i, j int) bool { return puts[i].Strike < puts[j].Strike })

			symbols := make([]string, 0, len(puts))
			for _, put := range puts {
				symbols = append(symbols, put.Symbol)
			}
			quotes, err := broker.Quotes(ctx, symbols)
			if err != nil {
				continue
			}

			best, index := math.MaxFloat64, -1
			for i, put := range puts {
				quote, answered := quotes[put.Symbol]
				if !answered || quote.Delta == nil || quote.Bid <= 0 || quote.Ask <= 0 {
					continue
				}
				if distance := math.Abs(*quote.Delta + 0.15); distance < best {
					best, index = distance, i
				}
			}
			if index <= 0 {
				continue
			}

			short, long := puts[index], puts[index-1]
			shortQuote, longQuote := quotes[short.Symbol], quotes[long.Symbol]
			if longQuote.Bid <= 0 || longQuote.Ask <= 0 {
				continue
			}

			width := short.Strike - long.Strike
			credit := (shortQuote.Bid+shortQuote.Ask)/2 - (longQuote.Bid+longQuote.Ask)/2
			risk := width - credit
			if risk <= 0 || credit <= 0 {
				continue
			}
			expires, _ := time.Parse(time.DateOnly, day)

			found = append(found, finding{
				underlying: underlying, expiration: day,
				days:   int(expires.Sub(today).Hours() / 24),
				strike: short.Strike, delta: *shortQuote.Delta,
				credit: credit, risk: risk,
				spread: (shortQuote.Ask - shortQuote.Bid) + (longQuote.Ask - longQuote.Bid),
			})
			break
		}
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].credit/found[i].risk > found[j].credit/found[j].risk
	})

	t.Log("underlying | expires (days out) | short strike (delta) | credit/risk | bid-ask cost")
	for _, f := range found {
		t.Logf("%-6s | %s (+%d) | %.1f (%.3f) | %.3f/%.3f = %5.1f%% | %.3f",
			f.underlying, f.expiration, f.days, f.strike, f.delta,
			f.credit, f.risk, f.credit/f.risk*100, f.spread)
	}
	require.NotEmpty(t, found, "no underlying priced a structure this project could sell")
}
