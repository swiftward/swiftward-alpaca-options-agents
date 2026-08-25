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
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Broker reads from the broker's server. It holds no credential: the server it
// calls does.
type Broker struct {
	url  string
	name string
}

func NewBroker(url string) *Broker {
	return &Broker{url: url, name: "swiftward-alpaca-options-agents-harness"}
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
	Symbol            string
	Bid               float64
	Ask               float64
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
	ID             string  `json:"id"`
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
	client := mcp.NewClient(&mcp.Implementation{Name: b.name, Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: b.url}, nil)
	if err != nil {
		return fmt.Errorf("reach the broker's server: %w", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return fmt.Errorf("call %s: %w", tool, err)
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
