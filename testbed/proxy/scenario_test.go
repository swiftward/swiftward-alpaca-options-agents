package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func writeScenario(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scene.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("the scenario was not written: %v", err)
	}

	return path
}

const walkToTheStrike = `{
  "name": "the price reaches the sold strike",
  "underlying": "SPY",
  "start": "2026-09-04T13:35:00Z",
  "open": true,
  "speed": 60,
  "steps": [
    {"after": "0s", "underlying_price": 645.00,
     "quotes": {"SPY260904P00640000": {"bid": 0.80, "ask": 0.90, "bid_size": 40, "ask_size": 40, "delta": -0.15, "iv": 0.128}}},
    {"after": "30m", "underlying_price": 641.00,
     "quotes": {"SPY260904P00640000": {"bid": 2.10, "ask": 2.30, "bid_size": 12, "ask_size": 12, "delta": -0.42, "iv": 0.151}}},
    {"after": "60m", "underlying_price": 638.50}
  ]
}`

// A scenario stages what the market will not stage on request. The reading that
// matters is that it MOVES: the same contract answers differently as the run
// goes on, and a step names only what changed.
func TestAScenarioStagesAMovingMarket(t *testing.T) {
	s, err := LoadScenario(writeScenario(t, walkToTheStrike))
	if err != nil {
		t.Fatalf("the scenario did not load: %v", err)
	}

	// Speed 60: one real second carries a scenario minute.
	start := s.at(0)
	if start.Price != 645 {
		t.Errorf("at the start the underlying is %v, expected 645", start.Price)
	}
	if q := start.Quotes["SPY260904P00640000"]; q.Bid != 0.80 || q.BidSize != 40 {
		t.Errorf("the staged book at the start: %+v", q)
	}
	// The staged quote carries the STAGED time, or the freshness check would
	// refuse every scenario not dated today.
	if !start.Quotes["SPY260904P00640000"].At.Equal(s.Start) {
		t.Errorf("the staged quote is stamped %s, expected the staged clock %s",
			start.Quotes["SPY260904P00640000"].At, s.Start)
	}

	// Thirty scenario minutes in - half a real second at this speed.
	mid := s.at(30 * time.Second)
	if mid.Price != 641 {
		t.Errorf("half an hour in the underlying is %v, expected 641", mid.Price)
	}
	if q := mid.Quotes["SPY260904P00640000"]; q.Bid != 2.10 {
		t.Errorf("the book did not move with the price: %+v", q)
	}
	if !mid.Now.Equal(s.Start.Add(30 * time.Minute)) {
		t.Errorf("the staged clock says %s, expected half an hour past the start", mid.Now)
	}

	// The last step names no quotes at all: what was not said carries over. This
	// is the property that lets a scenario state only what changes.
	end := s.at(60 * time.Second)
	if end.Price != 638.50 {
		t.Errorf("at the end the underlying is %v, expected 638.50", end.Price)
	}
	if q := end.Quotes["SPY260904P00640000"]; q.Bid != 2.10 {
		t.Errorf("a contract absent from the last step lost its book: %+v", q)
	}
}

