// Package marketdata reads the market through the broker's own server, over the
// same protocol the session uses.
//
// Nothing here decides anything. The harness reads prices to know when to wake a
// session; the volatility recorder reads quotes to keep a history the session
// can compare against. What to do about a number is the session's to say.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Broker reads from the broker's server, or from a policy gateway standing in
// front of it. The broker's own server asks for no credential; a gateway does,
// and that is the only difference between the two.
type Broker struct {
	url  string
	name string
	// session is the ONE connection this client keeps, and keeping it is the
	// whole point. Measured against the policy gateway on 27 August: opening a
	// session costs 2.68 seconds - initialize, initialized, tools/list - and a
	// call on an open one costs 0.85. A sweep asks about two hundred and ninety
	// things, so a session per call turned a four-minute pass into seventeen,
	// and the list the entry windows read went stale faster than it was rebuilt.
	//
	// Guarded because the screener, the ladder, the defence and both recorders
	// share one Broker. Only the making and the dropping are under the lock; the
	// calls themselves are not, because the session multiplexes them.
	mu      sync.Mutex
	session *mcp.ClientSession
	// token authenticates this client to whatever answers at url. It is empty
	// where that is the broker's own server, which asks for nothing, and set
	// where a policy gateway stands in front of it and asks who is calling.
	token string
}

// brokerCallLimit is how long one tool call may take, start to finish. Generous
// on purpose: the screener's pass has a budget of five minutes for work that
// takes about two and a half, and bars for years back are a big answer. What it
// stops is not slowness, it is silence that never ends.
var brokerCallLimit = 90 * time.Second

// dialLimit bounds getting the connection up at all - the TCP dial and the TLS
// handshake. Separate from the one above because a machine that cannot reach the
// host should say so in seconds, not in a minute and a half.
var dialLimit = 15 * time.Second

func NewBroker(url string) *Broker {
	return &Broker{url: url, name: "swiftward-alpaca-options-agents-harness"}
}

// NewBrokerWithToken is the same client, presenting a credential on every call.
func NewBrokerWithToken(url, token string) *Broker {
	broker := NewBroker(url)
	broker.token = token

	return broker
}

// roundTripper carries the credential where there is one. Where there is none -
// the broker's own server, which asks for nothing - it is the plain transport.
func (b *Broker) roundTripper() http.RoundTripper {
	under := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialLimit}).DialContext,
		TLSHandshakeTimeout:   dialLimit,
		ResponseHeaderTimeout: brokerCallLimit,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
	}

	if b.token == "" {
		return under
	}

	return bearer{token: b.token, next: under}
}

// bearer adds the credential to every request the transport makes. The MCP
// client opens more than one - the initialize, the calls, and a standing stream
// for anything the server sends back - and a gateway refuses each of them
// separately, so the header cannot be attached to one request by hand.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (b bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	// The request belongs to the caller; a RoundTripper must not modify it.
	carrying := request.Clone(request.Context())
	carrying.Header.Set("Authorization", "Bearer "+b.token)

	return b.next.RoundTrip(carrying)
}

// Contract is one option the broker lists.
type Contract struct {
	Symbol     string
	Expiration time.Time
	Strike     float64
	// Type is the broker's own word: "call" or "put".
	Type string
}

// Quote is what a contract is worth right now, with what the broker computes
// from it. Implied volatility and delta are absent unless the quote is
// two-sided, which is why they are pointers: zero volatility is a number, and
// "the market is closed" is not it.
type Quote struct {
	Symbol string
	Bid    float64
	Ask    float64
	// BidSize and AskSize are what the book is showing at each side. They do not
	// cap a fill: 50 contracts went through against 25 shown on 25 August. A side
	// showing nothing, though, is a price nobody is standing behind.
	BidSize           int
	AskSize           int
	ImpliedVolatility *float64
	Delta             *float64
}

