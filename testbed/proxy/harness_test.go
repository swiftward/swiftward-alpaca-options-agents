// Trials through the real handlers and a fake Alpaca MCP.
//
// These are the same checks the review found, except that they now describe the
// mended behaviour. The fake server answers with exactly what it is told to and
// remembers what it was asked: part of the defects were in what the proxy did
// NOT ask for - the clock, for one.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeUpstream is a fake Alpaca MCP: for each tool it returns exactly the text it
// was told to.
type fakeUpstream struct {
	mu     sync.Mutex
	bodies map[string]string
	errors map[string]bool
	calls  []string
}

func (f *fakeUpstream) set(tool, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	f.bodies[tool] = body
}

func (f *fakeUpstream) fail(tool string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.errors == nil {
		f.errors = map[string]bool{}
	}
	f.errors[tool] = true
}

func (f *fakeUpstream) asked(tool string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, c := range f.calls {
		if strings.HasPrefix(c, tool+" ") {
			return true
		}
	}

	return false
}

// fakeNow is the fake exchange's time. One value for every test, so that a quote
// handed out by the fake server is fresh by its own clock.
var fakeNow = time.Date(2026, 8, 28, 14, 30, 0, 0, exchangeZone)

// afterClose is a time after the close of the same day: expiry has arrived.
var afterClose = time.Date(2026, 8, 28, 16, 5, 0, 0, exchangeZone)

func clockBody(open bool) string {
	return clockBodyAt(open, fakeNow)
}

func clockBodyAt(open bool, at time.Time) string {
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{
		"is_open":    open,
		"timestamp":  at.Format(time.RFC3339Nano),
		"next_open":  fakeNow.Add(19 * time.Hour).Format(time.RFC3339Nano),
		"next_close": time.Date(2026, 8, 28, 16, 0, 0, 0, exchangeZone).Format(time.RFC3339Nano),
	}})

	return string(raw)
}

func startFake(t *testing.T, f *fakeUpstream) *arena {
	t.Helper()

	if f.bodies == nil {
		f.bodies = map[string]string{}
	}
	if _, ok := f.bodies["get_clock"]; !ok {
		f.set("get_clock", clockBody(true))
	}

	s := mcp.NewServer(&mcp.Implementation{Name: "fake", Version: "v0"}, nil)
	for _, name := range []string{"get_option_snapshot", "get_clock", "get_stock_latest_trade", "get_stock_latest_quote"} {
		n := name
		s.AddTool(&mcp.Tool{Name: n, Description: n, InputSchema: &jsonschema.Schema{Type: "object"}},
			func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				f.mu.Lock()
				f.calls = append(f.calls, n+" "+string(req.Params.Arguments))
				body, bad := f.bodies[n], f.errors[n]
				f.mu.Unlock()

				if body == "" {
					body = `{"data":{}}`
				}

				return &mcp.CallToolResult{
					IsError: bad,
					Content: []mcp.Content{&mcp.TextContent{Text: body}},
				}, nil
			})
	}

	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	// A cache with a zero lifetime: the test changes the fake server's answer
	// between calls, and a cache would hide the new price behind the old one.
	up, err := dial(context.Background(), srv.URL, 4, time.Nanosecond)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	up.cache.clockCap = time.Nanosecond
	t.Cleanup(func() { _ = up.Close() })

	return &arena{
		up:          up,
		start:       100000,
		maxQuoteAge: 2 * time.Minute,
		roster:      map[string]string{},
		books:       map[string]*Book{},
		served:      map[string]*mcp.Server{},
		said:        map[string]bool{},
	}
}

// book creates a participant's book and enters it into the arena so the matcher
// can see it.
func (a *arena) book(hash string) *Book {
	b := NewBook(hash, a.start, nil)
	a.mu.Lock()
	a.books[hash] = b
	a.mu.Unlock()

	return b
}

// tick is one pass of the matcher.
func (a *arena) tick(t *testing.T) {
	t.Helper()

	(&matcher{a: a, every: time.Second}).once(context.Background())
}

