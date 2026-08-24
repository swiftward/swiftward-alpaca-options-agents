// Package marketdata reads prices for the harness.
//
// The harness watches prices only to know when to wake a session - it never
// decides anything from them. The session does the deciding, with its own tools.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Broker reads last trades from the broker's own server, over the same protocol
// the session uses. It holds no credential: the server it calls does.
type Broker struct {
	url  string
	name string
}

func NewBroker(url string) *Broker {
	return &Broker{url: url, name: "swiftward-alpaca-options-agents-harness"}
}

// LastTrades returns the last traded price of each symbol it could read. A
// symbol the broker did not answer for is absent rather than zero: zero is a
// price, and a wake-up must not fire on a missing reading.
func (b *Broker) LastTrades(ctx context.Context, symbols []string) (map[string]float64, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	client := mcp.NewClient(&mcp.Implementation{Name: b.name, Version: "v0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: b.url}, nil)
	if err != nil {
		return nil, fmt.Errorf("reach the broker's server: %w", err)
	}
	defer func() { _ = session.Close() }()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_stock_latest_trade",
		Arguments: map[string]any{"symbols": strings.Join(symbols, ",")},
	})
	if err != nil {
		return nil, fmt.Errorf("read last trades: %w", err)
	}
	if result.IsError {
		return nil, fmt.Errorf("the broker refused to read last trades")
	}

	return pricesFrom(result.StructuredContent)
}

// pricesFrom digs the prices out of the broker's answer. Its shape is
// {"data": {"trades": {"SPY": {"p": 763.65}}}}, and the wrapper around it says
// so itself: the payload is data, not instructions.
func pricesFrom(structured any) (map[string]float64, error) {
	raw, err := json.Marshal(structured)
	if err != nil {
		return nil, fmt.Errorf("read the broker's answer: %w", err)
	}

	var answer struct {
		Data struct {
			Trades map[string]struct {
				Price float64 `json:"p"`
			} `json:"trades"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, fmt.Errorf("read the broker's answer: %w", err)
	}

	prices := make(map[string]float64, len(answer.Data.Trades))
	for symbol, trade := range answer.Data.Trades {
		if trade.Price > 0 {
			prices[strings.ToUpper(symbol)] = trade.Price
		}
	}

	return prices, nil
}
