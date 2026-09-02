// The review's tests, rewritten against the mended instrument.
//
// Every one of them was born red and described a DEFECT. Now every one describes
// the mending: where a test used to check "the instrument is wrong in this way",
// it now checks "the instrument counts in this way, and here is why that is
// right". Not one check was thrown away; some were turned round, because the
// behaviour turned round.
//
// What had to change IN THE TESTS themselves, and why: they were written against
// a book that filled an order inside the call (a two-argument NewBook,
// Book.Apply) - the very instantaneity the task said to remove. An order now
// STANDS, so where a test called Apply it now submits an order and gives the
// matcher a tick. The meaning of the checks is preserved word for word.
package main

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fill submits an order and books it straight away, bypassing the matcher. This
// is how the money is checked: the matcher is checked separately, against the
// fake market in harness_test.go.
func fill(t *testing.T, b *Book, legs []Leg, sets int, limit, executable float64, price map[string]float64, clientID string) (Order, error) {
	t.Helper()

	o := &Order{ClientID: clientID, Qty: sets, Limit: limit, TIF: "day", Legs: legs, SubmittedAt: time.Now()}
	if err := b.Submit(o); err != nil {
		return Order{}, err
	}
	if _, err := b.Fill(o.ID, sets, executable, price, time.Now()); err != nil {
		return Order{}, err
	}
	after, _ := b.ByID(o.ID)

	return after, nil
}

func legsOfRatio(t *testing.T, legs ...Leg) []Leg {
	t.Helper()

	out := make([]Leg, 0, len(legs))
	for _, leg := range legs {
		n, err := parseCount(leg.RatioQty)
		if err != nil {
			t.Fatalf("ratio_qty %q: %v", leg.RatioQty, err)
		}
		leg.Ratio = n
		out = append(out, leg)
	}

	return out
}

// ---------- parsing ratio_qty ----------

// The old atoiOr quietly returned one for "2.0", " 2", "0" and "-2": a leg of a
// spread came out half the size ordered, the risk profile stopped being the one
// agreed, and the fee came out half. The neighbouring qty refused the same input
// honestly, and that disagreement was the defect.
func TestParseCountRefusesEverythingButDigits(t *testing.T) {
	good := map[string]int{"1": 1, "2": 2, "10": 10}
	for in, want := range good {
		got, err := parseCount(in)
		if err != nil || got != want {
			t.Errorf("parseCount(%q) = %d, %v; expected %d and no error", in, got, err, want)
		}
	}

	for _, in := range []string{"", "2.0", " 2", "0", "-2", "+2", "1e1", "two", "99999999999999999999"} {
		if got, err := parseCount(in); err == nil {
			t.Errorf("parseCount(%q) = %d and no error: the quiet substitution is back", in, got)
		}
	}
}

func TestOrderRefusesUnparsableRatio(t *testing.T) {
	for _, ratio := range []string{"2.0", " 2", "0", "-2"} {
		_, err := legsOf(orderArgs{Qty: "1", Legs: []Leg{
			{Symbol: "SPY260828P00640000", Side: "buy", RatioQty: ratio},
		}})
		if err == nil {
			t.Errorf("ratio_qty=%q was accepted: a leg of the order would have drifted silently", ratio)
		}
	}
}

// ---------- Worst ----------

func mustWorst(t *testing.T, ps []Position) float64 {
	t.Helper()

	w, err := Worst(ps)
	if err != nil {
		t.Fatalf("Worst: %v", err)
	}

	return w
}

func TestWorstShortPutSpread(t *testing.T) {
	// sold the 640 put, bought the 635 put: the maximum loss is 5.00 * 100 = 500
	w := mustWorst(t, []Position{
		{Symbol: "SPY260828P00640000", Qty: -1},
		{Symbol: "SPY260828P00635000", Qty: +1},
	})
	if math.Abs(w-(-500)) > 1e-6 {
		t.Fatalf("the worst case of the put spread = %.2f, expected -500", w)
	}
}

// A naked short call: the loss is unbounded, and that is the ONLY right answer.
// The old code substituted "the last strike times two plus one" and got a
// number.
func TestWorstNakedShortCall(t *testing.T) {
	w := mustWorst(t, []Position{{Symbol: "SPY260828C00700000", Qty: -1}})
	if !math.IsInf(w, -1) {
		t.Fatalf("naked short C700: Worst = %.2f, expected -Inf", w)
	}
}

// The collateral required used to depend on the LEVEL of the strike: a C700 cost
// 70,100 and a C100 cost 10,100, though the risk on both is equally unbounded.
func TestWorstDoesNotDependOnStrikeLevel(t *testing.T) {
	lo := mustWorst(t, []Position{{Symbol: "SPY260828C00100000", Qty: -1}})
	hi := mustWorst(t, []Position{{Symbol: "SPY260828C00700000", Qty: -1}})
	if !math.IsInf(lo, -1) || !math.IsInf(hi, -1) {
		t.Fatalf("short C100 = %.2f, short C700 = %.2f: both are required to be -Inf", lo, hi)
	}
}

// A deep in-the-money short call: the strike is small, the old "far point" stood
// close by, and the collateral came out laughable next to the obligation.
func TestWorstDeepITMShortCall(t *testing.T) {
	w := mustWorst(t, []Position{{Symbol: "SPY260828C00050000", Qty: -100}})
	if !math.IsInf(w, -1) {
		t.Fatalf("100 short C50: Worst = %.2f, expected -Inf", w)
	}
}