func call(t *testing.T, h mcp.ToolHandler, args string) (text string, isErr bool) {
	t.Helper()

	res, err := h(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "x", Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = tc.Text
		}
	}

	return text, res.IsError
}

// data takes the useful part out of an answer and checks the wrapper on the way:
// the real server wraps EVERY answer in _alpaca_mcp_security, and intercepted
// answers are obliged to look the same, or an agent sees two different kinds of
// answer from one server.
func data(t *testing.T, text string) map[string]any {
	t.Helper()

	var out struct {
		Security map[string]any `json:"_alpaca_mcp_security"`
		Data     map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("the answer cannot be parsed: %v (%s)", err, text)
	}
	if out.Security == nil {
		t.Fatalf("the answer carries no _alpaca_mcp_security block: %s", text)
	}
	if out.Data == nil {
		t.Fatalf("the answer carries no data: %s", text)
	}

	return out.Data
}

func quotes(rows map[string][4]float64, at time.Time) string {
	snapshots := map[string]any{}
	for symbol, r := range rows {
		snapshots[symbol] = map[string]any{"latestQuote": map[string]any{
			"bp": r[0], "ap": r[1], "bs": int(r[2]), "as": int(r[3]),
			"t": at.Format(time.RFC3339Nano),
		}}
	}
	raw, _ := json.Marshal(map[string]any{"data": map[string]any{"snapshots": snapshots}})

	return string(raw)
}

func quote(sym string, bid, ask float64, bs, as int) string {
	return quotes(map[string][4]float64{sym: {bid, ask, float64(bs), float64(as)}}, fakeNow)
}

func place(t *testing.T, a *arena, b *Book, args string) map[string]any {
	t.Helper()

	text, isErr := call(t, a.placeOrder(b), args)
	if isErr {
		t.Fatalf("the order was rejected: %s", text)
	}

	return data(t, text)
}

// ------------------------------------------------------------------
// 1. A zero bid: the position can be closed and the account is visible.
// ------------------------------------------------------------------

// Demanding a two-sided quote on every leg locks a participant into a short
// position at exactly the moment buying it back is worth doing: a buy needs the
// ask alone.
func TestZeroBidDoesNotBlockClosing(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 0, 0.05, 0, 250))
	a := startFake(t, f)
	b := a.book("tok")
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.20, Class: classOption}

	place(t, a, b, `{"qty":"1","limit_price":"0.10","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1","position_intent":"buy_to_close"}]}`)
	a.tick(t)

	if _, held := b.Positions[sym]; held {
		t.Fatalf("the position was not bought back: %+v", b.Positions[sym])
	}
}

// One position with a zero bid used to put out get_account_info entirely, and a
// zero bid on a far out-of-the-money contract is the market being normal, not
// broken.
func TestZeroBidDoesNotBlockAccountInfo(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 0, 0.05, 0, 250))
	a := startFake(t, f)
	b := a.book("tok")
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.20, Class: classOption}

	text, isErr := call(t, a.account(b), `{}`)
	if isErr {
		t.Fatalf("the account is unavailable because of one position with a zero bid: %s", text)
	}
	// A one-sided quote is valued at the side that exists: 0.05/2.
	if got := data(t, text)["equity"]; got != "99997.50" {
		t.Fatalf("equity %v, expected 99997.50", got)
	}
}

// ------------------------------------------------------------------
// 2. A failed read upstream is visible to the agent rather than swallowed.
// ------------------------------------------------------------------

func TestPositionsSurfacesUpstreamError(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", `{"message":"rate limited"}`)
	f.fail("get_option_snapshot")
	a := startFake(t, f)
	b := a.book("tok")
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.20, Class: classOption}

	text, isErr := call(t, a.positions(b), `{}`)
	if !isErr {
		t.Fatalf("the failed quote read is not visible to the agent: %s", text)
	}
}

