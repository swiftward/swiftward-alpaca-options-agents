package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A sold put spread: one sold near the money, one bought a dollar below, opened
// for a credit. The most it can lose is the width less what it took.
func TestASoldSpreadRisksItsWidthLessTheCredit(t *testing.T) {
	held := []Position{
		{Symbol: "QQQ260828P00706000", Quantity: -1, CostBasis: -120},
		{Symbol: "QQQ260828P00705000", Quantity: 1, CostBasis: 92},
	}

	// Width 1 dollar on 100 shares is 100; the credit taken was 28.
	assert.InDelta(t, 72, AtRisk(held), 0.01)
}

// Two spreads on the same underlying expiring on DIFFERENT days do not offset:
// the near one settles while the far one is still alive, and a book that netted
// them would read as covered when it is not.
func TestDifferentExpiriesDoNotOffsetEachOther(t *testing.T) {
	near := []Position{
		{Symbol: "QQQ260828P00706000", Quantity: -1, CostBasis: -120},
		{Symbol: "QQQ260828P00705000", Quantity: 1, CostBasis: 92},
	}
	far := []Position{
		{Symbol: "QQQ260904C00720000", Quantity: -1, CostBasis: -150},
		{Symbol: "QQQ260904C00721000", Quantity: 1, CostBasis: 110},
	}

	assert.InDelta(t, AtRisk(near)+AtRisk(far), AtRisk(append(near, far...)), 0.01,
		"two series are two risks, not one netted one")
}

// A bought option risks the premium paid for it - that is what it loses when it
// expires worthless, and it is the whole risk of the convexity layer, which is
// bought rather than sold. Writing this test the other way round was wrong: a
// long option is not a position that cannot lose.
func TestABoughtOptionRisksWhatItCost(t *testing.T) {
	bought := []Position{{Symbol: "SPY260828P00700000", Quantity: 1, CostBasis: 300}}
	assert.InDelta(t, 300, AtRisk(bought), 0.01)

	sold := []Position{
		{Symbol: "QQQ260828P00706000", Quantity: -1, CostBasis: -120},
		{Symbol: "QQQ260828P00705000", Quantity: 1, CostBasis: 92},
	}
	assert.InDelta(t, AtRisk(sold)+300, AtRisk(append(bought, sold...)), 0.01,
		"different underlyings are separate risks and add up")
}

// Shares and anything else the account happens to hold are skipped rather than
// guessed at: this answers about option structures, and a wrong guess here would
// be spent as if it were a measurement.
func TestWhatIsNotAnOptionIsSkipped(t *testing.T) {
	assert.Zero(t, AtRisk([]Position{{Symbol: "AAPL", Quantity: 10, CostBasis: 2000}}))
	assert.Zero(t, AtRisk(nil))
}
