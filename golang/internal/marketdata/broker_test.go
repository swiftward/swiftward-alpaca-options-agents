//go:build broker

package marketdata

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The broker tier. It calls the real server on the development account and
// proves the fields this package reads are still there - the shapes above are
// captured answers, and a captured answer cannot notice the day the broker
// renames a field.
func TestTheBrokerStillAnswersInTheShapesWeRead(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL: this tier has nothing to say without a broker")

	ctx := context.Background()
	broker := NewBroker(url)

	prices, err := broker.LastTrades(ctx, []string{"SPY"})
	require.NoError(t, err)
	require.Contains(t, prices, "SPY")
	price := prices["SPY"]
	assert.Positive(t, price)

	_, err = broker.MarketOpen(ctx)
	require.NoError(t, err)

	contracts, err := broker.ContractsAround(ctx, "SPY", price, 3, time.Now(), 40)
	require.NoError(t, err)
	require.NotEmpty(t, contracts, "the broker lists no SPY option within $3 of the money")
	for _, contract := range contracts {
		assert.NotEmpty(t, contract.Symbol)
		assert.Positive(t, contract.Strike)
		assert.Contains(t, []string{"call", "put"}, contract.Type)
		assert.False(t, contract.Expiration.IsZero())
	}

	quotes, err := broker.Quotes(ctx, []string{contracts[0].Symbol})
	require.NoError(t, err)
	quote, answered := quotes[contracts[0].Symbol]
	require.True(t, answered, "the broker returned no snapshot for a contract it just listed")
	assert.Positive(t, quote.Ask)
	if quote.ImpliedVolatility != nil {
		// Present whenever the quote is two-sided. Outside market hours it can be
		// absent, and that is the clock, not a failure.
		assert.Positive(t, *quote.ImpliedVolatility)
		require.NotNil(t, quote.Delta, "a contract with implied volatility carries greeks too")
	}
}
