package main

import (
	"math"
	"strings"
	"testing"
	"time"
)

const twoDollarWalk = `{
  "name": "SPY walks two dollars up",
  "mode": "overlay",
  "underlying": "SPY",
  "steps": [
    {"after": "0s", "underlying_delta": 0},
    {"after": "10m", "underlying_delta": 2.0}
  ]
}`

// The overlay's whole claim is that at zero displacement it IS the real market.
// If that is not true to the cent, then every run of it carries a constant
// distortion nobody asked for, and the comparison between two curves measures
// our arithmetic as much as the market.
func TestAnOverlayAtZeroIsTheRealMarketExactly(t *testing.T) {
	s, err := LoadScenario(writeScenario(t, twoDollarWalk))
	if err != nil {
		t.Fatalf("the overlay did not load: %v", err)
	}
	if got := s.shiftAt(0); got != 0 {
		t.Fatalf("at the start the displacement is %v, expected 0", got)
	}

	real := Quote{Bid: 0.31, Ask: 0.37, BidSize: 40, AskSize: 40}
	c, err := parseOCC("SPY260908C00768000")
	if err != nil {
		t.Fatalf("the contract did not parse: %v", err)
	}
	if got := shiftQuote(real, c, 761, 0, 0.17, time.Now()); got != real {
		t.Errorf("at zero displacement the quote came back %+v, expected the real one %+v", got, real)
	}
}

// The curve moves BETWEEN its points and does not teleport. A staircase would
// put the price beyond the strike without ever trading through it, and every
// measurement of what the defence did on the way would have nothing on the way
// to measure.
func TestTheCurveWalksRatherThanJumps(t *testing.T) {
	s, err := LoadScenario(writeScenario(t, twoDollarWalk))
	if err != nil {
		t.Fatalf("the overlay did not load: %v", err)
	}

	half := s.shiftAt(5 * time.Minute)
	if math.Abs(half-1.0) > 1e-9 {
		t.Errorf("halfway the displacement is %v, expected 1.00 - the curve jumped instead of walking", half)
	}
	// Past the last point it holds, rather than running off.
	if got := s.shiftAt(30 * time.Minute); got != 2.0 {
		t.Errorf("past the last point the displacement is %v, expected it to hold at 2.00", got)
	}
}

// A short call spread must get MORE expensive to buy back when the underlying
// walks towards it, and the near leg must move more than the far one. This is
// the reading the whole instrument exists for; if it came out flat, the overlay
// would be answering every question with "nothing happened".
func TestAnOverlayRepricesTowardsTheStrike(t *testing.T) {
	now := time.Now()
	near, err := parseOCC("SPY260908C00768000")
	if err != nil {
		t.Fatalf("the contract did not parse: %v", err)
	}
	far, err := parseOCC("SPY260908C00769000")
	if err != nil {
		t.Fatalf("the contract did not parse: %v", err)
	}

	nearBefore := Quote{Bid: 0.31, Ask: 0.37}
	farBefore := Quote{Bid: 0.24, Ask: 0.29}
	nearAfter := shiftQuote(nearBefore, near, 761, 4, 0.17, now)
	farAfter := shiftQuote(farBefore, far, 761, 4, 0.17, now)

	if nearAfter.Ask <= nearBefore.Ask {
		t.Errorf("the sold leg went from %.2f to %.2f as the underlying walked four dollars towards it",
			nearBefore.Ask, nearAfter.Ask)
	}
	nearMove := nearAfter.Ask - nearBefore.Ask
	farMove := farAfter.Ask - farBefore.Ask
	if farMove >= nearMove {
		t.Errorf("the far leg moved %.4f and the near one %.4f: the nearer strike must move more", farMove, nearMove)
	}
	// And the spread between the two legs is still the scenario's rather than
	// ours: neither leg was allowed to cross the other.
	if nearAfter.Bid > nearAfter.Ask || farAfter.Bid > farAfter.Ask {
		t.Errorf("a leg came back crossed: near %+v, far %+v", nearAfter, farAfter)
	}
}

// The delta is repriced with the price. An agent reads the two together, and a
// delta that stayed at 0.30 while the spot walked through the strike is a pair
// of numbers that cannot both be true.
func TestTheDeltaMovesWithThePrice(t *testing.T) {
	c, _ := parseOCC("SPY260908C00768000")
	moved := shiftDelta(0.30, c, 761, 6, 0.17, time.Now())
	if moved <= 0.30 {
		t.Errorf("the delta came back %.3f after a six-dollar walk towards the strike, expected it above 0.30", moved)
	}
	if moved > 1 {
		t.Errorf("the delta came back %.3f, which is not a delta", moved)
	}
}