func TestWorstIgnoresUnderlyingConsistencyAcrossExpiries(t *testing.T) {
	// the same SPY, two dates: the groups are counted apart and added up. That is
	// stricter than the truth and was chosen deliberately.
	near := mustWorst(t, []Position{
		{Symbol: "SPY260828C00650000", Qty: -1},
		{Symbol: "SPY260828C00655000", Qty: +1},
	})
	far := mustWorst(t, []Position{{Symbol: "SPY260904C00650000", Qty: +1}})
	both := mustWorst(t, []Position{
		{Symbol: "SPY260828C00650000", Qty: -1},
		{Symbol: "SPY260828C00655000", Qty: +1},
		{Symbol: "SPY260904C00650000", Qty: +1},
	})
	if math.Abs(both-(near+far)) > 1e-6 {
		t.Fatalf("the near %.2f + the far %.2f != both together %.2f", near, far, both)
	}
}

func TestWorstUnbalancedPutSpreadIsCaught(t *testing.T) {
	// 2 short 640 puts against 1 long 635: the worst case sits at zero
	w := mustWorst(t, []Position{
		{Symbol: "SPY260828P00640000", Qty: -2},
		{Symbol: "SPY260828P00635000", Qty: +1},
	})
	want := float64(-2*640*100 + 1*635*100)
	if math.Abs(w-want) > 1e-6 {
		t.Fatalf("Worst=%.2f, expected %.2f", w, want)
	}
}

func TestWorstUnbalancedCallSpreadIsCaughtToo(t *testing.T) {
	// 2 short 650 calls against 1 long 655: the loss is unbounded, and now that
	// is visible. This used to come out as a finite number.
	w := mustWorst(t, []Position{
		{Symbol: "SPY260828C00650000", Qty: -2},
		{Symbol: "SPY260828C00655000", Qty: +1},
	})
	if !math.IsInf(w, -1) {
		t.Fatalf("Worst=%.2f, expected -Inf", w)
	}
}

func TestWorstCoveredCallStaysUnbounded(t *testing.T) {
	// A limitation taken deliberately and said out loud: shares are not part of
	// the line, and the arena still counts a call sold against shares held as
	// unbounded. We trade only structures whose maximum loss is known, and a
	// covered call is not among them.
	w := mustWorst(t, []Position{
		{Symbol: "SPY260828C00650000", Qty: -1, Class: classOption},
		{Symbol: "SPY", Qty: 100, Class: classEquity, Mark: 640},
	})
	if !math.IsInf(w, -1) {
		t.Fatalf("covered call: Worst=%.2f, expected -Inf", w)
	}
}

// ---------- Requirement ----------

func TestRequirementPutSpread(t *testing.T) {
	need, err := Requirement([]Position{
		{Symbol: "SPY260828P00640000", Qty: -1, Class: classOption},
		{Symbol: "SPY260828P00635000", Qty: +1, Class: classOption},
	})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(need-500) > 1e-6 {
		t.Fatalf("the requirement %.2f, expected 500", need)
	}
}

func TestRequirementShortShares(t *testing.T) {
	// Short shares: a hundred percent of the proceeds are already in the cash plus
	// fifty on top, the way Reg T counts it. The worst case here is infinite, and
	// a broker does not count it.
	need, err := Requirement([]Position{{Symbol: "SPY", Qty: -100, Class: classEquity, Mark: 640}})
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(need-1.5*100*640) > 1e-6 {
		t.Fatalf("the requirement %.2f, expected %.2f", need, 1.5*100*640.0)
	}
}

func TestRequirementLongSharesFree(t *testing.T) {
	// Long shares are paid for in full: they create no requirement.
	need, err := Requirement([]Position{{Symbol: "SPY", Qty: 100, Class: classEquity, Mark: 640}})
	if err != nil {
		t.Fatal(err)
	}
	if need != 0 {
		t.Fatalf("the requirement %.2f, expected 0", need)
	}
}

// ---------- parseOCC ----------

