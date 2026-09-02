// The book's worst case: how much a participant loses if the market goes against
// them all the way.
//
// The payoff of options at expiry is piecewise linear, and every one of its
// breakpoints sits on a strike. Between breakpoints the function is a straight
// line, so it is enough to price the book at zero and at each strike - the
// minimum cannot hide inside a segment.
//
// But exactly that is NOT ENOUGH, and an earlier version was caught by it. The
// underlying does not go below zero, but upwards it goes anywhere: past the last
// strike the line continues as a ray, and if that ray points down, there is no
// minimum at all. The earlier code substituted a point at "the last strike times
// two plus one" and got a NUMBER - finite, tidy and wrong. Because of it the
// collateral required depended on the LEVEL of the strike rather than on the
// risk: a naked short C700 cost 70,100 and a C100 cost 10,100, though the loss
// on both is unbounded - and a naked short call passed the collateral check
// without trouble.
//
// So past the last strike we look not at the value but at the SLOPE, which is
// the sum of the quantities across the calls. A negative slope means the loss is
// unbounded, and the answer here is minus infinity rather than a large number.
// An order that would create such a book is refused: the arena does not take
// structures whose maximum loss is unknown.
package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// contract is a parsed OCC symbol: SPY260828C00500000.
type contract struct {
	Root   string
	Expiry string // YYMMDD - we group by it: breakpoints of different expiries do not mix
	Call   bool
	Strike float64
	// Expires is the same date, parsed. Expiry settlement needs it, and keeping
	// it parsed here is cheaper than parsing six digits in three places.
	Expires time.Time
}

// parseOCC parses a contract symbol. The format is strict: a root, six digits of
// date, C or P, and eight digits of strike in thousandths.
//
// The strictness is not pedantry. An earlier version handed the whole tail to
// strconv.Atoi, which accepts a sign: SPY260828C-0050000 gave a strike of minus
// fifty, and such a position IMPROVED the book's worst case instead of making it
// worse. The date was not checked at all, and SPYZZZZZZ... parsed into a group
// with the expiry "ZZZZZZ", which never arrives.
func parseOCC(symbol string) (contract, error) {
	s := strings.TrimSpace(symbol)
	if len(s) < 16 {
		return contract{}, fmt.Errorf("%q is shorter than a contract symbol", symbol)
	}

	tail := s[len(s)-15:]
	root := s[:len(s)-15]
	for _, r := range root {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return contract{}, fmt.Errorf("%q: the root %q holds something that is neither a letter nor a digit", symbol, root)
		}
	}

	expiry := tail[:6]
	expires, err := time.ParseInLocation("060102", expiry, exchangeZone)
	if err != nil {
		return contract{}, fmt.Errorf("%q: the date %q cannot be read", symbol, expiry)
	}

	kind := tail[6]
	if kind != 'C' && kind != 'P' {
		return contract{}, fmt.Errorf("%q: kind %q is neither C nor P", symbol, string(kind))
	}

	strike := tail[7:]
	for _, r := range strike {
		if r < '0' || r > '9' {
			return contract{}, fmt.Errorf("%q: the strike %q holds something that is not a digit", symbol, strike)
		}
	}
	thousandths, err := strconv.Atoi(strike)
	if err != nil {
		return contract{}, fmt.Errorf("%q: the strike is not a number", symbol)
	}
	if thousandths <= 0 {
		return contract{}, fmt.Errorf("%q: the strike is not positive", symbol)
	}

	return contract{
		Root:    root,
		Expiry:  expiry,
		Call:    kind == 'C',
		Strike:  float64(thousandths) / 1000,
		Expires: expires,
	}, nil
}

// Unbounded explains WHY the book's loss has no ceiling, in words that show what
// to change. An empty string means it has one.
//
// A refusal that names only "a requirement of +Inf" makes the agent derive the
// rule from arithmetic. It does derive it - checked on 29 August, a participant
// worked it out unaided - but that is a guess rather than a disclosure: not a
// word in the answer said that what is forbidden is the uncovered short. A limit
// an agent is forced to guess is a limit it will sooner or later guess wrong.
func Unbounded(positions []Position) string {
	type side struct {
		calls, puts int
		strike      float64
	}

	groups := map[string]*side{}
	for _, p := range positions {
		c, err := parseOCC(p.Symbol)
		if err != nil {
			continue
		}
		key := c.Root + " " + c.Expiry
		g := groups[key]
		if g == nil {
			g = &side{}
			groups[key] = g
		}
		if c.Call {
			g.calls += p.Qty
			if p.Qty < 0 && c.Strike > g.strike {
				g.strike = c.Strike
			}
		} else {
			g.puts += p.Qty
		}
	}

	for key, g := range groups {
		if g.calls >= 0 {
			continue
		}

		return fmt.Sprintf(
			"the loss on %s has no ceiling: there are %d more calls sold than bought, "+
				"and above the strike %.2f there is nothing to cover them with. An infinite loss is "+
				"backed by no amount of cash, so this is not about the size of the account. To get the "+
				"order through, buy a call at a higher strike of the same expiry - then the maximum "+
				"loss becomes the difference between the strikes",
			key, -g.calls, g.strike)
	}

	// Nothing uncovered was found: the book is bounded, and there is nothing to explain.
	return ""
}

