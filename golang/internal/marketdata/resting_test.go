//go:build broker

package marketdata

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// What the book really says about the orders we are waiting on.
//
// The execution ladder walks toward a price it computes from each leg's quote.
// When an order rests for an hour against a book that supposedly beats its limit
// by two dollars, one of the two is lying, and only the raw quotes say which.
// Prints every resting order with its limit, each leg's bid and ask, and the
// price the ladder would read from them. Places nothing.
func TestWhatTheBookSaysAboutOurRestingOrders(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	orders, err := broker.Orders(ctx, 50)
	require.NoError(t, err)

	resting := 0
	for _, order := range orders {
		if order.Status != "new" && order.Status != "accepted" && order.Status != "partially_filled" {
			continue
		}
		resting++

		symbols := make([]string, 0, len(order.Legs))
		for _, leg := range order.Legs {
			symbols = append(symbols, leg.Symbol)
		}
		quotes, err := broker.Quotes(ctx, symbols)
		if err != nil {
			t.Logf("%s limit %.2f: quotes unavailable: %v", order.ID, order.LimitPrice, err)
			continue
		}

		t.Logf("=== %s  status %s  limit %.2f  name %q", order.ID, order.Status, order.LimitPrice, order.ClientID)
		total, priced := 0.0, true
		for _, leg := range order.Legs {
			quote, answered := quotes[leg.Symbol]
			if !answered || quote.Bid <= 0 || quote.Ask <= 0 {
				t.Logf("    %-4s %-24s no two-sided quote", leg.Side, leg.Symbol)
				priced = false
				continue
			}
			t.Logf("    %-4s %-24s bid %.2f ask %.2f (spread %.2f)",
				leg.Side, leg.Symbol, quote.Bid, quote.Ask, quote.Ask-quote.Bid)
			if leg.Side == "sell" {
				total -= quote.Bid
			} else {
				total += quote.Ask
			}
		}
		if priced {
			t.Logf("    the ladder would read %.2f against our limit %.2f", total, order.LimitPrice)
		}
	}
	t.Logf("%d resting orders of %d returned", resting, len(orders))
}