// Account is the money: what the account is worth, what is free, and what it was
// worth at yesterday's close, which is what the day's result is measured from.
type Account struct {
	Number string `json:"number"`
	Status string `json:"status"`
	// OptionsTradingLevel is what the broker allows this account to do with
	// options: 0 none, 1 covered, 2 long, 3 spreads. The agent reads it rather
	// than being told - a structure above the level is refused, and the level is
	// the broker's answer, not ours.
	OptionsTradingLevel int     `json:"options_trading_level"`
	Equity              float64 `json:"equity"`
	EquityYesterday     float64 `json:"equity_yesterday"`
	Cash                float64 `json:"cash"`
	BuyingPower         float64 `json:"buying_power"`
	OptionsBuyingPower  float64 `json:"options_buying_power"`
	PositionMarketValue float64 `json:"position_market_value"`
}

// Position is one holding, as the broker values it right now.
type Position struct {
	Symbol            string  `json:"symbol"`
	AssetClass        string  `json:"asset_class"`
	Side              string  `json:"side"`
	Quantity          float64 `json:"quantity"`
	AverageEntryPrice float64 `json:"average_entry_price"`
	CurrentPrice      float64 `json:"current_price"`
	MarketValue       float64 `json:"market_value"`
	CostBasis         float64 `json:"cost_basis"`
	UnrealizedPL      float64 `json:"unrealized_pl"`
	// UnrealizedPLFraction is the broker's own ratio, not a percentage: 0.0002 is
	// two hundredths of a percent. The page multiplies; nothing here does.
	UnrealizedPLFraction float64 `json:"unrealized_pl_fraction"`
}