// ------------------------------------------------------------------
// 3. An answer of the wrong shape is an error, not "the market is quiet".
// ------------------------------------------------------------------

func TestWrongShapeIsRefused(t *testing.T) {
	sym := "SPY260828P00600000"
	// the field is named "snapshot" rather than "snapshots": the shape upstream changed
	f := &fakeUpstream{}
	f.set("get_option_snapshot", `{"data":{"snapshot":{"`+sym+`":{"latestQuote":{"bp":1.0,"ap":1.1,"bs":10,"as":10}}}}}`)
	a := startFake(t, f)
	b := a.book("tok")
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.20, Class: classOption}

	if text, isErr := call(t, a.positions(b), `{}`); !isErr {
		t.Errorf("a changed shape was parsed as an empty answer: %s", text)
	}
	if text, isErr := call(t, a.account(b), `{}`); !isErr {
		t.Errorf("a changed shape was parsed as an empty answer: %s", text)
	}
}

// ------------------------------------------------------------------
// 4. An order STANDS: pending_new in the answer, the fill on the matcher's tick.
// ------------------------------------------------------------------

func TestOrderRestsAndMatcherFillsIt(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	out := place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	if out["status"] != "pending_new" {
		t.Fatalf("status in the answer %v, expected pending_new", out["status"])
	}
	if b.Cash != 100000 {
		t.Fatalf("the cash was touched inside the call: %.4f", b.Cash)
	}
	if len(b.OpenOrders()) != 1 {
		t.Fatalf("the order did not land in the book")
	}

	a.tick(t)

	if b.Positions[sym] == nil || b.Positions[sym].Qty != 1 {
		t.Fatalf("the matcher did not fill the order: %+v", b.Positions[sym])
	}
	if want := 100000 - 110 - perContract; b.Cash != want {
		t.Fatalf("cash %.4f, expected %.4f", b.Cash, want)
	}
	if len(b.OpenOrders()) != 0 {
		t.Fatalf("a filled order was left standing")
	}
}

// An order the market has not reached KEEPS standing and names the reason.
// Before, it arrived with the status new and never filled - an agent would have
// waited for it forever.
func TestUnreachableOrderKeepsStandingWithReason(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"0.50","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	open := b.OpenOrders()
	if len(open) != 1 || open[0].Status != statusNew {
		t.Fatalf("the order is not standing: %+v", open)
	}
	if open[0].Why == "" {
		t.Fatalf("the order stands in silence: the agent will not learn why")
	}

	// The market reached the price - and that same order filled.
	f.set("get_option_snapshot", quote(sym, 0.40, 0.45, 500, 500))
	a.tick(t)
	if b.Positions[sym] == nil {
		t.Fatalf("the order did not fill after the market reached it")
	}
}

// ------------------------------------------------------------------
// 5. No order goes upstream at all.
// ------------------------------------------------------------------

func TestNoOrderReachesUpstream(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	if f.asked("place_option_order") {
		t.Fatalf("an order went upstream: %v", f.calls)
	}
}

// ------------------------------------------------------------------
// 6. A partial fill, bounded by the smallest side shown.
// ------------------------------------------------------------------

