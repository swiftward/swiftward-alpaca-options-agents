//go:build db

package volatility

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db/dbtest"
)

func TestPostgresKeepsTheSeries(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	series := NewPostgres(pool)
	base := time.Date(2026, 8, 28, 14, 0, 0, 0, time.UTC)
	delta := -0.5012

	for i, value := range []float64{0.10, 0.20, 0.14, 0.15} {
		sample := Sample{
			Underlying: "SPY", Contract: fmt.Sprintf("SPY26082%dC00764000", i),
			RecordedAt: base.Add(time.Duration(i) * time.Hour),
			Expiration: time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
			Strike:     764, OptionType: "call", ImpliedVolatility: value,
			Delta: &delta, Bid: 3.3, Ask: 3.4, UnderlyingPrice: 763.65,
		}
		require.NoError(t, series.Append(ctx, sample))
		require.NoError(t, series.Append(ctx, sample), "the same reading twice is one row")
	}

	summary, err := series.Summarise(ctx, "SPY", base.Add(-time.Hour))
	require.NoError(t, err)

	assert.Equal(t, 4, summary.Samples)
	assert.InDelta(t, 0.15, summary.Latest, 1e-6)
	assert.InDelta(t, 0.10, summary.Lowest, 1e-6)
	assert.InDelta(t, 0.20, summary.Highest, 1e-6)
	assert.InDelta(t, 62.5, summary.Rank, 1e-6, "0.15 stands above two of the four readings and is one of them")

	// A window that starts after the readings answers with nothing rather than
	// with the whole series: the question was about the window.
	later, err := series.Summarise(ctx, "SPY", base.Add(24*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, later.Samples)

	other, err := series.Summarise(ctx, "AVGO", base.Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, other.Samples, "one underlying's history is not another's")
}
