package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A series left net short calls has no worst case to compute, and saying one
// anyway is how a ceiling passes something it cannot bound.
//
// AtRisk probes the payoff at the strikes, at zero and above the highest strike.
// That is exact for a vertical, a condor or a backspread, whose payoff flattens,
// and wrong for a book left short two calls against one long: the loss keeps
// growing past the last probe. The number AtRisk returns there is the loss at
// twice the highest strike, which nothing entitles anyone to treat as a limit.
func TestASeriesLeftNetShortCallsIsNamed(t *testing.T) {
	without := WithoutAFloor([]Position{
		{Symbol: "SPY260904C00650000", Quantity: -2, CostBasis: -60},
		{Symbol: "SPY260904C00640000", Quantity: 1, CostBasis: 66},
	})

	assert.Equal(t, []string{"SPY 2026-09-04"}, without)
}

// The convexity layer is the same shape the other way up - one call sold against
// two bought - and its loss is bounded by construction. Naming it would freeze
// entries on a book that is behaving exactly as designed.
func TestABackspreadHasAFloor(t *testing.T) {
	assert.Empty(t, WithoutAFloor([]Position{
		{Symbol: "SPY260904C00640000", Quantity: -1, CostBasis: -66},
		{Symbol: "SPY260904C00650000", Quantity: 2, CostBasis: 60},
	}))
}

// Puts need no such test: below a price of zero there is nothing left to lose,
// so a book short puts is bounded however many it holds.
func TestPutsAreNeverWithoutAFloor(t *testing.T) {
	assert.Empty(t, WithoutAFloor([]Position{
		{Symbol: "SPY260904P00640000", Quantity: -10, CostBasis: -600},
	}))
}

// Two expiries are two series: legs of different days do not offset at expiry,
// the near one settling while the far one is still alive.
func TestEachExpiryIsItsOwnSeries(t *testing.T) {
	without := WithoutAFloor([]Position{
		{Symbol: "SPY260904C00650000", Quantity: -1, CostBasis: -30},
		{Symbol: "SPY260911C00650000", Quantity: 1, CostBasis: 40},
	})

	assert.Equal(t, []string{"SPY 2026-09-04"}, without)
}
