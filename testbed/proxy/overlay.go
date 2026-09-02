// The alternative reality: the real market with ONE number moved.
//
// A staged scenario answers questions the market will not stage on request, and
// it pays for that by inventing the whole world - only the contracts it names
// have a price at all, and the book it shows is as thin or as generous as the
// author happened to type. That is right for "the price reached the strike, does
// the defence fire", and wrong for "what would this position be worth if SPY
// walked four dollars by Thursday", because the answer to the second depends on
// every number the author did not think to write down.
//
// So: overlay. Every read still goes to the real broker. The real answer comes
// back, and exactly one number in it is moved - the underlying - along a curve
// the scenario gives. Everything else is DERIVED from that move rather than
// invented: each contract is repriced by what the shift is worth to IT, using
// its own live implied volatility and its own strike and expiry.
//
// The one modelling decision worth naming. The theoretical price of an option is
// not the market's price - a model is wrong about the level by a few cents on
// every contract. So the model is used for the DIFFERENCE only:
//
//	overlaid = real + (theoretical(spot+shift) - theoretical(spot))
//
// At shift zero the difference is zero and the overlay is the real market, to
// the cent. Away from zero, the part of the model error that does not depend on
// the spot cancels. The spread, the sizes and the timestamps are the real ones
// throughout: a widening spread is a thing a scenario should have to ASK for,
// not something an overlay does behind the author's back.
//
// What is NOT moved: the clock, the chain, which contracts exist, the news, the
// account. An overlay is a different price, not a different day.
package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// overlayRate is the interest rate the pricing model uses. Zero, deliberately.
// It enters the answer only through the DIFFERENCE of two prices at the same
// moment, where the carry very nearly cancels; a wrong rate would buy us a
// second decimal of realism and a number nobody can check.
const overlayRate = 0.0

// normCDF is the standard normal distribution function.
func normCDF(x float64) float64 {
	return 0.5 * math.Erfc(-x/math.Sqrt2)
}

// bsPrice is Black-Scholes: what an option is worth at a spot, a volatility and
// a time left. Past expiry, or with no volatility, it is the intrinsic value -
// the same formula's own limit, and the case a walk into the last hour hits.
func bsPrice(call bool, spot, strike, years, sigma float64) float64 {
	intrinsic := func() float64 {
		if call {
			return math.Max(0, spot-strike)
		}

		return math.Max(0, strike-spot)
	}
	if years <= 0 || sigma <= 0 || spot <= 0 || strike <= 0 {
		return intrinsic()
	}

	d1 := (math.Log(spot/strike) + (overlayRate+sigma*sigma/2)*years) / (sigma * math.Sqrt(years))
	d2 := d1 - sigma*math.Sqrt(years)
	discount := math.Exp(-overlayRate * years)
	if call {
		return spot*normCDF(d1) - strike*discount*normCDF(d2)
	}

	return strike*discount*normCDF(-d2) - spot*normCDF(-d1)
}

// bsDelta is how much the option's price moves per dollar of the underlying.
// It is repriced along with the price for one reason: an agent reads the two
// together. A delta that stayed at 0.30 while the spot walked through the strike
// is a pair of numbers that cannot both be true, and a rule that picks its
// strikes by delta would be answering a question about our arithmetic.
func bsDelta(call bool, spot, strike, years, sigma float64) float64 {
	if years <= 0 || sigma <= 0 || spot <= 0 || strike <= 0 {
		switch {
		case call && spot > strike:
			return 1
		case !call && spot < strike:
			return -1
		}

		return 0
	}

	d1 := (math.Log(spot/strike) + (overlayRate+sigma*sigma/2)*years) / (sigma * math.Sqrt(years))
	if call {
		return normCDF(d1)
	}

	return normCDF(d1) - 1
}

// yearsLeft is the time to expiry the model uses. Contracts expire at the close
// of the exchange, not at midnight of the day their symbol names: on the last
// day the difference is the whole of the remaining time value.
func yearsLeft(c contract, now time.Time) float64 {
	expires := c.Expires.Add(16 * time.Hour)
	left := expires.Sub(now).Hours() / 24 / 365

	return math.Max(0, left)
}

