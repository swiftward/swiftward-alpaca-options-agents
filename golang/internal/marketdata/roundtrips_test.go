//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// What our own trades actually earned, round trip by round trip.
//
// The question this answers is the only one that counts at the end of the week,
// and it has been argued about instead of measured: thresholds were moved three
// days running without anyone reading back what the previous setting produced.
//
// A round trip is one series - one underlying, one expiration, one set of strikes
// - from the order that opened it to the order that closed it, or to the day it
// expired. Money in is what the broker paid us; money out is what buying it back
// cost. Fees are real and measured: 0.025 per contract per leg, confirmed twice
// on 25 August by watching cash move.
//
// Places nothing. Reads orders and prints a table.
func TestWhatOurRoundTripsEarned(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	broker := NewBroker(url)
	if token := os.Getenv("BROKER_MCP_TOKEN"); token != "" {
		broker = NewBrokerWithToken(url, token)
	}
	orders, err := broker.Orders(context.Background(), 500)
	require.NoError(t, err)

	// A leg's own fill is what matters: a multi-leg order carries one price for
	// the structure, and the legs carry theirs.
	type money struct {
		taken     float64 // credit received, positive
		paid      float64 // debit paid, positive
		contracts float64
		legs      int
		opened    time.Time
		closed    time.Time
		symbols   map[string]bool
	}
	rounds := map[string]*money{}

	for _, order := range orders {
		// Not "filled" but "something filled": an order partly filled and then
		// cancelled moved money in exactly the same way, and dropping it means
		// diverging from the account by its size.
		if order.FilledQuantity <= 0 || order.FilledAt == nil {
			continue
		}
		legs := order.Legs
		if len(legs) == 0 {
			legs = []Order{order}
		}
		for _, leg := range legs {
			contract, parsed := ContractFrom(leg.Symbol)
			if !parsed {
				continue
			}
			// The strike has to be in the key: on 25 August QQQ 706/705 and 710/709
			// ran on the same day, and a key of underlying plus date would merge them
			// into one round trip that never existed.
			key := fmt.Sprintf("%s %s %.0f",
				contract.Symbol[:len(contract.Symbol)-15],
				contract.Expiration.Format("2006-01-02"),
				contract.Strike)

			round, seen := rounds[key]
			if !seen {
				round = &money{opened: *order.FilledAt, symbols: map[string]bool{}}
				rounds[key] = round
			}
			if order.FilledAt.Before(round.opened) {
				round.opened = *order.FilledAt
			}
			if order.FilledAt.After(round.closed) {
				round.closed = *order.FilledAt
			}
			round.symbols[leg.Symbol] = true
			round.legs++

			filled := leg.FilledQuantity
			if filled == 0 {
				filled = order.FilledQuantity
			}
			round.contracts += filled

			cash := leg.FilledPrice * filled * 100
			if leg.Side == "sell" {
				round.taken += cash
			} else {
				round.paid += cash
			}
		}
	}

	keys := make([]string, 0, len(rounds))
	for key := range rounds {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	const feePerContractPerLeg = 0.025

	total, totalFees := 0.0, 0.0
	t.Log("series | legs | contracts | received | paid | fees | result")
	for _, key := range keys {
		round := rounds[key]
		fees := round.contracts * feePerContractPerLeg
		result := round.taken - round.paid - fees
		total += result
		totalFees += fees
		t.Log(fmt.Sprintf("%-22s | %3d | %7.0f | %+9.2f | %+9.2f | %6.2f | %+9.2f",
			key, round.legs, round.contracts, round.taken, -round.paid, -fees, result))
	}

	t.Logf("round trips: %d, result %+.2f, of which fees %.2f", len(keys), total, totalFees)
	t.Log("TWO CAVEATS, both important:")
	t.Log("  a series opened and NOT closed is shown as the bare credit - its real " +
		"result becomes known at expiry or on the close;")
	t.Log("  expiry in the money and assignment are NOT VISIBLE in orders at all: the " +
		"broker does them, not us. On a bad day this table's total will diverge from " +
		"the account, and it must be checked against the equity curve, not against itself.")
}
