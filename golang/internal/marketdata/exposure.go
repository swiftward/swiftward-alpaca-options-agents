package marketdata

import "math"

// AtRisk is the most everything open can lose at expiry, in dollars, as a
// positive number.
//
// Positions are grouped by what they expire against - one underlying, one day -
// because legs of different days never offset each other at expiry: the near one
// settles while the far one is still alive, and a book that netted them would
// read as covered when it is not.
//
// Inside a group the payoff is piecewise linear, so the worst point is at a
// strike, at zero, or far above the highest strike. Checking those points is
// exact rather than a sample. Money already taken counts: a credit spread was
// paid for on the way in, and that money is in the account.
//
// Anything that is not an option is skipped rather than guessed at. A group that
// can only make money contributes nothing - not a negative that would offset a
// group that can lose.
func AtRisk(positions []Position) float64 {
	type series struct{ underlying, expiration string }

	groups := map[series][]Position{}
	for _, position := range positions {
		contract, parsed := ContractFrom(position.Symbol)
		if !parsed {
			continue
		}
		key := series{
			underlying: contract.Symbol[:len(contract.Symbol)-15],
			expiration: contract.Expiration.Format("2006-01-02"),
		}
		groups[key] = append(groups[key], position)
	}

	total := 0.0
	for _, held := range groups {
		total += math.Min(0, worstAtExpiry(held))
	}

	return -total
}

// WithoutAFloor names the groups whose loss has no floor at all: a series left
// net SHORT calls loses more the higher the underlying goes, and no price stops
// it. Each name is one underlying and one expiry, in the form "SPY 2026-09-04".
//
// It is separate from AtRisk because AtRisk answers with a NUMBER, and there is
// no number for this: it probes the payoff at the strikes, at zero and above the
// highest strike, which is exact for every structure that flattens and wrong for
// one that does not. A caller that only asked AtRisk would be handed a large
// finite figure and would compare it with a ceiling as if it meant something.
//
// The cage refuses to OPEN such a position, so this is what is left when the
// market makes one: a long leg assigned early, or expired while the short one
// lived on.
func WithoutAFloor(positions []Position) []string {
	net := map[string]float64{}
	order := []string{}
	for _, position := range positions {
		contract, parsed := ContractFrom(position.Symbol)
		if !parsed || contract.Type != "call" {
			continue
		}
		key := contract.Symbol[:len(contract.Symbol)-15] + " " + contract.Expiration.Format("2006-01-02")
		if _, seen := net[key]; !seen {
			order = append(order, key)
		}
		net[key] += position.Quantity
	}

	var without []string
	for _, key := range order {
		if net[key] < 0 {
			without = append(without, key)
		}
	}

	return without
}

// worstAtExpiry is what one group is worth at its worst point, money already
// settled included. Negative means a loss.
func worstAtExpiry(held []Position) float64 {
	probes := []float64{0}
	highest := 0.0
	settled := 0.0
	for _, position := range held {
		contract, parsed := ContractFrom(position.Symbol)
		if !parsed {
			continue
		}
		probes = append(probes, contract.Strike)
		highest = math.Max(highest, contract.Strike)
		// A credit spread has a negative cost basis: that money arrived when it
		// was opened.
		settled -= position.CostBasis
	}
	probes = append(probes, highest*2)

	worst := math.MaxFloat64
	for _, price := range probes {
		payoff := 0.0
		for _, position := range held {
			contract, parsed := ContractFrom(position.Symbol)
			if !parsed {
				continue
			}
			intrinsic := math.Max(0, contract.Strike-price)
			if contract.Type != "put" {
				intrinsic = math.Max(0, price-contract.Strike)
			}
			payoff += position.Quantity * 100 * intrinsic
		}
		worst = math.Min(worst, payoff+settled)
	}

	return worst
}

// RestingUnderlyings names the underlyings this account has an order working on
// right now - one entry each, however many orders or legs stand on it.
//
// It reads the orders the broker still has in play (Order.Active) and takes the
// underlying out of each leg's contract symbol. An order the ladder is walking
// counts: it can fill at any moment, and a second structure sized as though the
// first were not there is one bet taken twice.
func RestingUnderlyings(orders []Order) []string {
	seen := map[string]bool{}
	var names []string
	for _, order := range orders {
		if !order.Active() {
			continue
		}
		legs := order.Legs
		if len(legs) == 0 {
			legs = []Order{order}
		}
		for _, leg := range legs {
			contract, parsed := ContractFrom(leg.Symbol)
			if !parsed {
				continue
			}
			name := contract.Symbol[:len(contract.Symbol)-15]
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}

	return names
}