// Every one of these would stage the wrong thing quietly, so each is refused at
// load rather than at the moment the run goes green for the wrong reason.
func TestABrokenScenarioIsRefusedAtLoad(t *testing.T) {
	cases := []struct{ why, body string }{
		{"no name", `{"underlying":"SPY","start":"2026-09-04T13:35:00Z","steps":[{"after":"0s","underlying_price":1}]}`},
		{"no underlying", `{"name":"x","start":"2026-09-04T13:35:00Z","steps":[{"after":"0s","underlying_price":1}]}`},
		{"no start", `{"name":"x","underlying":"SPY","steps":[{"after":"0s","underlying_price":1}]}`},
		{"no steps", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z","steps":[]}`},
		{"steps out of order", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z","steps":[
			{"after":"10m","underlying_price":1},{"after":"5m","underlying_price":2}]}`},
		{"a price of nothing", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z","steps":[{"after":"0s","underlying_price":0}]}`},
		{"a symbol that is not a contract", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z","steps":[
			{"after":"0s","underlying_price":1,"quotes":{"SPY-640-PUT":{"bid":1,"ask":2}}}]}`},
		{"a field nobody defined", `{"name":"x","underlying":"SPY","start":"2026-09-04T13:35:00Z","volatility":0.3,"steps":[
			{"after":"0s","underlying_price":1}]}`},
	}
	for _, c := range cases {
		if _, err := LoadScenario(writeScenario(t, c.body)); err == nil {
			t.Errorf("a scenario with %s loaded: it would have staged the wrong thing quietly", c.why)
		}
	}
}

// The answers handed to an agent must be in the BROKER's shape, taken from a
// live answer rather than invented: an agent reads greeks and impliedVolatility
// beside latestQuote, and a shape of our own breaks the code that reads them.
func TestTheStagedAnswersKeepTheBrokersShape(t *testing.T) {
	s, err := LoadScenario(writeScenario(t, walkToTheStrike))
	if err != nil {
		t.Fatalf("the scenario did not load: %v", err)
	}
	st := &stage{scenario: s, began: time.Now()}

	snap := st.snapshotJSON([]string{"SPY260904P00640000", "SPY260904P00600000"})
	rows, ok := snap["snapshots"].(map[string]any)
	if !ok {
		t.Fatalf("the snapshot answer has no snapshots: %+v", snap)
	}
	// A contract the scenario never staged is absent, not zero: absent reads as
	// "the market is quiet about it", zero reads as a price.
	if _, staged := rows["SPY260904P00600000"]; staged {
		t.Error("a contract the scenario never staged came back with a price")
	}
	row, ok := rows["SPY260904P00640000"].(map[string]any)
	if !ok {
		t.Fatalf("the staged contract is missing: %+v", rows)
	}
	quote, ok := row["latestQuote"].(map[string]any)
	if !ok || quote["bp"] != 0.80 || quote["ap"] != 0.90 {
		t.Errorf("latestQuote is not in the broker's shape: %+v", row["latestQuote"])
	}
	greeks, ok := row["greeks"].(map[string]any)
	if !ok || greeks["delta"] != -0.15 {
		t.Errorf("greeks are not where an agent reads them: %+v", row["greeks"])
	}
	if row["impliedVolatility"] != 0.128 {
		t.Errorf("impliedVolatility is %v, expected 0.128", row["impliedVolatility"])
	}

	trade, ok := st.tradeJSON([]string{"SPY"})["trades"].(map[string]any)
	if !ok {
		t.Fatal("the trade answer has no trades")
	}
	if row, ok := trade["SPY"].(map[string]any); !ok || row["p"] != 645.00 {
		t.Errorf("the underlying's trade is not in the broker's shape: %+v", trade)
	}

	clock := st.clockJSON()
	if clock["is_open"] != true {
		t.Errorf("the staged clock says the session is shut: %+v", clock)
	}
}

// The whole point, end to end: a staged market moves, and the book fills against
// the SAME prices the agent read.
//
// This is the case the real market will not stage on request - the price walking
// down to the sold strike - and the one their gateway has been bitten by. What
// is checked here is not profit. It is that the defence had something to fire
// at: the order rested while the market was away and filled when the market
// arrived, and that the fill is marked as no measurement at all.
func TestAStagedMarketDrivesAFillAndMarksIt(t *testing.T) {
	f := &fakeUpstream{}
	a := startFake(t, f)
	s, err := LoadScenario(writeScenario(t, walkToTheStrike))
	if err != nil {
		t.Fatalf("the scenario did not load: %v", err)
	}
	a.staged = &stage{scenario: s, began: time.Now()}
	// A staged market carries its own clock and its own stamps, so the checks
	// that refuse a shut session and a stale quote have nothing true to say.
	a.ignoreSession = true

	b := a.book("tok")
	sym := "SPY260904P00640000"

	// Buying the put back at 1.00 while the market asks 0.90 would fill at once,
	// so ask for something the opening step does not offer: sell it at 2.00 when
	// the bid is 0.80.
	out := place(t, a, b, `{"legs":[{"symbol":"`+sym+`","side":"sell","ratio_qty":"1"}],
		"qty":"1","limit_price":"-2.00","time_in_force":"day","client_order_id":"turn=st-1;staged"}`)
	if out["status"] != "pending_new" {
		t.Fatalf("the order did not rest: %v", out["status"])
	}

	a.tick(t)
	if len(b.OpenOrders()) != 1 {
		t.Fatalf("the order filled while the market was still away: %+v", b.Orders)
	}

	// Half an hour into the scenario - half a real second at speed 60 - the price
	// has walked to the strike and the bid is 2.10.
	a.staged.began = time.Now().Add(-30 * time.Second)
	a.tick(t)

	if len(b.OpenOrders()) != 0 {
		open := b.OpenOrders()
		t.Fatalf("the order did not fill after the market arrived: %+v", open[0].Why)
	}
	if p := b.Positions[sym]; p == nil || p.Qty != -1 {
		t.Fatalf("the position after the staged fill: %+v", p)
	}

	// And the mark that keeps a staged number out of a measurement, in the BOOK
	// rather than only in the log.
	for _, o := range b.OrdersSnapshot() {
		if o.FilledQty > 0 && !o.Stand {
			t.Error("a fill on a staged market is not marked: in a week it reads as a real one")
		}
	}
}

// An agent under a scenario must read the staged market, not the real one -
// otherwise the bench is one where the agent sees one world and trades another.
func TestTheAgentReadsTheStagedMarketToo(t *testing.T) {
	f := &fakeUpstream{}
	a := startFake(t, f)
	s, err := LoadScenario(writeScenario(t, walkToTheStrike))
	if err != nil {
		t.Fatalf("the scenario did not load: %v", err)
	}
	a.staged = &stage{scenario: s, began: time.Now()}

	res, staged := a.stagedAnswer("get_option_snapshot", map[string]any{"symbols": "SPY260904P00640000"})
	if !staged {
		t.Fatal("the snapshot was not served from the scenario")
	}
	got := data(t, res.Content[0].(*mcp.TextContent).Text)
	rows, ok := got["snapshots"].(map[string]any)
	if !ok || rows["SPY260904P00640000"] == nil {
		t.Fatalf("the staged snapshot did not reach the agent: %+v", got)
	}

	// The chain is NOT staged: a scenario has no business inventing which
	// contracts exist, and that read still goes to the broker.
	if _, staged := a.stagedAnswer("get_option_chain", map[string]any{"underlying_symbol": "SPY"}); staged {
		t.Error("the chain was answered from the scenario: contracts are the broker's to name")
	}
	if _, staged := a.stagedAnswer("get_news", map[string]any{}); staged {
		t.Error("the news was answered from the scenario")
	}
}

// A market read that names no symbol is REFUSED, and the refusal shows the agent
// the word it actually wrote.
//
// This is not a nicety. On 31 August a participant asked every market read with
// `symbol` instead of `symbols` through a whole trial, was answered
// {"trades":{}} and {"snapshots":{}}, and concluded the market had gone dark. It
// then stood over an open spread for eight windows reporting itself blind and
// unprotected while the staged price walked six dollars through its short
// strike - and the broker had been answering all along. An instrument built to
// measure whether an agent notices it is blind must never be the thing that
// blinds it.
func TestAReadThatNamesNoSymbolIsRefused(t *testing.T) {
	f := &fakeUpstream{}
	a := startFake(t, f)
	s, err := LoadScenario(writeScenario(t, walkToTheStrike))
	if err != nil {
		t.Fatalf("the scenario did not load: %v", err)
	}
	a.staged = &stage{scenario: s, began: time.Now()}

	for _, name := range []string{"get_stock_latest_trade", "get_option_snapshot"} {
		res, staged := a.stagedAnswer(name, map[string]any{"symbol": "SPY260904P00640000"})
		if !staged {
			t.Fatalf("%s was not served from the scenario at all", name)
		}
		if !res.IsError {
			t.Fatalf("%s answered a call that named no symbol instead of refusing it", name)
		}
		text := res.Content[0].(*mcp.TextContent).Text
		if !strings.Contains(text, "symbols") || !strings.Contains(text, "it carried: symbol") {
			t.Errorf("the refusal does not show the agent the word it wrote: %s", text)
		}
	}

	// And the good call still answers.
	res, _ := a.stagedAnswer("get_stock_latest_trade", map[string]any{"symbols": "SPY"})
	if res.IsError {
		t.Errorf("a call that named its symbol was refused: %s", res.Content[0].(*mcp.TextContent).Text)
	}
}

// A scenario anchored to now puts the staged clock ON wall time.
//
// The reason is a defect this bench produced itself. Production holds an
// invariant: the broker's timestamps and the harness's own clock are one clock.
// A staged DATE breaks it, and every guard that measures an age against a broker
// timestamp then measures the offset instead. On 31 August the ladder read two
// orders stamped by a staged clock seven and a half hours behind the wall, called
// them seven and a half hours old, and cancelled both on patience a second after
// they were placed. The ladder was right; the stand was lying about the hour.
func TestAScenarioAnchoredToNowUsesTheWallClock(t *testing.T) {
	path := writeScenario(t, `{
	  "name": "a flat book at whatever hour it is", "underlying": "QQQ",
	  "anchor": "now", "open": true,
	  "steps": [{"after": "0s", "underlying_price": 716.91,
	             "quotes": {"QQQ260904P00710000": {"bid": 1.95, "ask": 2.10}}}]
	}`)
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("the anchored scenario was refused: %v", err)
	}
	if !sc.Start.IsZero() {
		t.Fatal("an anchored scenario learned a start at load: only the proxy knows when it came up")
	}

	began := time.Now()
	st := newStage(sc, began)
	if got := st.now().Now; got.Sub(began).Abs() > time.Second {
		t.Errorf("the staged clock reads %s while the wall reads %s: anchoring did not take", got, began)
	}

	// And the two ways of naming the hour cannot both be given.
	both := writeScenario(t, `{
	  "name": "both", "underlying": "QQQ", "anchor": "now",
	  "start": "2026-08-31T13:35:00Z", "open": true,
	  "steps": [{"after": "0s", "underlying_price": 716.91,
	             "quotes": {"QQQ260904P00710000": {"bid": 1.95, "ask": 2.10}}}]
	}`)
	if _, err := LoadScenario(both); err == nil {
		t.Error("a scenario with both an anchor and a start loaded: one of the two is a lie")
	}

	// Anchoring at a speed is refused: a speed moves the two clocks apart again.
	fast := writeScenario(t, `{
	  "name": "fast", "underlying": "QQQ", "anchor": "now", "speed": 60, "open": true,
	  "steps": [{"after": "0s", "underlying_price": 716.91,
	             "quotes": {"QQQ260904P00710000": {"bid": 1.95, "ask": 2.10}}}]
	}`)
	if _, err := LoadScenario(fast); err == nil {
		t.Error("a scenario anchored to now at speed 60 loaded")
	}
}

// Every scenario shipped beside the arena has to load.
//
// A scenario is data, and data breaks the way data breaks: an edit that dates a
// step wrong or misspells a symbol reads perfectly well and stages nothing. That
// is discovered here, at `go test`, rather than by a bench that came up and
// quietly answered a different question than the one it was written for.
func TestEveryShippedScenarioLoads(t *testing.T) {
	files, err := filepath.Glob("../scenarios/*.json")
	if err != nil {
		t.Fatalf("the scenarios were not listed: %v", err)
	}
	// The trial bundles too. They were outside this check until 31 August, which
	// is the half of the shelf that actually gets run: a trial is raised by name
	// and its scenario is read at proxy startup, so a misspelt symbol in one of
	// them surfaces as a bench that came up and staged nothing.
	bundled, err := filepath.Glob("../trials/*/scenario.json")
	if err != nil {
		t.Fatalf("the trial scenarios were not listed: %v", err)
	}
	files = append(files, bundled...)
	if len(files) == 0 {
		t.Fatal("no scenarios were found beside the arena")
	}

	for _, path := range files {
		s, err := LoadScenario(path)
		if err != nil {
			t.Errorf("%s: %v", shortName(path), err)

			continue
		}
		// An overlay stages no contract BY CONSTRUCTION - every contract is the
		// real one, repriced - so the emptiness check below does not apply to it.
		// What is asked of an overlay instead is that its curve goes somewhere: a
		// displacement that never leaves zero is the same kind of unfinished file,
		// and it would run and move nothing.
		if s.Mode == "overlay" {
			moved := false
			for i := range s.Steps {
				if s.Steps[i].shift() != 0 {
					moved = true
				}
			}
			if !moved {
				t.Errorf("%s: the overlay %q never displaces the underlying at all", shortName(path), s.Name)
			}

			continue
		}
		// A scenario that stages nothing at all is a file somebody meant to
		// finish. It loads, it runs, and it answers no question.
		staged := 0
		for i := range s.Steps {
			staged += len(s.Steps[i].Quotes)
		}
		if staged == 0 {
			t.Errorf("%s: the scenario %q stages no contract at all", shortName(path), s.Name)
		}
	}
}

// shortName names a scenario the way a person refers to it: a trial's file is
// always scenario.json, so the folder is the name worth printing.
func shortName(path string) string {
	if filepath.Base(path) == "scenario.json" {
		return filepath.Base(filepath.Dir(path)) + "/scenario.json"
	}

	return filepath.Base(path)
}

// A fault takes a tool away for a stretch of the run and gives it back.
func TestAFaultRefusesOnlyItsOwnToolAndOnlyInItsWindow(t *testing.T) {
	path := writeScenario(t, `{
	  "name": "the reads stop while the market keeps moving",
	  "underlying": "QQQ", "start": "2026-09-04T13:35:00Z", "open": true, "speed": 60,
	  "steps": [{"after": "0s", "underlying_price": 716.90}],
	  "faults": [{
	    "after": "5m", "until": "20m",
	    "tools": ["get_stock_latest_trade", "get_option_snapshot"],
	    "message": "rate limit reached for this session; the read was not served"
	  }]
	}`)
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("the scenario was refused: %v", err)
	}

	// At speed 60 one real second carries a scenario minute, so the window is
	// real seconds five to twenty.
	st := &stage{scenario: sc, began: time.Now().Add(-10 * time.Second)}
	why, out := st.refuses("get_stock_latest_trade")
	if !out {
		t.Fatal("inside its window the named tool must refuse")
	}
	if !strings.Contains(why, "rate limit") {
		t.Fatalf("the agent is told %q, which is not what the scenario says", why)
	}
	if _, out := st.refuses("get_clock"); out {
		t.Fatal("a tool the fault does not name keeps answering")
	}

	st.began = time.Now().Add(-2 * time.Second)
	if _, out := st.refuses("get_stock_latest_trade"); out {
		t.Fatal("before the window the tool answers")
	}

	st.began = time.Now().Add(-25 * time.Second)
	if _, out := st.refuses("get_stock_latest_trade"); out {
		t.Fatal("after the window the tool answers again")
	}
}

// A fault that could never fire is refused at load rather than kept: it would sit
// in the file looking like a test that passed.
func TestAFaultThatCouldNotFireIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"a tool the arena does not serve": `"tools": ["get_stock_bars"], "message": "no"`,
		"a window that runs backwards":    `"until": "1m", "tools": ["get_clock"], "message": "no"`,
		"no message at all":               `"tools": ["get_clock"], "message": "  "`,
		"no tools at all":                 `"tools": [], "message": "no"`,
	} {
		t.Run(name, func(t *testing.T) {
			path := writeScenario(t, `{
			  "name": "bad", "underlying": "QQQ", "start": "2026-09-04T13:35:00Z",
			  "steps": [{"after": "0s", "underlying_price": 1}],
			  "faults": [{"after": "5m", "until": "20m", `+body+`}]
			}`)
			if _, err := LoadScenario(path); err == nil {
				t.Fatal("a fault that can never fire must be refused at load")
			}
		})
	}
}

// A scenario with no faults at all keeps every tool answering.
func TestNoFaultsMeansNothingRefuses(t *testing.T) {
	path := writeScenario(t, `{
	  "name": "quiet", "underlying": "QQQ", "start": "2026-09-04T13:35:00Z",
	  "steps": [{"after": "0s", "underlying_price": 716.90}]
	}`)
	sc, err := LoadScenario(path)
	if err != nil {
		t.Fatalf("the scenario was refused: %v", err)
	}
	st := &stage{scenario: sc, began: time.Now()}
	for _, name := range append(append([]string{}, passthrough...), intercepted...) {
		if _, out := st.refuses(name); out {
			t.Fatalf("%s refuses although the scenario names no fault", name)
		}
	}
	var none *stage
	if _, out := none.refuses("get_clock"); out {
		t.Fatal("with no scenario at all nothing refuses")
	}
}
