//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Every position expiring TODAY, against the one thing that can hurt it: the
// price closing BETWEEN its legs.
//
// That is the only outcome where a bounded loss turns into a position in shares
// - the sold leg is assigned, the bought leg expires empty. The size of the gap
// to fall into is the structure's own WIDTH, so that is the distance measured
// here, not a fixed number of cents.
//
// Prints and places nothing. Run it in the last hour.
func TestWhatExpiresTodayAndHowCloseItIs(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	positions, err := broker.Positions(ctx)
	require.NoError(t, err)

	today := time.Now().Format(time.DateOnly)
	type series struct{ underlying, kind string }
	held := map[series][]Position{}
	for _, position := range positions {
		contract, parsed := ContractFrom(position.Symbol)
		if !parsed || contract.Expiration.Format(time.DateOnly) != today {
			continue
		}
		key := series{position.Symbol[:len(position.Symbol)-15], contract.Type}
		held[key] = append(held[key], position)
	}
	if len(held) == 0 {
		t.Log("nothing expires today")
		return
	}

	names := make([]string, 0, len(held))
	for key := range held {
		names = append(names, key.underlying)
	}
	prices, err := broker.LastTrades(ctx, names)
	require.NoError(t, err)

	t.Log("underlying | sold | bought | width | price | distance to the sold strike | in the gap?")
	for key, legs := range held {
		var sold, bought Contract
		for _, one := range legs {
			contract, _ := ContractFrom(one.Symbol)
			if one.Quantity < 0 {
				sold = contract
			} else {
				bought = contract
			}
		}
		if sold.Symbol == "" || bought.Symbol == "" {
			t.Logf("%-6s %s: not a two-legged structure, %d legs - look by hand",
				key.underlying, key.kind, len(legs))
			continue
		}

		price := prices[key.underlying]
		width := math.Abs(sold.Strike - bought.Strike)
		away := math.Abs(price - sold.Strike)

		// Between the legs is where assignment turns a capped loss into shares.
		low, high := math.Min(sold.Strike, bought.Strike), math.Max(sold.Strike, bought.Strike)
		inTheGap := price > low && price < high

		verdict := "no"
		switch {
		case inTheGap:
			verdict = "IN THE GAP - CLOSE IT"
		case away <= width:
			verdict = "within one width - watch"
		}

		t.Log(fmt.Sprintf("%-6s | %7.2f | %7.2f | %5.2f | %8.2f | %6.2f | %s",
			key.underlying, sold.Strike, bought.Strike, width, price, away, verdict))
	}
	sort.Strings(names)
}