// Order is what was sent to the broker and what became of it. A spread is one
// order with legs, which is how it was sent and how it should be read.
type Order struct {
	ID string `json:"id"`
	// ClientID is the name the caller gave the order. The session writes the worst
	// price it accepts into it, so that bound travels with the order itself.
	ClientID       string  `json:"client_id,omitempty"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Type           string  `json:"type"`
	Class          string  `json:"class"`
	Status         string  `json:"status"`
	PositionIntent string  `json:"position_intent"`
	Quantity       float64 `json:"quantity"`
	// Notional is what a crypto order names instead of a quantity: the money to
	// spend rather than the amount to buy.
	Notional       float64    `json:"notional"`
	FilledQuantity float64    `json:"filled_quantity"`
	LimitPrice     float64    `json:"limit_price"`
	FilledPrice    float64    `json:"filled_price"`
	SubmittedAt    *time.Time `json:"submitted_at"`
	FilledAt       *time.Time `json:"filled_at"`
	CanceledAt     *time.Time `json:"canceled_at"`
	Legs           []Order    `json:"legs,omitempty"`
}

// Account reads the money.
func (b *Broker) Account(ctx context.Context) (Account, error) {
	var answer accountAnswer
	if err := b.call(ctx, "get_account_info", map[string]any{}, &answer); err != nil {
		return Account{}, err
	}

	return answer.account()
}

// Positions reads what is held right now.
func (b *Broker) Positions(ctx context.Context) ([]Position, error) {
	var answer positionsAnswer
	if err := b.call(ctx, "get_all_positions", map[string]any{}, &answer); err != nil {
		return nil, err
	}

	return answer.positions()
}

// Orders reads the most recent orders, newest first, whatever became of them:
// a cancelled order is as much a fact as a filled one.
func (b *Broker) Orders(ctx context.Context, limit int) ([]Order, error) {
	var answer ordersAnswer
	err := b.call(ctx, "get_orders", map[string]any{
		"status": "all", "limit": limit, "direction": "desc", "nested": true,
	}, &answer)
	if err != nil {
		return nil, err
	}

	return answer.orders()
}

// ReplaceOrder moves an order's limit price. The broker does this by making a
// new order that replaces the old one, and the new one is named by the broker
// unless the caller names it - so name is passed through, or everything written
// into the old order's name is lost at the first move.
func (b *Broker) ReplaceOrder(ctx context.Context, id string, limit float64, name string) error {
	arguments := map[string]any{
		"order_id":    id,
		"limit_price": fmt.Sprintf("%.2f", limit),
	}
	if name != "" {
		arguments["client_order_id"] = name
	}

	var answer struct{}

	return b.call(ctx, "replace_order_by_id", arguments, &answer)
}

// CancelOrder takes an order out of the book.
func (b *Broker) CancelOrder(ctx context.Context, id string) error {
	var answer struct{}

	return b.call(ctx, "cancel_order_by_id", map[string]any{"order_id": id}, &answer)
}

// LastTrades returns the last traded price of each symbol it could read. A
// symbol the broker did not answer for is absent rather than zero: zero is a
// price, and a wake-up must not fire on a missing reading.
func (b *Broker) LastTrades(ctx context.Context, symbols []string) (map[string]float64, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	var answer tradesAnswer
	if err := b.call(ctx, "get_stock_latest_trade",
		map[string]any{"symbols": strings.Join(symbols, ",")}, &answer); err != nil {
		return nil, err
	}

	return answer.prices(), nil
}

// MarketOpen answers whether the exchange is trading right now. Quotes outside
// those hours are yesterday's, and a history built from them measures the clock
// rather than the market.
func (b *Broker) MarketOpen(ctx context.Context) (bool, error) {
	var answer clockAnswer
	if err := b.call(ctx, "get_clock", map[string]any{}, &answer); err != nil {
		return false, err
	}

	return answer.Data.IsOpen, nil
}

// ContractsAround lists the options on underlying with a strike within span of
// price, expiring on or after from. It is a narrow question on purpose: the
// whole chain is thousands of contracts and pages, and the recorder wants the
// handful at the money.
func (b *Broker) ContractsAround(ctx context.Context, underlying string, price, span float64, from time.Time, limit int) ([]Contract, error) {
	var answer contractsAnswer
	err := b.call(ctx, "get_option_contracts", map[string]any{
		"underlying_symbols":  underlying,
		"expiration_date_gte": from.Format(time.DateOnly),
		"strike_price_gte":    fmt.Sprintf("%.2f", price-span),
		"strike_price_lte":    fmt.Sprintf("%.2f", price+span),
		"limit":               limit,
	}, &answer)
	if err != nil {
		return nil, err
	}

	return answer.contracts()
}

// Chain reads a whole underlying's options in ONE call: the contracts, their
// quotes, their implied volatility and their greeks together.
//
// It replaces a pair of calls - list the contracts, then snapshot them - and the
// pair was the sweep's whole cost. The broker allows 180 requests a minute and
// the sweep spent two of them per underlying, so halving that is the difference
// between pricing 284 names and pricing twice as many, or pricing the same names
// twice as often. Nothing about the arithmetic changes; only how much of the
// market it reaches.
//
// A contract the answer prices without a quote is returned anyway: what is
// missing is the caller's to judge, and a strike dropped here is a strike the
// screener cannot even count as refused.
func (b *Broker) Chain(ctx context.Context, underlying string, low, high float64,
	until time.Time, most int) ([]Contract, map[string]Quote, error) {

	var answer chainAnswer
	if err := b.call(ctx, "get_option_chain", map[string]any{
		"underlying_symbol":   underlying,
		"strike_price_gte":    fmt.Sprintf("%.2f", low),
		"strike_price_lte":    fmt.Sprintf("%.2f", high),
		"expiration_date_lte": until.Format(time.DateOnly),
		"limit":               most,
	}, &answer); err != nil {
		return nil, nil, err
	}

	return answer.chain()
}

// Quotes reads the quote, the implied volatility and the delta of each contract
// named. The broker returns them together in one snapshot, and only there.
func (b *Broker) Quotes(ctx context.Context, symbols []string) (map[string]Quote, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	var answer snapshotsAnswer
	if err := b.call(ctx, "get_option_snapshot",
		map[string]any{"symbols": strings.Join(symbols, ",")}, &answer); err != nil {
		return nil, err
	}

	return answer.quotes(), nil
}

// call makes one tool call and reads the broker's answer into into. The answer
// is data: the broker's own wrapper says so, and nothing here treats it as
// anything else.
func (b *Broker) call(ctx context.Context, tool string, arguments map[string]any, into any) error {
	// The bound below is held by THIS function and by nothing it calls, because
	// every narrower place has already been tried and each let a live hang
	// through. ResponseHeaderTimeout on the transport stops a server that
	// accepts the connection and says nothing. It does NOT stop a server that
	// answers the headers and then never carries the reply: measured on
	// 27 August against a gateway that returned 200 in four milliseconds and
	// then held the call, where the screener, the account recorder and the
	// volatility recorder each stopped on their first call after boot and
	// logged nothing for fifty minutes. A deadline on the context does not stop
	// it either - the SDK waits on its own stream reader and returns when that
	// reader does.
	//
	// So the work runs in its own goroutine and this function returns on its own
	// limit whether or not that goroutine ever does. A caller that gets an error
	// skips a turn and says why; a caller that gets nothing is a loop that has
	// died in silence.
	bounded, giveUp := context.WithTimeout(ctx, brokerCallLimit)
	defer giveUp()

	type answer struct {
		result *mcp.CallToolResult
		err    error
	}
	// Buffered: the goroutine must never block sending to a receiver this
	// function has already stopped listening for.
	answered := make(chan answer, 1)

	go func() {
		result, err := b.ask(bounded, tool, arguments)
		answered <- answer{result: result, err: err}
	}()

	var result *mcp.CallToolResult
	select {
	case <-bounded.Done():
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("the broker's server did not answer %s within %s", tool, brokerCallLimit)
	case got := <-answered:
		if got.err != nil {
			return got.err
		}
		result = got.result
	}

	if result.IsError {
		return fmt.Errorf("the broker refused %s", tool)
	}

	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("read the answer to %s: %w", tool, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("read the answer to %s: %w", tool, err)
	}

	return nil
}

// ask makes the call on the kept session and, if that session has gone bad, once
// more on a fresh one.
func (b *Broker) ask(ctx context.Context, tool string, arguments map[string]any) (*mcp.CallToolResult, error) {
	session, err := b.connect(ctx)
	if err != nil {
		return nil, err
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err == nil {
		return result, nil
	}

	// The session is kept, so a broken one would break every call after it
	// rather than one. Drop it and try once more on a fresh one: a gateway
	// restart, a rotated credential or an idle connection the far end closed all
	// look like this, and none of them is a reason to fail a sweep.
	b.drop(session)
	if session, err = b.connect(ctx); err != nil {
		return nil, err
	}

	result, err = session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		b.drop(session)
		return nil, fmt.Errorf("call %s: %w", tool, err)
	}

	return result, nil
}

// connect hands back the session, making it if there is none.
func (b *Broker) connect(ctx context.Context) (*mcp.ClientSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.session != nil {
		return b.session, nil
	}

	// The bounds are on the TRANSPORT, not on the http.Client, and that matters
	// now that the session outlives the call. A client-wide Timeout would cut
	// the standing stream this transport keeps open, once every limit. What we
	// actually need to stop is a server that accepts the connection and then
	// says nothing - measured: reading our own orders from a machine that could
	// not reach the proxy sat inside Connect for eight minutes and came back
	// with nothing. ResponseHeaderTimeout stops exactly that and leaves a body
	// already streaming alone.
	//
	// A deadline on the context around Connect reads as the obvious fix and does
	// not work; silence_test.go held a server open and the call outlived it.
	transport := &mcp.StreamableClientTransport{
		Endpoint:   b.url,
		HTTPClient: &http.Client{Transport: b.roundTripper()},
	}

	session, err := mcp.NewClient(&mcp.Implementation{Name: b.name, Version: "v0.1.0"}, nil).
		Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("reach the broker's server: %w", err)
	}

	b.session = session

	return session, nil
}

// drop closes the session and forgets it, unless somebody already replaced it.
func (b *Broker) drop(used *mcp.ClientSession) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.session != used {
		return
	}

	b.session = nil
	_ = used.Close()
}

// Close ends the session this client keeps.
func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.session == nil {
		return nil
	}

	session := b.session
	b.session = nil

	return session.Close()
}