// shiftQuote moves one contract's quote by what the underlying's shift is worth
// to it. The spread is carried across unchanged, and a side that was quoted
// stays quoted: a bid that the shift would drive below zero becomes zero, and an
// ask is held at a cent, because a contract that stops being quoted at all is a
// different event from a contract that got cheap, and an overlay must not
// manufacture the first while imitating the second.
func shiftQuote(q Quote, c contract, spot, shift, sigma float64, now time.Time) Quote {
	if shift == 0 || spot <= 0 {
		return q
	}
	years := yearsLeft(c, now)
	move := bsPrice(c.Call, spot+shift, c.Strike, years, sigma) -
		bsPrice(c.Call, spot, c.Strike, years, sigma)

	out := q
	if q.Bid > 0 {
		out.Bid = math.Max(0, round2(q.Bid+move))
	}
	if q.Ask > 0 {
		out.Ask = math.Max(0.01, round2(q.Ask+move))
	}
	if out.Ask > 0 && out.Bid > out.Ask {
		out.Bid = out.Ask
	}

	return out
}

// shiftDelta moves the reported delta the same way the price is moved: by the
// model's difference, laid on top of the real number.
func shiftDelta(real float64, c contract, spot, shift, sigma float64, now time.Time) float64 {
	if shift == 0 || spot <= 0 {
		return real
	}
	years := yearsLeft(c, now)
	move := bsDelta(c.Call, spot+shift, c.Strike, years, sigma) -
		bsDelta(c.Call, spot, c.Strike, years, sigma)
	out := real + move
	if c.Call {
		return math.Min(1, math.Max(0, out))
	}

	return math.Min(0, math.Max(-1, out))
}

// round2 keeps prices at the cent the exchange quotes them in. Without it the
// overlaid book carries eleven decimals and every log of it reads as fabricated.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// overlayShift is the curve: how far the underlying is from the real market at
// this moment of the run.
//
// Between two points the shift moves LINEARLY, and that is not a detail. A
// staircase teleports the price: a position that never traded through the strike
// is suddenly beyond it, no ladder ever saw a step it could have worked, and
// every measurement of "what did the defence do on the way" has nothing on the
// way to measure. A jump is still expressible - two points a second apart - and
// then it is the author who asked for it.
func (s *Scenario) shiftAt(elapsed time.Duration) float64 {
	if len(s.Steps) == 0 {
		return 0
	}
	at := time.Duration(float64(elapsed) * s.Speed)

	prev := s.Steps[0]
	if at <= prev.offset {
		return prev.shift()
	}
	for i := 1; i < len(s.Steps); i++ {
		step := s.Steps[i]
		if at >= step.offset {
			prev = step

			continue
		}
		span := (step.offset - prev.offset).Seconds()
		if span <= 0 {
			return step.shift()
		}
		part := (at - prev.offset).Seconds() / span

		return prev.shift() + part*(step.shift()-prev.shift())
	}

	return prev.shift()
}

// shift is the step's own displacement, zero if it names none.
func (s Step) shift() float64 {
	if s.Delta == nil {
		return 0
	}

	return *s.Delta
}

// describe is what the proxy prints when an overlay comes up, so a reader of the
// log can see which reality a run happened in without opening the file.
func (s *Scenario) describe() string {
	if len(s.Steps) == 0 {
		return s.Name
	}
	last := s.Steps[len(s.Steps)-1]

	return fmt.Sprintf("%s: %s moves %+.2f over %s, in %d points",
		s.Name, s.Underlying, last.shift(), last.After, len(s.Steps))
}

// realSpot is the true price of the overlay's underlying, held for a moment.
//
// It goes upstream directly and not through lastTrades, which the overlay has
// already taught to lie: asking the displaced reader for the undisplaced number
// would apply the shift twice, and the second application is invisible - the
// prices stay plausible and every contract is repriced against a market that
// never existed.
func (a *arena) realSpot(ctx context.Context, prio int) (float64, error) {
	symbol := strings.ToUpper(a.staged.underlying())
	if symbol == "" {
		return 0, fmt.Errorf("the overlay names no underlying")
	}

	a.spotMu.Lock()
	if a.spot > 0 && a.spotFor > 0 && time.Since(a.spotAt) < a.spotFor {
		held := a.spot
		a.spotMu.Unlock()

		return held, nil
	}
	a.spotMu.Unlock()

	var answer tradeAnswer
	if err := a.up.CallJSON(ctx, prio, "get_stock_latest_trade",
		map[string]any{"symbols": symbol}, &answer); err != nil {
		return 0, err
	}
	if answer.Data.Trades == nil {
		return 0, fmt.Errorf("the get_stock_latest_trade answer has no trades field: the shape upstream has changed")
	}
	price := 0.0
	for name, row := range *answer.Data.Trades {
		if strings.EqualFold(name, symbol) {
			price = row.Price
		}
	}
	if price <= 0 {
		return 0, fmt.Errorf("the broker quotes no last trade for %s", symbol)
	}

	a.spotMu.Lock()
	a.spot, a.spotAt = price, time.Now()
	a.spotMu.Unlock()

	return price, nil
}

