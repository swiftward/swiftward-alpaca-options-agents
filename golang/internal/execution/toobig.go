package execution

import (
	"math"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// WorstCase is the most a resting multi-leg order can lose if it fills, in
// dollars, and whether it could be worked out at all.
//
// The payoff of a spread is piecewise linear, so its worst point is at one of
// the strikes, at zero, or far above the highest strike. Checking those points
// is exact rather than a sample.
//
// It reports false where the order cannot be read: a leg whose symbol does not
// parse, a quantity of nothing, an order with no legs. Unknown is not the same
// as small, and the caller must not treat it as such.
func WorstCase(order marketdata.Order) (float64, bool) {
	if len(order.Legs) == 0 || order.Quantity <= 0 {
		return 0, false
	}

	type leg struct {
		contract marketdata.Contract
		held     float64
	}
	legs := make([]leg, 0, len(order.Legs))
	probes := []float64{0}
	highest := 0.0
	for _, one := range order.Legs {
		contract, parsed := marketdata.ContractFrom(one.Symbol)
		if !parsed {
			return 0, false
		}
		// A leg's own quantity where the broker gives one - the ratio of a
		// backspread lives there - and the order's otherwise.
		held := one.Quantity
		if held <= 0 {
			held = order.Quantity
		}
		if one.Side == "sell" {
			held = -held
		}
		legs = append(legs, leg{contract: contract, held: held})
		probes = append(probes, contract.Strike)
		highest = math.Max(highest, contract.Strike)
	}
	probes = append(probes, highest*2)

	// The credit is already in hand and the debit already paid, so the price the
	// order rests at moves the worst case by exactly that much. A credit is
	// quoted negative, which is why it is added rather than subtracted.
	settled := -order.LimitPrice * 100 * order.Quantity

	worst := math.MaxFloat64
	for _, price := range probes {
		payoff := 0.0
		for _, one := range legs {
			intrinsic := math.Max(0, one.contract.Strike-price)
			if one.contract.Type == "call" {
				intrinsic = math.Max(0, price-one.contract.Strike)
			}
			payoff += one.held * 100 * intrinsic
		}
		worst = math.Min(worst, payoff+settled)
	}

	return math.Min(0, worst), true
}