func TestParseOCC(t *testing.T) {
	cases := []string{
		"SPY260828C00500000",
		"SPXW260828P05000000",
		"A260828C00050000",
		"BRKB260828C00500000",
	}
	for _, s := range cases {
		if _, err := parseOCC(s); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
}

// A negative strike used to pass (Atoi takes a sign) and IMPROVED the book's
// worst case, and the date was not checked at all.
func TestParseOCCRefusesGarbage(t *testing.T) {
	for _, s := range []string{
		"SPY260828C-0050000", // a strike with a sign
		"SPYZZZZZZC00500000", // no such date
		"SPY261332C00500000", // a thirteenth month
		"SPY260828C00000000", // a zero strike
		"SPY260828X00500000", // neither a call nor a put
		"260828C00500000",    // no root
	} {
		if c, err := parseOCC(s); err == nil {
			t.Errorf("%q parsed as %+v: rubbish was taken for a contract", s, c)
		}
	}
}

// ---------- the book ----------

func TestCreditSpreadCash(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	legs := legsOfRatio(t,
		Leg{Symbol: "SPY260828P00640000", Side: "sell", RatioQty: "1"},
		Leg{Symbol: "SPY260828P00635000", Side: "buy", RatioQty: "1"},
	)
	// sold the 640 put at the 2.00 bid, bought the 635 put at the 1.20 ask -> a credit of 0.80
	executable := -2.00 + 1.20
	price := map[string]float64{"SPY260828P00640000": 2.00, "SPY260828P00635000": 1.20}

	o, err := fill(t, b, legs, 1, -0.80, executable, price, "c1")
	if err != nil {
		t.Fatalf("it did not fill: %v", err)
	}
	if o.Status != statusFilled {
		t.Fatalf("status %q, expected %q", o.Status, statusFilled)
	}

	wantCash := 100000 + 80 - 2*perContract
	if math.Abs(b.Cash-wantCash) > 1e-9 {
		t.Fatalf("cash %.4f, expected %.4f", b.Cash, wantCash)
	}
	if math.Abs(o.FilledAvg-(-0.80)) > 1e-9 {
		t.Fatalf("the average fill price %.4f, expected -0.80", o.FilledAvg)
	}
}

func TestAvgPriceOnFlip(t *testing.T) {
	b := NewBook("tok", 100000000, nil)
	sym := "SPY260828P00640000"

	// bought 5 at 2.00
	if _, err := fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "buy", RatioQty: "5"}),
		1, 99, 2.00*5, map[string]float64{sym: 2.00}, "a"); err != nil {
		t.Fatal(err)
	}
	if p := b.Positions[sym]; p == nil || p.Qty != 5 || math.Abs(p.AvgPrice-2.00) > 1e-9 {
		t.Fatalf("after the buy: %+v", p)
	}

	// sold 8 at 3.00 -> the position became -3, and the entry price now belongs to
	// the new side. The long position's price used to stay here, and the P&L of
	// the short was counted from a number belonging to somebody else.
	if _, err := fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "sell", RatioQty: "8"}),
		1, 99, -3.00*8, map[string]float64{sym: 3.00}, "b"); err != nil {
		t.Fatal(err)
	}
	p := b.Positions[sym]
	if p == nil || p.Qty != -3 {
		t.Fatalf("after the flip: %+v", p)
	}
	if math.Abs(p.AvgPrice-3.00) > 1e-9 {
		t.Fatalf("the average entry price %.4f, expected 3.0000", p.AvgPrice)
	}
}

func TestAvgPriceWeightedByQuantity(t *testing.T) {
	b := NewBook("tok", 100000000, nil)
	sym := "SPY260828P00640000"

	fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "buy", RatioQty: "100"}),
		1, 999, 1.00*100, map[string]float64{sym: 1.00}, "a")
	fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "buy", RatioQty: "1"}),
		1, 999, 2.00, map[string]float64{sym: 2.00}, "b")

	p := b.Positions[sym]
	want := (1.00*100 + 2.00*1) / 101
	if math.Abs(p.AvgPrice-want) > 1e-9 {
		t.Fatalf("the average %.6f, expected %.6f: weighted by quantity, not by the number of orders", p.AvgPrice, want)
	}
}

func TestSameSymbolTwiceInOrder(t *testing.T) {
	// Two legs on one symbol: legPrice is a map by symbol, and the price of both
	// legs is the same. The position is required to add up rather than be
	// overwritten.
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828P00640000"
	legs := legsOfRatio(t,
		Leg{Symbol: sym, Side: "buy", RatioQty: "1"},
		Leg{Symbol: sym, Side: "buy", RatioQty: "1"},
	)
	if _, err := fill(t, b, legs, 1, 99, 4.00, map[string]float64{sym: 2.00}, "a"); err != nil {
		t.Fatal(err)
	}
	p := b.Positions[sym]
	if p.Qty != 2 {
		t.Fatalf("position %d, expected 2", p.Qty)
	}
	if math.Abs(b.Cash-(100000-400-2*perContract)) > 1e-9 {
		t.Fatalf("cash %.4f", b.Cash)
	}
}

// ---------- parsing the arguments ----------

func TestRemarshalNumberInsteadOfString(t *testing.T) {
	raw := json.RawMessage(`{"qty":2,"limit_price":-0.8,"legs":[{"symbol":"SPY260828P00640000","side":"sell","ratio_qty":1}]}`)
	var in orderArgs
	if err := remarshal(raw, &in); err == nil {
		t.Errorf("a number where a string belongs was accepted: %+v", in)
	}
}

func TestMarketOrderHasNoLimit(t *testing.T) {
	raw := json.RawMessage(`{"qty":"1","type":"market","legs":[{"symbol":"SPY260828P00640000","side":"sell","ratio_qty":"1"}]}`)
	var in orderArgs
	if err := remarshal(raw, &in); err != nil {
		t.Fatalf("%v", err)
	}
	if in.LimitPrice != "" {
		t.Fatalf("limit_price=%q, expected empty", in.LimitPrice)
	}
}

// ---------- collateral ----------

func TestCollateralRefusesNakedShortCall(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828C00700000"

	_, err := fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "sell", RatioQty: "1"}),
		1, -1.00, -1.00, map[string]float64{sym: 1.00}, "n")
	if err == nil {
		t.Fatalf("a naked short call was accepted: cash %.2f, positions %+v", b.Cash, b.Positions[sym])
	}
	if b.Cash != 100000 {
		t.Fatalf("the cash was touched by a refused order: %.2f", b.Cash)
	}
}