func TestPartialFillLimitedByShownSize(t *testing.T) {
	long, short := "SPY260828P00595000", "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quotes(map[string][4]float64{
		short: {1.00, 1.10, 3, 500},
		long:  {0.50, 0.60, 500, 500},
	}, fakeNow))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"10","limit_price":"-0.30","legs":[
		{"symbol":"`+short+`","side":"sell","ratio_qty":"1"},
		{"symbol":"`+long+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	// credit 1.00-0.60 = 0.40 per set, the short leg's bid holds 3 sets
	want := 100000 + 0.40*3*100 - 6*perContract
	if b.Cash != want {
		t.Fatalf("cash %.4f, expected %.4f", b.Cash, want)
	}
	open := b.OpenOrders()
	if len(open) != 1 || open[0].FilledQty != 3 || open[0].Status != statusPartial {
		t.Fatalf("what is left of the order: %+v", open)
	}

	// On the next tick the book got deeper - we take the rest.
	f.set("get_option_snapshot", quotes(map[string][4]float64{
		short: {1.00, 1.10, 500, 500},
		long:  {0.50, 0.60, 500, 500},
	}, fakeNow))
	a.tick(t)
	if len(b.OpenOrders()) != 0 {
		t.Fatalf("the order did not top up: %+v", b.OpenOrders())
	}
	if b.Positions[short].Qty != -10 {
		t.Fatalf("position %d, expected -10", b.Positions[short].Qty)
	}
}

// ------------------------------------------------------------------
// 7. An expired position is settled rather than putting the account out for good.
// ------------------------------------------------------------------

func TestExpiredPositionIsSettledNotBricking(t *testing.T) {
	dead := "SPY260828P00600000" // expired, the broker no longer quotes it
	f := &fakeUpstream{}
	f.set("get_option_snapshot", `{"data":{"snapshots":{}}}`)
	f.set("get_stock_latest_trade", `{"data":{"trades":{"SPY":{"p":640.00,"t":"2026-08-28T20:00:00Z"}}}}`)
	// The clock after the close: expiry settlement arrives at 16:00 exchange time.
	f.set("get_clock", clockBodyAt(false, afterClose))
	a := startFake(t, f)
	b := a.book("tok")
	b.Cash = 100120 // the credit for the sold spread has already been received
	b.Positions[dead] = &Position{Symbol: dead, Qty: -1, AvgPrice: 1.20, Class: classOption}

	// The account is visible before settlement too: a position that could not be
	// valued is not a refusal but an unknown said out loud.
	text, isErr := call(t, a.account(b), `{}`)
	if isErr {
		t.Fatalf("an expired position put the account out: %s", text)
	}
	arena, _ := data(t, text)["arena"].(map[string]any)
	if unpriced, _ := arena["unpriced"].([]any); len(unpriced) != 1 {
		t.Fatalf("the unvalued position was not named: %v", arena)
	}

	// And the matcher's tick settles it: the put is out of the money with SPY at 640.
	a.tick(t)
	if len(b.Positions) != 0 {
		t.Fatalf("the position was not settled: %+v", b.Positions)
	}
	if b.Cash != 100120 {
		t.Fatalf("cash %.2f, expected 100120: an empty expiry does not move money", b.Cash)
	}
}

// ------------------------------------------------------------------
// 8. The market is closed: the order waits for the open.
// ------------------------------------------------------------------

// A fill against a frozen evening quote is a trade without risk: the price is
// guaranteed not to move until morning.
func TestNoFillsWhileMarketClosed(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	f.set("get_clock", clockBody(false))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	if !f.asked("get_clock") {
		t.Fatalf("the clock was not asked for at all: %v", f.calls)
	}

	a.tick(t)
	if b.Positions[sym] != nil {
		t.Fatalf("the order filled against a closed market")
	}
	if open := b.OpenOrders(); len(open) != 1 || !strings.Contains(open[0].Why, "closed") {
		t.Fatalf("the reason for waiting was not named: %+v", open)
	}
}

// A stale quote with the market open is the same thing: a price that is no
// longer there.
func TestNoFillsOnStaleQuote(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quotes(map[string][4]float64{sym: {1.00, 1.10, 500, 500}},
		fakeNow.Add(-3*time.Hour)))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	if b.Positions[sym] != nil {
		t.Fatalf("the order filled against a three-hour-old quote")
	}
	if open := b.OpenOrders(); len(open) != 1 || !strings.Contains(open[0].Why, "older than") {
		t.Fatalf("the reason was not named: %+v", open)
	}
}

// ------------------------------------------------------------------
// 9. The record: an order the market did not reach leaves a trace.
// ------------------------------------------------------------------

