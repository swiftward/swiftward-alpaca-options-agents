package placement

import (
	"math"
	"sort"
)

// enumerate walks every permitted pairing of a sold leg and a bought leg, prices
// it at the sides of the book a real order would cross, and replays it. It
// returns what survived and how many pairings were looked at, because a caller
// told "nothing qualifies" is entitled to know whether that was out of five or
// out of six hundred.
func (s Scorer) enumerate(ask Ask, legs []leg, spot, sigma float64, moves []float64) ([]Placement, int) {
	most := ask.ShortMostSigma
	if most <= 0 {
		most = 4
	}

	var found []Placement
	considered := 0

	for _, short := range legs {
		away := distance(ask.Kind, short.strike, spot, sigma)
		if away < ask.ShortLeastSigma || away > most {
			continue
		}
		for _, long := range legs {
			if !further(ask.Kind, long.strike, short.strike) {
				continue
			}
			valley := distance(ask.Kind, long.strike, spot, sigma)
			if valley < ask.ValleyLeastSigma {
				continue
			}
			considered++

			// The executable sides, not the midpoint: the sold leg is hit on the
			// bid, the bought legs are lifted on the ask. A credit worked out on
			// midpoints is a credit nobody pays.
			credit := short.bid - float64(ask.Bought)*long.ask
			width := math.Abs(long.strike - short.strike)
			perSet := (width - credit) * 100
			if perSet <= 0 {
				// The valley is above water: the structure cannot lose there, and
				// what it is is not a backspread. Whatever it is, it is not what
				// this ceiling was meant to size.
				continue
			}
			// A credit worth more than half the width is a broken quote, and the
			// screener already refuses verticals on the same grounds. Here it
			// matters more than there, because sets are what the ceiling divides
			// by: as the credit walks toward the width the worst case per set
			// walks toward zero, hundreds of sets fit, and expectation - which is
			// linear in sets - carries a placement built on a stale one-lot bid
			// straight to the top of the list. Found in review on 28 August 2026.
			if credit > width/2 {
				continue
			}
			sets := int(math.Floor(ask.WorstCaseMost / perSet))
			if sets < 1 {
				continue
			}

			at := s.replay(ask, moves, spot, short.strike, long.strike, credit, sets)
			at.ShortStrike, at.LongStrike = short.strike, long.strike
			at.ShortSymbol, at.LongSymbol = short.symbol, long.symbol
			at.Credit, at.Sets = credit, sets
			at.WorstCase = -perSet * float64(sets)
			at.ShortSigma, at.ValleySigma = away, valley
			found = append(found, at)
		}
	}

	return found, considered
}

// distance is how far a strike sits from the price, in sigmas, on the side the
// structure is built. A put backspread is the mirror of a call one, and its
// strikes sit BELOW: measuring them the same way would call every one of them
// negative and refuse the lot.
func distance(kind string, strike, spot, sigma float64) float64 {
	if kind == "call" {
		return (strike - spot) / sigma
	}

	return (spot - strike) / sigma
}

// further says whether the bought strike sits beyond the sold one, which for a
// put means lower.
func further(kind string, bought, sold float64) bool {
	if kind == "call" {
		return bought > sold
	}

	return bought < sold
}

// replay values the structure at the end of every window and reports what the
// spread of those values looks like. It answers in dollars for the whole
// position, not per set: the caller is deciding whether to send this, and what
// it is deciding about is the money.
func (s Scorer) replay(ask Ask, moves []float64, spot, short, long, credit float64, sets int) Placement {
	out := make([]float64, len(moves))
	for i, move := range moves {
		end := spot * move
		var value float64
		if ask.Kind == "call" {
			value = -math.Max(end-short, 0) + float64(ask.Bought)*math.Max(end-long, 0)
		} else {
			value = -math.Max(short-end, 0) + float64(ask.Bought)*math.Max(long-end, 0)
		}
		out[i] = (value + credit) * 100 * float64(sets)
	}

	total, losing, touched := 0.0, 0, 0
	worst := math.Inf(1)
	for i, x := range out {
		total += x
		if x < 0 {
			losing++
		}
		if x < worst {
			worst = x
		}
		if reached(ask.Kind, spot*moves[i], short) {
			touched++
		}
	}
	expected := total / float64(len(out))

	sorted := append([]float64(nil), out...)
	sort.Float64s(sorted)

	// What the best one percent contributes to the average. A structure whose
	// expectation lives here is a lottery ticket wearing a strategy's clothes.
	top := len(sorted) / 100
	if top < 1 {
		top = 1
	}
	tail := 0.0
	for _, x := range sorted[len(sorted)-top:] {
		tail += x
	}
	var fromTop *float64
	if expected > 0 {
		share := 100 * (tail / float64(len(sorted))) / expected
		fromTop = &share
	}

	return Placement{
		Expected:       expected,
		Median:         median(sorted),
		Worst:          worst,
		LosingShare:    100 * float64(losing) / float64(len(out)),
		TouchedShare:   100 * float64(touched) / float64(len(out)),
		FromTopPercent: fromTop,
	}
}

// reached says whether the price ended at or past the sold strike, on the side
// the structure is built. Below it the structure did nothing at all and kept its
// credit, which is worth knowing separately from what it is worth.
func reached(kind string, end, short float64) bool {
	if kind == "call" {
		return end >= short
	}

	return end <= short
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return sorted[n/2]
	}

	return (sorted[n/2-1] + sorted[n/2]) / 2
}