func TestCollateralRefusesDeepITMShortCall(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828C00050000"

	if _, err := fill(t, b, legsOfRatio(t, Leg{Symbol: sym, Side: "sell", RatioQty: "20"}),
		1, -590.0, -590.0, map[string]float64{sym: 590.0}, "n2"); err == nil {
		t.Fatalf("twenty short C50 were accepted: cash %.2f", b.Cash)
	}
}

func TestCollateralCountsStandingOrders(t *testing.T) {
	// Ten identical sold spreads each pass the collateral check on their own. A
	// broker holds collateral against an order from the moment it is submitted,
	// and so do we: otherwise together they ruin the account.
	b := NewBook("tok", 600, nil)
	legs := legsOfRatio(t,
		Leg{Symbol: "SPY260828P00640000", Side: "sell", RatioQty: "1"},
		Leg{Symbol: "SPY260828P00635000", Side: "buy", RatioQty: "1"},
	)

	first := &Order{Qty: 1, Limit: -0.80, TIF: "day", Legs: legs, SubmittedAt: time.Now()}
	if err := b.Submit(first); err != nil {
		t.Fatalf("the first spread was refused: %v", err)
	}
	// 600 of cash plus an 80 credit cover the collateral for one spread (500) and
	// do not cover two (1000), even though the cash only grows.
	second := &Order{Qty: 1, Limit: -0.80, TIF: "day", Legs: legs, SubmittedAt: time.Now()}
	if err := b.Submit(second); err == nil {
		t.Fatalf("the second spread was accepted although there is no collateral for both")
	}
}

// ---------- the life of an order ----------

