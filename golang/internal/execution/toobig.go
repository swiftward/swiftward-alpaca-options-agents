package execution

import (
	"math"
	"strings"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// OnlyCloses reports whether every leg of an order gives back a position the
// account already holds.
//
// It matters because the size checks price an order as if it were new exposure,
// and a closing order read that way is judged by the structure it LOOKS like
// rather than by what it does. Closing a backspread - buying back one sold leg
// and selling two bought ones - prices out as a ratio spread short two calls:
// measured at 640,200 dollars against a ceiling of 8,000, so the guard cancels
// the one order that was reducing the account's risk, tells the session to take
// less, and does it again on the retry.
//
// An order whose legs do not say what they do is NOT treated as closing: absent
// is not the same as closing, and the fail-safe direction here is to judge it.
func OnlyCloses(order marketdata.Order) bool {
	if len(order.Legs) == 0 {
		return false
	}
	for _, leg := range order.Legs {
		if !strings.HasSuffix(leg.PositionIntent, "_to_close") {
			return false
		}
	}

	return true
}

// Unbounded reports whether an order's loss has no floor at all: a structure
// left net SHORT calls loses more the higher the underlying goes, and there is no
// price at which that stops.
//
// It is separate from WorstCase because WorstCase samples the payoff at the
// strikes, at zero and above the highest strike, and calls the smallest of those
// the worst. That is exact for every structure whose payoff flattens - a
// vertical, a condor, a backspread - and wrong for one that does not: a ratio
// short two calls against one long prices out as a large finite number, which
// then passes or fails a ceiling as if it meant something.
//
// Puts need no such test: below a price of zero there is nothing left to lose.
func Unbounded(order marketdata.Order) bool {
	net := 0.0
	for _, one := range order.Legs {
		contract, parsed := marketdata.ContractFrom(one.Symbol)
		if !parsed || !strings.EqualFold(contract.Type, "call") {
			continue
		}
		held := one.Quantity
		if held <= 0 {
			held = order.Quantity
		}
		if one.Side == "sell" {
			held = -held
		}
		net += held
	}

	return net < 0
}

// WorstCase is the most a resting multi-leg order can lose if it fills, in
// dollars, at the worst price it is allowed to fill at - and whether it could be
// worked out at all.
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

	// What is still RESTING, not what was ordered. Part of an order can fill
	// before the rest, and cancelling gives back only the unfilled part - so
	// judging the whole order would take a sound remainder away for the size of
	// a position the account already holds and this cancel cannot undo.
	resting := order.Quantity - order.FilledQuantity
	if resting <= 0 {
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
		// A leg's own quantity carries the RATIO - one sold against two bought in
		// a backspread - so what matters is its share of the order, applied to
		// what is still resting. Reading the leg's quantity directly would price
		// the whole order again on an order half filled.
		held := resting
		if one.Quantity > 0 {
			held = one.Quantity / order.Quantity * resting
		}
		if one.Side == "sell" {
			held = -held
		}
		legs = append(legs, leg{contract: contract, held: held})
		probes = append(probes, contract.Strike)
		highest = math.Max(highest, contract.Strike)
	}
	probes = append(probes, highest*2)

	// Priced at the WORST the order may reach, not at where it rests.
	//
	// The ladder concedes as it walks, and every cent it gives up makes the worst
	// case bigger. Judging the order by its current price would pass it when it
	// is placed and refuse it three steps later - the same order, cancelled for
	// moving to a price its own session had already declared acceptable. Judging
	// it by the floor it can never pass answers the only question worth asking:
	// can this order lose more than one position may, at any price it can fill at?
	//
	// An order that names no floor is judged where it stands. There is no honest
	// way to invent the number here.
	worstPrice := order.LimitPrice
	if floor, named := Reservation(order); named {
		worstPrice = floor
	}
	settled := -worstPrice * 100 * resting

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