func TestStandingOrderIsVisibleThroughGetOrders(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 0, 0))
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	text, isErr := call(t, a.getOrders(b), `{"status":"open","nested":true}`)
	if isErr {
		t.Fatalf("get_orders refused: %s", text)
	}
	rows, _ := data(t, text)["result"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the book of orders holds %d rows", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["status"] != statusNew || row["filled_qty"] != "0" {
		t.Fatalf("the order row: %+v", row)
	}
	if arena, _ := row["arena"].(map[string]any); arena["why"] == "" {
		t.Fatalf("the reason was not shown outside: %+v", row)
	}
}

// get_orders HONOURS its limit and answers newest first, the way the broker does.
//
// This is a property an instrument is tempted to skip - a stand that always
// returns everything is friendlier and never surprises anybody. It is also the
// exact shape of a defect the team hit on 1 September: a working order fell out
// of the newest-N window, the ladder lost its memory of it, rebuilt its age from
// the replacement's fresh stamp, and so restarted patience forever. An order that
// is never cancelled kept its underlying withheld from the candidate list, and
// twenty-six entry windows across two accounts ended with nothing taken.
//
// Their own fake broker did not honour the limit, so no test of theirs could show
// it. This one exists so the same blindness cannot be ours: a bench more generous
// than the broker hides precisely the bugs that only appear when the broker is
// stingy.
func TestGetOrdersHonoursItsLimitAndAnswersNewestFirst(t *testing.T) {
	f := &fakeUpstream{}
	a := startFake(t, f)
	b := a.book("tok")

	// Five orders, placed in a known order and never filled.
	syms := []string{
		"SPY260828P00600000", "SPY260828P00601000", "SPY260828P00602000",
		"SPY260828P00603000", "SPY260828P00604000",
	}
	for _, sym := range syms {
		f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 0, 0))
		place(t, a, b, `{"qty":"1","limit_price":"1.20","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	}
	a.tick(t)

	text, isErr := call(t, a.getOrders(b), `{"status":"open","limit":2,"nested":true}`)
	if isErr {
		t.Fatalf("get_orders refused: %s", text)
	}
	rows, _ := data(t, text)["result"].([]any)
	if len(rows) != 2 {
		t.Fatalf("a limit of 2 was answered with %d rows: the bench is more generous than the broker", len(rows))
	}

	// And the two it kept are the NEWEST two, because that is which two the
	// broker keeps - the oldest working order is the one that disappears.
	// A one-leg order is reported the way the broker reports it, with the symbol
	// on the order itself and no legs array.
	newest, _ := rows[0].(map[string]any)
	if newest["symbol"] != syms[len(syms)-1] {
		t.Errorf("the first row is %v, and the newest order is %s: an order read oldest-first "+
			"never falls out of the window, and the whole class of defect goes unseen",
			newest["symbol"], syms[len(syms)-1])
	}
	oldestKept, _ := rows[1].(map[string]any)
	if oldestKept["symbol"] != syms[len(syms)-2] {
		t.Errorf("the second row is %v rather than %s", oldestKept["symbol"], syms[len(syms)-2])
	}
}

func TestGetOrdersCancelReplaceRoundTrip(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	out := place(t, a, b, `{"qty":"1","limit_price":"0.50","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	id, _ := out["id"].(string)

	moved, isErr := call(t, a.replaceOrder(b), `{"order_id":"`+id+`","limit_price":"0.60"}`)
	if isErr {
		t.Fatalf("the move refused: %s", moved)
	}
	next := data(t, moved)
	if next["id"] == id {
		t.Fatalf("the move returned the same identifier")
	}
	if next["limit_price"] != "0.60" {
		t.Fatalf("the new price %v", next["limit_price"])
	}

	text, isErr := call(t, a.cancelOrder(b), `{"order_id":"`+next["id"].(string)+`"}`)
	if isErr {
		t.Fatalf("the cancel refused: %s", text)
	}
	if data(t, text)["status"] != statusCanceled {
		t.Fatalf("the status after the cancel: %v", data(t, text)["status"])
	}

	a.tick(t)
	if b.Positions[sym] != nil {
		t.Fatalf("a cancelled order filled")
	}
}

// ------------------------------------------------------------------
// 10. An unknown token gets a REFUSAL rather than a fresh book.
// ------------------------------------------------------------------

// A typo in a token used to create a new book with a hundred thousand in it -
// that is, it worked as a button that reset the account, while an agent read the
// empty answer as "there are no positions".
func TestUnknownTokenIsRefused(t *testing.T) {
	a := startFake(t, &fakeUpstream{})
	a.tools = map[string]*mcp.Tool{}
	for _, n := range append(append([]string{}, passthrough...), intercepted...) {
		a.tools[n] = &mcp.Tool{Name: n, Description: n, InputSchema: &jsonschema.Schema{Type: "object"}}
	}
	a.roster[hashOf("real")] = "probe"

	known := httptest.NewRequest("POST", "/", nil)
	known.Header.Set("Authorization", "Bearer real")
	typo := httptest.NewRequest("POST", "/", nil)
	typo.Header.Set("Authorization", "Bearer typo")

	if a.serverFor(known) == nil {
		t.Fatalf("a participant on the roster was not served")
	}
	if a.serverFor(typo) != nil {
		t.Fatalf("an unknown token was served")
	}
	if len(a.books) != 1 {
		t.Fatalf("%d books were created, expected one", len(a.books))
	}
}

func TestEmptyRosterServesNobody(t *testing.T) {
	a := startFake(t, &fakeUpstream{})
	r := httptest.NewRequest("POST", "/", nil)
	r.Header.Set("Authorization", "Bearer anybody")

	if a.serverFor(r) != nil {
		t.Fatalf("an empty roster served somebody")
	}
}

// ------------------------------------------------------------------
// 11. The single-leg and market orders are the main shapes in the broker's schema.
// ------------------------------------------------------------------

func TestSingleLegOrderAccepted(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	// exactly the shape the real place_option_order's schema describes:
	// symbol/side at the top, no legs
	out := place(t, a, b,
		`{"qty":"1","type":"limit","limit_price":"1.20","symbol":"`+sym+`","side":"buy","position_intent":"buy_to_open"}`)
	if out["order_class"] != "simple" || out["symbol"] != sym {
		t.Fatalf("the single-leg order row: %+v", out)
	}

	a.tick(t)
	if b.Positions[sym] == nil {
		t.Fatalf("the single-leg order did not fill")
	}
}

func TestMarketOrderAccepted(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	out := place(t, a, b, `{"qty":"1","legs":[{"symbol":"`+sym+`","side":"buy","ratio_qty":"1"}]}`)
	if out["order_type"] != "market" {
		t.Fatalf("the order type %v, expected market", out["order_type"])
	}

	// A market order takes the price standing on its side: ask 1.10.
	a.tick(t)
	if want := 100000 - 110 - perContract; b.Cash != want {
		t.Fatalf("cash %.4f, expected %.4f", b.Cash, want)
	}
}

// ------------------------------------------------------------------
// 12. client_order_id is the idempotency key.
// ------------------------------------------------------------------

func TestClientOrderIDIsIdempotent(t *testing.T) {
	sym := "SPY260828P00600000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 500, 500))
	a := startFake(t, f)
	b := a.book("tok")

	args := `{"qty":"1","limit_price":"1.20","client_order_id":"same-key-1","legs":[{"symbol":"` + sym + `","side":"buy","ratio_qty":"1"}]}`
	place(t, a, b, args)

	// A retry after a "timeout": the broker rejects these, and so must we -
	// otherwise the agent gets a second position instead of the same one.
	if text, isErr := call(t, a.placeOrder(b), args); !isErr {
		t.Fatalf("the duplicate was accepted: %s", text)
	}

	a.tick(t)
	if b.Positions[sym].Qty != 1 {
		t.Fatalf("position %d, expected 1", b.Positions[sym].Qty)
	}
}

// ------------------------------------------------------------------
// 13. Buying power and the liquidation value.
// ------------------------------------------------------------------

func TestBuyingPowerAndLiquidation(t *testing.T) {
	short, long := "SPY260828P00640000", "SPY260828P00635000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quotes(map[string][4]float64{
		short: {2.00, 2.20, 100, 100},
		long:  {1.00, 1.20, 100, 100},
	}, fakeNow))
	a := startFake(t, f)
	b := a.book("tok")
	b.Positions[short] = &Position{Symbol: short, Qty: -1, AvgPrice: 2.00, Class: classOption}
	b.Positions[long] = &Position{Symbol: long, Qty: 1, AvgPrice: 1.20, Class: classOption}

	text, isErr := call(t, a.account(b), `{}`)
	if isErr {
		t.Fatalf("%s", text)
	}
	got := data(t, text)

	// Equity at the middle of the market: the short leg -2.10, the long +1.10.
	if got["equity"] != "99900.00" {
		t.Fatalf("equity %v, expected 99900.00", got["equity"])
	}
	// The collateral is the width of the spread: 5.00 * 100.
	if got["options_buying_power"] != "99500.00" {
		t.Fatalf("buying power %v, expected 99500.00", got["options_buying_power"])
	}
	if got["initial_margin"] != "500.00" {
		t.Fatalf("collateral %v", got["initial_margin"])
	}
	// Liquidation is worse than the mark by the spread of both legs: -2.20 and +1.00.
	arena, _ := got["arena"].(map[string]any)
	if arena["liquidation_value"] != "99880.00" {
		t.Fatalf("liquidation value %v, expected 99880.00", arena["liquidation_value"])
	}
}

