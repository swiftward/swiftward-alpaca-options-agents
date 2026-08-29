//go:build broker

package marketdata

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Watches what happens to a position expiring today, minute by minute.
//
// On 25 August a sold put that was in the money vanished from the account
// between 19:40 and 19:49 UTC, and the record shows our agent sent no closing
// order at all: it was closed by an order priced at zero and named with a bare
// UUID. Either the broker closes an in-the-money short before expiry, or
// something else does - and the difference decides whether assignment is
// something this account can ever show us.
//
// Minutes come from WATCH_MINUTES; it prints every position and order expiring
// today on each pass, and says plainly when one disappears. Places nothing.
func TestWhatHappensOnExpiryDay(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	minutes := 20
	if raw := os.Getenv("WATCH_MINUTES"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		require.NoError(t, err, "WATCH_MINUTES")
		minutes = parsed
	}

	ctx := context.Background()
	broker := NewBroker(url)
	today := time.Now().Format("060102")

	seen := map[string]bool{}
	for pass := range minutes {
		positions, err := broker.Positions(ctx)
		if err != nil {
			t.Logf("%s could not read positions: %v", time.Now().Format("15:04:05"), err)
			time.Sleep(time.Minute)
			continue
		}

		now := map[string]float64{}
		for _, position := range positions {
			if strings.Contains(position.Symbol, today) {
				now[position.Symbol] = position.Quantity
			}
		}

		for symbol := range seen {
			if _, still := now[symbol]; !still {
				t.Logf("%s *** %s GONE ***", time.Now().Format("15:04:05"), symbol)
			}
		}
		for symbol, quantity := range now {
			if !seen[symbol] {
				t.Logf("%s + %s %.0f", time.Now().Format("15:04:05"), symbol, quantity)
			}
		}
		seen = map[string]bool{}
		for symbol := range now {
			seen[symbol] = true
		}

		if pass%5 == 0 {
			orders, err := broker.Orders(ctx, 30)
			if err == nil {
				for _, order := range orders {
					legs := ""
					for _, leg := range order.Legs {
						legs += leg.Symbol + " "
					}
					if strings.Contains(legs, today) && order.Status != "canceled" {
						t.Logf("%s   order %s %s limit %.2f name %q",
							time.Now().Format("15:04:05"), order.ID[:8], order.Status,
							order.LimitPrice, order.ClientID)
					}
				}
			}
		}

		t.Logf("%s watching %d positions expiring today", time.Now().Format("15:04:05"), len(now))
		time.Sleep(time.Minute)
	}
}