func TestOrderRestsUntilFilled(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	legs := legsOfRatio(t, Leg{Symbol: "SPY260828P00640000", Side: "buy", RatioQty: "1"})

	o := &Order{Qty: 2, Limit: 2.00, TIF: "day", Legs: legs, SubmittedAt: time.Now()}
	if err := b.Submit(o); err != nil {
		t.Fatal(err)
	}
	if o.Status != statusNew {
		t.Fatalf("status %q, expected %q", o.Status, statusNew)
	}
	if b.Cash != 100000 {
		t.Fatalf("the cash was touched before the fill: %.2f", b.Cash)
	}

	// Partial fills accumulate.
	if _, err := b.Fill(o.ID, 1, 1.90, map[string]float64{"SPY260828P00640000": 1.90}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ := b.ByID(o.ID)
	if got.Status != statusPartial || got.FilledQty != 1 {
		t.Fatalf("after the first part: %s %d", got.Status, got.FilledQty)
	}

	if _, err := b.Fill(o.ID, 1, 2.00, map[string]float64{"SPY260828P00640000": 2.00}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got, _ = b.ByID(o.ID)
	if got.Status != statusFilled || got.FilledQty != 2 {
		t.Fatalf("after the second part: %s %d", got.Status, got.FilledQty)
	}
	// The average is by sets, not by the last price.
	if math.Abs(got.FilledAvg-1.95) > 1e-9 {
		t.Fatalf("the average price %.4f, expected 1.95", got.FilledAvg)
	}
	if math.Abs(b.Cash-(100000-390-2*perContract)) > 1e-6 {
		t.Fatalf("cash %.4f", b.Cash)
	}
}

func TestCancelAndReplace(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	legs := legsOfRatio(t, Leg{Symbol: "SPY260828P00640000", Side: "buy", RatioQty: "1"})
	now := time.Now()

	o := &Order{Qty: 2, Limit: 1.00, TIF: "day", Legs: legs, SubmittedAt: now}
	if err := b.Submit(o); err != nil {
		t.Fatal(err)
	}

	// A move at Alpaca is a NEW order: the old one becomes replaced.
	next, err := b.Replace(o.ID, 1.50, "", 0, now)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID == o.ID {
		t.Fatalf("the move returned the same identifier")
	}
	old, _ := b.ByID(o.ID)
	if old.Status != statusReplaced || old.ReplacedBy != next.ID {
		t.Fatalf("the old order: %s -> %q", old.Status, old.ReplacedBy)
	}
	if next.Replaces != o.ID || next.Qty != 2 || next.Limit != 1.50 {
		t.Fatalf("the new order: %+v", next)
	}

	if _, err := b.Cancel(next.ID, now); err != nil {
		t.Fatal(err)
	}
	canceled, _ := b.ByID(next.ID)
	if canceled.Status != statusCanceled {
		t.Fatalf("the status after the cancel: %s", canceled.Status)
	}
	if _, err := b.Cancel(next.ID, now); err == nil {
		t.Fatalf("a cancelled order cancelled twice")
	}
}

func TestDayOrderExpiresWithSession(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	legs := legsOfRatio(t, Leg{Symbol: "SPY260828P00640000", Side: "buy", RatioQty: "1"})
	closeAt := time.Date(2026, 8, 28, 16, 0, 0, 0, exchangeZone)

	day := &Order{Qty: 1, Limit: 1.00, TIF: "day", Legs: legs, SubmittedAt: closeAt.Add(-time.Hour), ExpiresAt: closeAt}
	gtc := &Order{Qty: 1, Limit: 1.00, TIF: "gtc", Legs: legs, SubmittedAt: closeAt.Add(-time.Hour)}
	if err := b.Submit(day); err != nil {
		t.Fatal(err)
	}
	if err := b.Submit(gtc); err != nil {
		t.Fatal(err)
	}

	if gone := b.ExpireDay(closeAt.Add(-time.Minute)); len(gone) != 0 {
		t.Fatalf("the order was withdrawn before the end of the session: %+v", gone)
	}
	gone := b.ExpireDay(closeAt.Add(time.Second))
	if len(gone) != 1 || gone[0].ID != day.ID {
		t.Fatalf("%d orders were withdrawn after the close", len(gone))
	}
	if got, _ := b.ByID(day.ID); got.Status != statusExpired {
		t.Fatalf("the day order: %s", got.Status)
	}
	if got, _ := b.ByID(gtc.ID); got.Status != statusNew {
		t.Fatalf("the gtc order was withdrawn along with the day order: %s", got.Status)
	}
}

// ---------- expiry ----------

func TestSettleOutOfTheMoneyVanishes(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828P00600000"
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.20, Class: classOption}

	e, err := b.Settle(sym, 640, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != "expiry" {
		t.Fatalf("the kind of event %q", e.Kind)
	}
	if _, ok := b.Positions[sym]; ok {
		t.Fatalf("the position was not written off")
	}
	if b.Cash != 100000 {
		t.Fatalf("the cash was touched by an empty expiry: %.2f", b.Cash)
	}
}

func TestSettleAssignedShortPutBecomesShares(t *testing.T) {
	// A sold put in the money: assignment, a hundred shares per contract and the
	// strike debited. This is the case the settlement was written for.
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828P00640000"
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 2.00, Class: classOption}

	e, err := b.Settle(sym, 638.50, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != "assignment" {
		t.Fatalf("the kind of event %q, expected assignment", e.Kind)
	}
	shares := b.Positions["SPY"]
	if shares == nil || shares.Qty != 100 || shares.Class != classEquity {
		t.Fatalf("the shares: %+v", shares)
	}
	if math.Abs(shares.AvgPrice-640) > 1e-9 {
		t.Fatalf("the entry price of the shares %.2f, expected 640 (the strike)", shares.AvgPrice)
	}
	if math.Abs(b.Cash-(100000-64000)) > 1e-6 {
		t.Fatalf("cash %.2f, expected %.2f", b.Cash, 100000-64000.0)
	}
}

func TestSettleExercisedLongCallBuysShares(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828C00600000"
	b.Positions[sym] = &Position{Symbol: sym, Qty: 1, AvgPrice: 2.00, Class: classOption}

	e, err := b.Settle(sym, 640, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != "exercise" {
		t.Fatalf("the kind of event %q", e.Kind)
	}
	if shares := b.Positions["SPY"]; shares == nil || shares.Qty != 100 {
		t.Fatalf("the shares: %+v", shares)
	}
	if math.Abs(b.Cash-(100000-60000)) > 1e-6 {
		t.Fatalf("cash %.2f", b.Cash)
	}
}

// The price closed BETWEEN the legs of a sold spread: the short leg was
// assigned, the long expired empty, and instead of a bounded loss the account
// held seventy thousand dollars' worth of shares. The instrument is required to
// reproduce this - it is the strategy's main risk, not a rare case.
func TestSettlePinBetweenLegs(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	short, long := "SPY260828P00640000", "SPY260828P00635000"
	b.Positions[short] = &Position{Symbol: short, Qty: -1, AvgPrice: 2.00, Class: classOption}
	b.Positions[long] = &Position{Symbol: long, Qty: 1, AvgPrice: 1.20, Class: classOption}

	if _, err := b.Settle(short, 637.00, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Settle(long, 637.00, time.Now()); err != nil {
		t.Fatal(err)
	}

	shares := b.Positions["SPY"]
	if shares == nil || shares.Qty != 100 {
		t.Fatalf("after closing between the legs the shares are %+v, expected +100", shares)
	}
	if math.Abs(b.Cash-(100000-64000)) > 1e-6 {
		t.Fatalf("cash %.2f, expected %.2f", b.Cash, 100000-64000.0)
	}
	// The loss on the position shows only against the market: 100 shares bought at
	// 640 with the market at 637, that is minus 300 on top of the credit
	// received.
	need, err := Requirement([]Position{*shares})
	if err != nil || need != 0 {
		t.Fatalf("long shares demand collateral of %.2f: %v", need, err)
	}
}

func TestSettleCentInTheMoneyIsExercised(t *testing.T) {
	// The OCC's threshold is exactly a cent. In the money by a cent it is
	// exercised, by half a cent it is not.
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828C00640000"

	b.Positions[sym] = &Position{Symbol: sym, Qty: 1, AvgPrice: 0.05, Class: classOption}
	if _, err := b.Settle(sym, 640.01, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b.Positions["SPY"] == nil {
		t.Fatalf("in the money by a cent, the contract was not exercised")
	}

	b2 := NewBook("tok2", 100000, nil)
	b2.Positions[sym] = &Position{Symbol: sym, Qty: 1, AvgPrice: 0.05, Class: classOption}
	if _, err := b2.Settle(sym, 640.005, time.Now()); err != nil {
		t.Fatal(err)
	}
	if b2.Positions["SPY"] != nil {
		t.Fatalf("in the money by half a cent, the contract was exercised")
	}
}

func TestDueOnlyAfterClose(t *testing.T) {
	b := NewBook("tok", 100000, nil)
	sym := "SPY260828P00640000"
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, Class: classOption}

	noon := time.Date(2026, 8, 28, 12, 0, 0, 0, exchangeZone)
	if due := b.Due(noon); len(due) != 0 {
		t.Fatalf("the contract was declared expired at noon on its expiry day")
	}
	after := time.Date(2026, 8, 28, 16, 0, 1, 0, exchangeZone)
	if due := b.Due(after); len(due) != 1 {
		t.Fatalf("%d expired after the close", len(due))
	}
}

// A note lives while an order is open - and a fact about the fill has to outlive
// it. Found by a trial on 29 August: the bench-mode mark was being set through
// Note AFTER the fill, and it quietly did not land, because a filled order is no
// longer open. The instrument looked sound the whole time.
func TestTheBenchModeMarkOutlivesTheFill(t *testing.T) {
	book := NewBook("token", 100000, nil)
	legs := []Leg{
		{Symbol: "SPY260904P00755000", Side: "sell", Ratio: 1, PositionIntent: "sell_to_open"},
		{Symbol: "SPY260904P00750000", Side: "buy", Ratio: 1, PositionIntent: "buy_to_open"},
	}
	now := time.Date(2026, 8, 29, 7, 19, 0, 0, time.UTC)
	o := &Order{ID: "probe-1", ClientID: "probe", Qty: 1, Limit: -0.10, TIF: "day", Legs: legs, SubmittedAt: now}
	if err := book.Submit(o); err != nil {
		t.Fatalf("the order was not accepted: %v", err)
	}

	if _, err := book.Fill(o.ID, 1, -0.28, map[string]float64{
		"SPY260904P00755000": 0.75, "SPY260904P00750000": 0.47,
	}, now); err != nil {
		t.Fatalf("the fill did not go through: %v", err)
	}

	book.Note(o.ID, "this reason must not land: the order has already filled")
	book.MarkStand(o.ID)

	got := book.Orders[o.ID]
	if !got.Stand {
		t.Error("the bench-mode mark did not outlive the fill - and it is about the fill, not about its absence")
	}
	if got.Why != "" {
		t.Errorf("a reason for not filling landed on a filled order: %q", got.Why)
	}
}

// A refusal is required to NAME what was broken rather than leave the agent to
// derive it from arithmetic. Found by a trial on 29 August: to a naked short the
// arena answered "a requirement of +Inf", the participant derived the rule
// unaided and said plainly that this had been a guess rather than a disclosure.
func TestACollateralRefusalNamesWhatBrokeAndWhatToChange(t *testing.T) {
	naked := []Position{{Symbol: "SPY260904C00785000", Qty: -1}}

	why := Unbounded(naked)
	for _, needed := range []string{"no ceiling", "785.00", "buy a call", "difference between the strikes", "not about the size of the account"} {
		if !strings.Contains(why, needed) {
			t.Errorf("the refusal does not carry %q; the refusal in full: %s", needed, why)
		}
	}

	// A bounded loss gets no explanation about infinity: the advice there is the
	// exact opposite - reduce the size.
	spread := []Position{
		{Symbol: "SPY260904C00785000", Qty: -1},
		{Symbol: "SPY260904C00790000", Qty: 1},
	}
	if got := Unbounded(spread); got != "" && strings.Contains(got, "no ceiling") {
		t.Errorf("an honest spread was declared unbounded: %s", got)
	}

	short := shortfall(spread, 500, 100)
	if !strings.Contains(short, "400.00 short") || !strings.Contains(short, "in fewer sets") {
		t.Errorf("the refusal by size does not name what is short and by how much: %s", short)
	}
}

// The account is obliged to speak about fees itself. Found by the arena's second
// participant: it read accrued_fees, saw a zero and wrote that "the arena charges
// no commission" - though it had been charged for that very trade. Positions are
// sized from the account, not from the order.
func TestTheAccountShowsTheFeesPaid(t *testing.T) {
	book := NewBook("token", 100000, nil)
	legs := []Leg{
		{Symbol: "SPY260904P00756000", Side: "sell", Ratio: 1, PositionIntent: "sell_to_open"},
		{Symbol: "SPY260904P00751000", Side: "buy", Ratio: 1, PositionIntent: "buy_to_open"},
	}
	now := time.Date(2026, 8, 29, 7, 50, 0, 0, time.UTC)
	o := &Order{ID: "probe-fees", Qty: 1, Limit: -0.10, TIF: "day", Legs: legs, SubmittedAt: now}
	if err := book.Submit(o); err != nil {
		t.Fatalf("the order was not accepted: %v", err)
	}
	if _, err := book.Fill(o.ID, 1, -0.33, map[string]float64{
		"SPY260904P00756000": 0.85, "SPY260904P00751000": 0.52,
	}, now); err != nil {
		t.Fatalf("the fill did not go through: %v", err)
	}

	// Two legs of one contract each, 0.025 per contract per leg.
	if got := book.AccruedFees(); got < 0.049 || got > 0.051 {
		t.Errorf("the account shows fees of %.3f, while 0.050 was taken", got)
	}
}

// The judge stitches an intent to an order by the turn reference, and the
// reference arrives in the tail of the name the agent gave the order. We parse
// exactly the turn= field and not "something that looks like one": parsing by
// shape is a guess, and an order stitched at random is worse than an unstitched
// one, because it looks the same.
func TestTheTurnReferenceIsTakenFromTheFieldNotGuessed(t *testing.T) {
	cases := []struct{ id, want string }{
		{"worst=-0.11;turn=tu-7;QQQ703-702", "tu-7"},
		{"turn=mb-1787-t3", "mb-1787-t3"},
		{" worst=1 ; turn = tu-9 ", "tu-9"},
		{"worst=-0.11;QQQ703-702", ""},
		{"", ""},
		{"turnip=tu-7", ""},
		{"arena1-liveness-20260829-0713", ""},
	}
	for _, c := range cases {
		if got := turnOf(c.id); got != c.want {
			t.Errorf("turnOf(%q) = %q, expected %q", c.id, got, c.want)
		}
	}
}

// The turn has to reach the BOOK, not merely the name of the order.
//
// Found on 29 August, the moment record_intent began answering with the turn at
// all: the agent put `turn=` into client_order_id exactly as asked, the arena
// wrote the order, and turn_ref in the book stayed empty. The turn was taken out
// of the name in Replace and not in placeOrder - one of the two paths - so every
// new order arrived unstitched while replacements carried a turn. Nothing showed
// it earlier, because a field nobody fills reads exactly like a field nobody
// sends.
func TestTheTurnReachesTheBookOnBothPaths(t *testing.T) {
	book := NewBook("token", 100000, nil)
	legs := []Leg{
		{Symbol: "SPY260904P00754000", Side: "sell", Ratio: 1, PositionIntent: "sell_to_open"},
		{Symbol: "SPY260904P00749000", Side: "buy", Ratio: 1, PositionIntent: "buy_to_open"},
	}
	now := time.Date(2026, 8, 29, 14, 48, 0, 0, time.UTC)

	// A new order: the turn comes from the name the agent gave it.
	o := &Order{ClientID: "turn=mb-1788014569803892751-t2;SPY754-749P;sz=1",
		Qty: 1, Limit: -0.10, TIF: "day", Legs: legs, SubmittedAt: now}
	if err := book.Submit(o); err != nil {
		t.Fatalf("the order was not accepted: %v", err)
	}
	if got := book.Orders[o.ID].TurnRef; got != "mb-1788014569803892751-t2" {
		t.Errorf("a new order reached the book with turn_ref %q: the judge has nothing to stitch it by", got)
	}

	// A replacement given no name of its own inherits the old name - and the turn
	// lives inside that name.
	next, err := book.Replace(o.ID, -0.20, "", 0, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("the move refused: %v", err)
	}
	if next.TurnRef != "mb-1788014569803892751-t2" {
		t.Errorf("the moved order carries turn_ref %q: the turn was lost in the move", next.TurnRef)
	}

	// And a replacement named afresh carries the turn of its own name.
	third, err := book.Replace(next.ID, -0.30, "turn=mb-9-t5;SPY754-749P", 0, now.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("the second move refused: %v", err)
	}
	if third.TurnRef != "mb-9-t5" {
		t.Errorf("a renamed move carries turn_ref %q, expected mb-9-t5", third.TurnRef)
	}
}

// A field set AFTER the row exists has to survive the write.
//
// Adding the columns was only half of it, and the half that shows in a schema.
// An order is inserted when it is submitted and updated on every change after
// that, and the update named neither turn_ref nor stand - so MarkStand, which
// fires only once an order has filled, wrote to memory and never to the file. A
// week later a fill against a frozen book would have been indistinguishable from
// a real one, which is the exact thing the mark exists to prevent.
func TestTheStandMarkSurvivesTheWrite(t *testing.T) {
	dir := t.TempDir() + "/book.db"
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("the store did not open: %v", err)
	}

	book := NewBook("token", 100000, store)
	legs := []Leg{
		{Symbol: "SPY260904P00754000", Side: "sell", Ratio: 1, PositionIntent: "sell_to_open"},
		{Symbol: "SPY260904P00749000", Side: "buy", Ratio: 1, PositionIntent: "buy_to_open"},
	}
	now := time.Date(2026, 8, 29, 14, 48, 0, 0, time.UTC)
	o := &Order{ClientID: "turn=mb-9-t1;SPY754-749P", Qty: 1, Limit: -0.10, TIF: "day", Legs: legs, SubmittedAt: now}
	if err := book.Submit(o); err != nil {
		t.Fatalf("the order was not accepted: %v", err)
	}
	if _, err := book.Fill(o.ID, 1, -0.28, map[string]float64{
		"SPY260904P00754000": 0.75, "SPY260904P00749000": 0.47,
	}, now); err != nil {
		t.Fatalf("the fill did not go through: %v", err)
	}
	// Only now, after the row has been inserted and updated at least once.
	book.MarkStand(o.ID)
	store.Close()

	again, err := OpenStore(dir)
	if err != nil {
		t.Fatalf("the store did not reopen: %v", err)
	}
	defer again.Close()

	back := NewBook("token", 100000, again)
	if found, err := again.Load(back); err != nil || !found {
		t.Fatalf("the book was not raised: found=%v err=%v", found, err)
	}
	got := back.Orders[o.ID]
	if got == nil {
		t.Fatalf("the order was lost in the restart")
	}
	if !got.Stand {
		t.Error("the bench mark did not survive the write: a frozen-book fill now reads as a real one")
	}
	if got.TurnRef != "mb-9-t1" {
		t.Errorf("the turn did not survive the write: %q", got.TurnRef)
	}
}

// "Now" is the one answer a cache may not serve as it stands.
//
// The broker's clock is read on every tick and on every order, so it is held for
// a few seconds rather than fetched each time. What is held is the READING; the
// time inside it is advanced by however long ago the reading was taken. Without
// that, an order carries a timestamp older than itself - measured on 29 August
// as 9.4 seconds, which put the order BEFORE the intent that had preceded it and
// made the judge report an honest agent as one that wrote its intent afterwards.
func TestTheHeldClockAdvancesWithRealTime(t *testing.T) {
	a := &arena{clockFor: time.Minute}
	taken := time.Date(2026, 8, 29, 14, 48, 0, 0, time.UTC)
	a.heldClock = Clock{IsOpen: true, Now: taken, NextClose: taken.Add(time.Hour)}
	a.heldAt = time.Now().Add(-9400 * time.Millisecond)

	got, err := a.clock(context.Background(), prioTrade)
	if err != nil {
		t.Fatalf("the clock was not read: %v", err)
	}

	// The reading was taken 9.4 seconds ago, so now is 9.4 seconds later than it.
	if drift := got.Now.Sub(taken); drift < 9*time.Second || drift > 10*time.Second {
		t.Errorf("now moved by %s since the reading, expected about 9.4s: a stale now is wrong, not merely old", drift)
	}
	// The session boundaries are still true whenever they were read, so they are
	// handed back untouched.
	if !got.NextClose.Equal(taken.Add(time.Hour)) {
		t.Errorf("next_close was moved to %s: a boundary does not drift with the clock", got.NextClose)
	}
}

// A crossed quote is a broken reading, not a market, and must not fill.
//
// With the bid above the ask the sell side pays more than the buy side costs, so
// one set opened and closed makes money with no risk taken at all - profit
// printed out of a bad tick. Their gateway met eleven crossed quotes in two
// hours on 29 August, which is why this is a refusal and not a warning.
func TestACrossedQuoteDoesNotFill(t *testing.T) {
	legs := []Leg{{Symbol: "SPY260904P00754000", Side: "sell", Ratio: 1}}
	now := time.Date(2026, 8, 29, 14, 0, 0, 0, time.UTC)

	crossed := map[string]Quote{
		"SPY260904P00754000": {Symbol: "SPY260904P00754000", Bid: 1.20, Ask: 0.80, BidSize: 10, AskSize: 10, At: now},
	}
	_, _, _, err := executable(legs, crossed, nil, now, time.Minute, false)
	if err == nil {
		t.Fatal("a crossed quote filled: profit was printed out of a bad tick")
	}
	if !strings.Contains(err.Error(), "crossed") {
		t.Errorf("the refusal does not say the quote is crossed: %v", err)
	}

	// The same book the right way round fills as it always did.
	straight := map[string]Quote{
		"SPY260904P00754000": {Symbol: "SPY260904P00754000", Bid: 0.80, Ask: 1.20, BidSize: 10, AskSize: 10, At: now},
	}
	if _, sets, _, err := executable(legs, straight, nil, now, time.Minute, false); err != nil || sets != 10 {
		t.Errorf("an honest quote was refused: sets=%d err=%v", sets, err)
	}

	// A one-sided quote cannot be crossed, and is still refused for its own
	// reason - no price on the side we fill against - not for this one.
	oneSided := map[string]Quote{
		"SPY260904P00754000": {Symbol: "SPY260904P00754000", Ask: 1.20, AskSize: 10, At: now},
	}
	_, _, _, err = executable(legs, oneSided, nil, now, time.Minute, false)
	if err == nil || strings.Contains(err.Error(), "crossed") {
		t.Errorf("a one-sided quote was called crossed: %v", err)
	}
}

// Every answer the arena serves itself carries STRUCTURED content as well as
// text, because the two readers are different: a model reads the text, and code
// reads `StructuredContent`.
//
// The harness's own broker client decodes `result.StructuredContent` and nothing
// else. While the arena answered with text alone, `Positions` returned an empty
// slice and a nil error - an empty portfolio rather than a failure - and every
// guard that reads the broker was blind here for a week. Measured 31 August: the
// profit watch held a spread whose buy-back was 0.28 against a line of 0.35 and
// said nothing, because it had been handed no positions at all.
func TestAnAnswerCarriesStructuredContentAndNotOnlyText(t *testing.T) {
	res, err := answer("get_all_positions", map[string]any{"result": []any{
		map[string]any{"symbol": "QQQ260904P00710000", "qty": "-1", "avg_entry_price": "1.9500"},
	}})
	if err != nil {
		t.Fatalf("the answer was not built: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatal("the answer carries no text: a model reads that half")
	}
	if res.StructuredContent == nil {
		t.Fatal("the answer carries no structured content: code reads that half, and gets an empty struct with no error")
	}

	// And it is the SAME body, not a summary of it.
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("the structured content did not marshal: %v", err)
	}
	var fromText, fromStruct any
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &fromText); err != nil {
		t.Fatalf("the text half is not json: %v", err)
	}
	if err := json.Unmarshal(raw, &fromStruct); err != nil {
		t.Fatalf("the structured half is not json: %v", err)
	}
	if !reflect.DeepEqual(fromText, fromStruct) {
		t.Error("the two halves of the answer say different things")
	}
}