func TestUnboundedRiskLeavesNoBuyingPower(t *testing.T) {
	sym := "SPY260828C00650000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote(sym, 1.00, 1.10, 100, 100))
	a := startFake(t, f)
	b := a.book("tok")
	// The arena will not let such a book come about itself; we check that if one
	// did, the account does not lie about free money.
	b.Positions[sym] = &Position{Symbol: sym, Qty: -1, AvgPrice: 1.00, Class: classOption}

	text, isErr := call(t, a.account(b), `{}`)
	if isErr {
		t.Fatalf("%s", text)
	}
	got := data(t, text)
	if got["options_buying_power"] != "0.00" {
		t.Fatalf("buying power %v against an unbounded risk", got["options_buying_power"])
	}
}

// ------------------------------------------------------------------
// 14. The cache and the queue.
// ------------------------------------------------------------------

func TestCacheCollapsesRepeatedReads(t *testing.T) {
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quote("SPY260828P00600000", 1, 1.1, 10, 10))
	a := startFake(t, f)
	// A live cache rather than a zero one: it is the thing under test.
	a.up.cache = newCache(time.Minute)

	for range 5 {
		if _, err := a.optionQuotes(context.Background(), prioBrowse, []string{"SPY260828P00600000"}); err != nil {
			t.Fatal(err)
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, "get_option_snapshot ") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d identical snapshots went upstream, expected one", n)
	}
}