// overlaidAnswer serves a read that has been through the real broker and had the
// underlying moved in it. The second value says whether this tool is overlaid at
// all; the clock and the chain are not, and go on untouched.
//
// The shape returned is the broker's own, because it IS the broker's own: the
// answer that came back is edited in place rather than rebuilt, so a field we
// never thought about survives the trip instead of quietly disappearing.
func (a *arena) overlaidAnswer(ctx context.Context, name string, args any) (*mcp.CallToolResult, bool) {
	switch name {
	case "get_stock_latest_trade", "get_option_snapshot":
	default:
		return nil, false
	}

	var in struct {
		Symbols string `json:"symbols"`
	}
	_ = remarshal(args, &in)
	syms := symbolsOf(in.Symbols)
	if len(syms) == 0 {
		// The same refusal a staged market gives, and for the same reason: an
		// empty success turns the agent's own mistake into news about the world.
		res, _ := refuse("the market reads take their symbols in `symbols`, comma separated, "+
			"and this call to %s named none%s. This is a refusal and not an empty market: "+
			"the price is there, the call did not ask for it.", name, argNames(args))

		return res, true
	}

	shift := a.staged.shiftNow()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := a.up.CallJSON(ctx, prioBrowse, name, args, &body); err != nil {
		res, _ := refuse("the broker did not answer %s: %v", name, err)

		return res, true
	}
	if shift == 0 {
		res, err := answer(name, body.Data)
		if err != nil {
			return nil, false
		}

		return res, true
	}

	spot, err := a.realSpot(ctx, prioBrowse)
	if err != nil {
		res, _ := refuse("the overlay could not read the real price of %s, and without it nothing can be repriced: %v",
			a.staged.underlying(), err)

		return res, true
	}

	if name == "get_stock_latest_trade" {
		if err := overlayTrades(body.Data, strings.ToUpper(a.staged.underlying()), shift); err != nil {
			res, _ := refuse("%v", err)

			return res, true
		}
	} else if err := overlaySnapshots(body.Data, spot, shift); err != nil {
		res, _ := refuse("%v", err)

		return res, true
	}

	res, err2 := answer(name, body.Data)
	if err2 != nil {
		return nil, false
	}

	return res, true
}

// overlayTrades moves the last trade of the underlying, and only of it.
func overlayTrades(data map[string]any, underlying string, shift float64) error {
	trades, ok := data["trades"].(map[string]any)
	if !ok {
		return fmt.Errorf("the get_stock_latest_trade answer has no trades field: the shape upstream has changed")
	}
	for symbol, raw := range trades {
		if !strings.EqualFold(symbol, underlying) {
			continue
		}
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		price, ok := row["p"].(float64)
		if !ok || price <= 0 {
			return fmt.Errorf("the overlay found no price in the last trade of %s", symbol)
		}
		row["p"] = round2(price + shift)
	}

	return nil
}

// overlaySnapshots reprices every contract in a snapshot from the move of the
// underlying, using each contract's own volatility.
//
// A contract the broker sends without a volatility is REFUSED rather than left
// alone. Leaving it alone is the dangerous option: the answer would then hold
// some contracts from the alternative reality and some from this one, the two
// differ by a few cents, and a rule comparing legs would be reading the seam
// between two worlds and calling it an opportunity.
func overlaySnapshots(data map[string]any, spot, shift float64) error {
	snapshots, ok := data["snapshots"].(map[string]any)
	if !ok {
		return fmt.Errorf("the get_option_snapshot answer has no snapshots field: the shape upstream has changed")
	}
	now := time.Now()
	for symbol, raw := range snapshots {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		c, err := parseOCC(symbol)
		if err != nil {
			return fmt.Errorf("the overlay cannot reprice %s: %w", symbol, err)
		}
		sigma, _ := row["impliedVolatility"].(float64)
		if sigma <= 0 {
			return fmt.Errorf("the overlay cannot reprice %s: the broker sends no implied volatility for it, "+
				"and a volatility of our own would make the answer ours rather than the market's", symbol)
		}

		if quote, ok := row["latestQuote"].(map[string]any); ok {
			bid, _ := quote["bp"].(float64)
			ask, _ := quote["ap"].(float64)
			moved := shiftQuote(Quote{Bid: bid, Ask: ask}, c, spot, shift, sigma, now)
			quote["bp"], quote["ap"] = moved.Bid, moved.Ask
		}
		if greeks, ok := row["greeks"].(map[string]any); ok {
			if delta, ok := greeks["delta"].(float64); ok {
				greeks["delta"] = shiftDelta(delta, c, spot, shift, sigma, now)
			}
		}
	}

	return nil
}
