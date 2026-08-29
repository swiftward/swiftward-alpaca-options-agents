//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

// How much of the account is actually at risk right now, measured the way the
// envelope measures it: the worst the open structures can do at expiry.
//
// The number matters because "50% of equity" sounds like half the money sits
// idle, and it does not - a credit spread's maximum loss is the collateral the
// broker holds, not capital left unused. This prints what is held against what
// is allowed, so the question is answered from the account rather than argued.
//
// Places nothing.
func TestHowMuchIsAtRisk(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")

	ctx := context.Background()
	broker := NewBroker(url)

	account, err := broker.Account(ctx)
	require.NoError(t, err)

	positions, err := broker.Positions(ctx)
	require.NoError(t, err)

	// Structures are grouped by what they expire against: one underlying, one
	// day. Legs of different days never offset each other at expiry.
	type series struct {
		underlying string
		expiration string
	}
	legs := map[series][]Position{}
	for _, position := range positions {
		contract, parsed := ContractFrom(position.Symbol)
		if !parsed {
			t.Logf("not an option, skipped: %s", position.Symbol)
			continue
		}
		key := series{contract.Symbol[:len(contract.Symbol)-15], contract.Expiration.Format("2006-01-02")}
		legs[key] = append(legs[key], position)
	}

	keys := make([]series, 0, len(legs))
	for key := range legs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].underlying != keys[j].underlying {
			return keys[i].underlying < keys[j].underlying
		}
		return keys[i].expiration < keys[j].expiration
	})

	worstTotal, creditTotal := 0.0, 0.0
	t.Log("underlying | expires | legs | worst case at expiry | already paid or taken")
	for _, key := range keys {
		held := legs[key]

		// The payoff of a spread is piecewise linear, so its worst point is at a
		// strike, at zero, or far above the highest strike. Checking those points
		// is exact, not a sample.
		var probes []float64
		probes = append(probes, 0)
		highest := 0.0
		for _, position := range held {
			contract, _ := ContractFrom(position.Symbol)
			probes = append(probes, contract.Strike)
			highest = math.Max(highest, contract.Strike)
		}
		probes = append(probes, highest*2)

		// What the position cost: a credit spread has a negative cost basis, and
		// that money is already in the account.
		credit := 0.0
		for _, position := range held {
			credit -= position.CostBasis
		}

		worst := math.MaxFloat64
		for _, price := range probes {
			payoff := 0.0
			for _, position := range held {
				contract, _ := ContractFrom(position.Symbol)
				intrinsic := 0.0
				if contract.Type == "put" {
					intrinsic = math.Max(0, contract.Strike-price)
				} else {
					intrinsic = math.Max(0, price-contract.Strike)
				}
				payoff += position.Quantity * 100 * intrinsic
			}
			worst = math.Min(worst, payoff+credit)
		}

		worstTotal += math.Min(0, worst)
		creditTotal += credit
		t.Log(fmt.Sprintf("%-6s | %s | %d | %+9.2f | %+9.2f",
			key.underlying, key.expiration, len(held), worst, credit))
	}

	t.Logf("equity %.2f, cash %.2f, options buying power %.2f",
		account.Equity, account.Cash, account.OptionsBuyingPower)
	t.Logf("worst case across everything open: %.2f = %.1f%% of equity",
		worstTotal, math.Abs(worstTotal)/account.Equity*100)
	t.Logf("credit and cost already settled: %+.2f", creditTotal)
}