func TestQueueLetsTradesPassBrowsing(t *testing.T) {
	g := newGate(1)
	if err := g.enter(context.Background(), prioBrowse); err != nil {
		t.Fatal(err)
	}

	order := make(chan string, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		g.enter(context.Background(), prioBrowse) //nolint:errcheck // the context is alive
		order <- "browse"
		g.leave()
	}()
	// We let browsing queue up first.
	time.Sleep(20 * time.Millisecond)
	go func() {
		defer wg.Done()
		g.enter(context.Background(), prioTrade) //nolint:errcheck // the context is alive
		order <- "trade"
		g.leave()
	}()
	time.Sleep(20 * time.Millisecond)

	g.leave()
	wg.Wait()
	close(order)

	if first := <-order; first != "trade" {
		t.Fatalf("%q went first: an order is required to overtake browsing", first)
	}
}

// ------------------------------------------------------------------
// 15. The book outlives a restart.
// ------------------------------------------------------------------

func TestBookSurvivesRestart(t *testing.T) {
	file := t.TempDir() + "/arena.db"
	store, err := OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}

	hash := hashOf("participant")
	b := NewBook(hash, 100000, store)
	legs := legsOfRatio(t, Leg{Symbol: "SPY260828P00640000", Side: "buy", RatioQty: "1"})
	o := &Order{ClientID: "name", Qty: 2, Limit: 1.50, TIF: "day", Legs: legs, SubmittedAt: fakeNow}
	if err := b.Submit(o); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Fill(o.ID, 1, 1.40, map[string]float64{"SPY260828P00640000": 1.40}, fakeNow); err != nil {
		t.Fatal(err)
	}
	store.Close()

	again, err := OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()

	back := NewBook(hash, 100000, again)
	found, err := again.Load(back)
	if err != nil || !found {
		t.Fatalf("the book was not raised: found=%v err=%v", found, err)
	}
	if back.Cash != b.Cash {
		t.Fatalf("cash %.4f, was %.4f", back.Cash, b.Cash)
	}
	p := back.Positions["SPY260828P00640000"]
	if p == nil || p.Qty != 1 || p.AvgPrice != 1.40 {
		t.Fatalf("the position after the restart: %+v", p)
	}
	got, ok := back.ByID(o.ID)
	if !ok || got.Status != statusPartial || got.FilledQty != 1 || got.ClientID != "name" {
		t.Fatalf("the order after the restart: %+v", got)
	}
	// Idempotency outlives the restart along with the book.
	if _, ok := back.ByClientID("name"); !ok {
		t.Fatalf("the order's name was lost in the restart")
	}
}