// intrinsic is what a contract is worth at expiry with the underlying at s.
func (c contract) intrinsic(s float64) float64 {
	if c.Call {
		if s > c.Strike {
			return s - c.Strike
		}

		return 0
	}
	if c.Strike > s {
		return c.Strike - s
	}

	return 0
}

// Worst returns the value of the book at expiry at its worst point. A negative
// value is a loss. Minus infinity means the loss has no ceiling at all, and that
// is not a failure of the arithmetic but the answer.
//
// Groups of (root, expiry) are counted apart and added up. That is deliberately
// stricter than the truth: the market is under no obligation to move against us
// in every underlying at once. But an instrument that errs towards caution does
// not let a participant draw profit, and one that errs the other way does.
func Worst(positions []Position) (float64, error) {
	type group struct {
		legs   []contract
		qty    []int
		points []float64
	}

	groups := map[string]*group{}
	for _, p := range positions {
		// Shares are not part of this line: they have no expiry, and their
		// contribution is linear and unbounded above. Their collateral is counted
		// apart, in Requirement, under Reg T - the way a real broker counts it.
		if p.Class == classEquity {
			continue
		}
		c, err := parseOCC(p.Symbol)
		if err != nil {
			return 0, err
		}
		key := c.Root + "/" + c.Expiry
		g := groups[key]
		if g == nil {
			// Zero is the left end: the underlying does not go below it.
			g = &group{points: []float64{0}}
			groups[key] = g
		}
		g.legs = append(g.legs, c)
		g.qty = append(g.qty, p.Qty)
		g.points = append(g.points, c.Strike)
	}

	total := 0.0
	for _, g := range groups {
		// The slope of the ray past the last strike: out there every call is in
		// the money and rises with the underlying, and every put is worth zero.
		slope := 0
		far := 0.0
		for i, c := range g.legs {
			if c.Call {
				slope += g.qty[i]
			}
			if c.Strike > far {
				far = c.Strike
			}
		}
		if slope < 0 {
			return math.Inf(-1), nil
		}
		// The slope is not negative, so nothing gets worse along the ray; the last
		// strike itself is enough as the far point.
		g.points = append(g.points, far)

		worst := 0.0
		first := true
		for _, s := range g.points {
			value := 0.0
			for i, c := range g.legs {
				value += c.intrinsic(s) * float64(g.qty[i]) * multiplier
			}
			if first || value < worst {
				worst, first = value, false
			}
		}
		total += worst
	}

	return total, nil
}

// Requirement is how much money the book must hold against its obligations.
// Buying power follows from it: options_buying_power = cash - requirement.
//
// Options produce a requirement through the line above: what the book loses at
// its worst point. Shares are counted apart and under Reg T, because their loss
// has no ceiling at all and the "worst case" of a short share position is
// infinity. A real broker does not count a worst case here either - it takes a
// share: a hundred percent of the proceeds of the sale, which are already in the
// cash, plus fifty on top. Long shares are paid for in full and create no
// requirement.
//
// The price of a short share is the last one known rather than a live one: the
// requirement is computed inside the filling of an order among other places, and
// going to the network from there would mean holding the book's lock for the
// length of a network call. The price is refreshed by every valuation of the
// account, so the lag is one tick of the matcher.
func Requirement(positions []Position) (float64, error) {
	worst, err := Worst(positions)
	if err != nil {
		return 0, err
	}

	need := 0.0
	if math.IsInf(worst, -1) {
		// An unbounded loss cannot be backed by any amount of money. The answer
		// "infinity" carries itself onward: any cash is less than it, and an order
		// that would create such a book is refused with a reason in plain words.
		return math.Inf(1), nil
	}
	if worst < 0 {
		need = -worst
	}

	for _, p := range positions {
		if p.Class != classEquity || p.Qty >= 0 {
			continue
		}
		price := p.Mark
		if price <= 0 {
			price = p.AvgPrice
		}
		need += 1.5 * absf(p.Qty) * price
	}

	return need, nil
}