// A contract the broker sends without a volatility is refused, not passed
// through. Passing it through is the dangerous option: the answer would then
// hold some contracts from the alternative reality and some from this one, and a
// rule comparing legs would read the seam between two worlds as an opportunity.
func TestAnOverlayRefusesAContractWithNoVolatility(t *testing.T) {
	data := map[string]any{"snapshots": map[string]any{
		"SPY260908C00768000": map[string]any{
			"latestQuote": map[string]any{"bp": 0.31, "ap": 0.37},
		},
	}}
	err := overlaySnapshots(data, 761, 2)
	if err == nil {
		t.Fatal("a contract with no implied volatility was repriced anyway")
	}
	if !strings.Contains(err.Error(), "implied volatility") {
		t.Errorf("the refusal does not name what is missing: %v", err)
	}
}

// Only the overlay's own underlying moves. A run that displaces SPY has said
// nothing about QQQ, and moving it too would be the instrument inventing a
// correlation it was never given.
func TestAnOverlayMovesOnlyItsOwnUnderlying(t *testing.T) {
	data := map[string]any{"trades": map[string]any{
		"SPY": map[string]any{"p": 761.00},
		"QQQ": map[string]any{"p": 512.00},
	}}
	if err := overlayTrades(data, "SPY", 2.5); err != nil {
		t.Fatalf("the trades were not overlaid: %v", err)
	}
	trades := data["trades"].(map[string]any)
	if got := trades["SPY"].(map[string]any)["p"]; got != 763.50 {
		t.Errorf("SPY came back at %v, expected 763.50", got)
	}
	if got := trades["QQQ"].(map[string]any)["p"]; got != 512.00 {
		t.Errorf("QQQ came back at %v: the overlay moved a symbol it was never given", got)
	}
}

// The two modes must not be mixable in one file. Every one of these would
// otherwise run and stage half of one world.
func TestAnOverlayRefusesTheStagedWorldsFields(t *testing.T) {
	for _, bad := range []struct{ name, body, says string }{
		{"a staged clock", `{"name":"x","mode":"overlay","underlying":"SPY","anchor":"now",
			"steps":[{"after":"0s","underlying_delta":0}]}`, "anchor"},
		{"a start", `{"name":"x","mode":"overlay","underlying":"SPY","start":"2026-09-04T13:35:00Z",
			"steps":[{"after":"0s","underlying_delta":0}]}`, "start"},
		{"an invented price", `{"name":"x","mode":"overlay","underlying":"SPY",
			"steps":[{"after":"0s","underlying_price":645}]}`, "underlying_delta"},
		{"quotes of its own", `{"name":"x","mode":"overlay","underlying":"SPY",
			"steps":[{"after":"0s","underlying_delta":0,
			"quotes":{"SPY260904P00640000":{"bid":0.8,"ask":0.9}}}]}`, "quotes no contracts"},
		{"a displacement in a staged market", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z",
			"steps":[{"after":"0s","underlying_delta":2}]}`, "underlying_delta belongs to an overlay"},
		{"an unknown mode", `{"name":"x","mode":"replay","underlying":"SPY",
			"steps":[{"after":"0s","underlying_delta":0}]}`, "mode="},
	} {
		t.Run(bad.name, func(t *testing.T) {
			_, err := LoadScenario(writeScenario(t, bad.body))
			if err == nil {
				t.Fatalf("%s loaded without complaint", bad.name)
			}
			if !strings.Contains(err.Error(), bad.says) {
				t.Errorf("the refusal does not say %q: %v", bad.says, err)
			}
		})
	}
}

// An overlay is on, but it is not STAGING: every place that answers a read out
// of the scenario has to tell the two apart, or an overlay would serve invented
// prices and quietly stop reading the market at all.
func TestAnOverlayIsOnButNotStaging(t *testing.T) {
	s, err := LoadScenario(writeScenario(t, twoDollarWalk))
	if err != nil {
		t.Fatalf("the overlay did not load: %v", err)
	}
	st := newStage(s, time.Now())
	if !st.on() || st.staging() || !st.overlaying() {
		t.Errorf("on=%v staging=%v overlaying=%v: an overlay must be on and not staging",
			st.on(), st.staging(), st.overlaying())
	}
	// And it did not acquire a staged clock on the way through newStage.
	if !s.Start.IsZero() {
		t.Errorf("the overlay was given a start of %s: its clock is the real one", s.Start)
	}
}