// ------------------------------------------------------------------
// 16. The full round: the order stands, fills, the position is visible, expiry pays.
// ------------------------------------------------------------------

func TestFullRoundTrip(t *testing.T) {
	short, long := "SPY260828P00640000", "SPY260828P00635000"
	f := &fakeUpstream{}
	f.set("get_option_snapshot", quotes(map[string][4]float64{
		short: {2.00, 2.10, 50, 50},
		long:  {1.10, 1.20, 50, 50},
	}, fakeNow))
	f.set("get_stock_latest_trade", `{"data":{"trades":{"SPY":{"p":641.00,"t":"2026-08-28T20:00:00Z"}}}}`)
	a := startFake(t, f)
	b := a.book("tok")

	place(t, a, b, `{"qty":"1","limit_price":"-0.70","legs":[
		{"symbol":"`+short+`","side":"sell","ratio_qty":"1"},
		{"symbol":"`+long+`","side":"buy","ratio_qty":"1"}]}`)
	a.tick(t)

	// A credit of 2.00 - 1.20 = 0.80 per set.
	if want := 100000 + 80 - 2*perContract; b.Cash != want {
		t.Fatalf("cash %.4f, expected %.4f", b.Cash, want)
	}

	text, isErr := call(t, a.positions(b), `{}`)
	if isErr {
		t.Fatalf("%s", text)
	}
	rows, _ := data(t, text)["result"].([]any)
	if len(rows) != 2 {
		t.Fatalf("%d positions", len(rows))
	}

	// Expiry arrives: SPY at 641, both legs out of the money - the spread kept its credit.
	f.set("get_clock", clockBodyAt(false, afterClose))
	a.mu.Lock()
	a.said = map[string]bool{}
	a.mu.Unlock()
	m := &matcher{a: a, every: time.Second}
	m.once(context.Background())

	if len(b.Positions) != 0 {
		t.Fatalf("positions were left after expiry: %+v", b.Positions)
	}
	if want := 100000 + 80 - 2*perContract; b.Cash != want {
		t.Fatalf("cash after expiry %.4f, expected %.4f", b.Cash, want)
	}
}
