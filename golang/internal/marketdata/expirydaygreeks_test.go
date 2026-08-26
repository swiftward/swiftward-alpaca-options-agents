//go:build broker

package marketdata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What the broker prices on the day a contract expires, and what it does not.
//
// Answered on 26 August, and the answer decided the design: of 28 QQQ contracts
// expiring that day, NONE carried implied volatility and none carried delta. The
// expiry-day book arrives as two prices and nothing else.
//
// So a measure for that book cannot read its risk off its own quote. It has to
// borrow the number from a expiration that does carry one, and say that it did.
// Run this again before trusting any measure that assumes otherwise.
func TestWhetherVolatilityIsGivenOnExpiryDay(t *testing.T) {
	broker := NewBroker(os.Getenv("BROKER_MCP_URL"))
	ctx := context.Background()

	prices, err := broker.LastTrades(ctx, []string{"QQQ"})
	require.NoError(t, err)
	price := prices["QQQ"]

	contracts, err := broker.ContractsAround(ctx, "QQQ", price, price*0.01, time.Now(), 60)
	require.NoError(t, err)

	today := time.Now().Format(time.DateOnly)
	var symbols []string
	for _, c := range contracts {
		if c.Expiration.Format(time.DateOnly) == today {
			symbols = append(symbols, c.Symbol)
		}
	}
	require.NotEmpty(t, symbols, "nothing expires today")

	quotes, err := broker.Quotes(ctx, symbols)
	require.NoError(t, err)

	withIV, withDelta, quoted := 0, 0, 0
	for _, q := range quotes {
		quoted++
		if q.ImpliedVolatility != nil {
			withIV++
		}
		if q.Delta != nil {
			withDelta++
		}
	}
	t.Logf("expiry day: %d quoted, %d carry implied volatility, %d carry delta", quoted, withIV, withDelta)

	for _, q := range quotes {
		if q.ImpliedVolatility != nil {
			t.Logf("example: %s iv=%.4f", q.Symbol, *q.ImpliedVolatility)
			break
		}
	}
}
