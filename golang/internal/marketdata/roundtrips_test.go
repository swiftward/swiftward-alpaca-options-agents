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

// What our own trades actually earned, круг за кругом.
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
		// Не "filled", а "что-то исполнилось": частично исполненная и затем снятая
		// заявка двигала деньги ровно так же, и выбросить её значит разойтись со
		// счётом на её величину.
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
			// Страйк в ключе обязателен: 25 августа в один день шли QQQ 706/705 и
			// 710/709, и ключ из бумаги с датой слил бы их в один круг, которого
			// никогда не было.
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
	t.Log("серия | ног | контрактов | получено | отдано | сборы | итог")
	for _, key := range keys {
		round := rounds[key]
		fees := round.contracts * feePerContractPerLeg
		result := round.taken - round.paid - fees
		total += result
		totalFees += fees
		t.Log(fmt.Sprintf("%-22s | %3d | %7.0f | %+9.2f | %+9.2f | %6.2f | %+9.2f",
			key, round.legs, round.contracts, round.taken, -round.paid, -fees, result))
	}

	t.Logf("кругов: %d, итог %+.2f, из них сборы %.2f", len(keys), total, totalFees)
	t.Log("ОГОВОРКИ, обе важные:")
	t.Log("  серия, открытая и НЕ закрытая, показана как чистый кредит - её настоящий " +
		"итог станет известен на истечении или при закрытии;")
	t.Log("  истечение в деньгах и исполнение в заявках НЕ ВИДНЫ вовсе: их делает " +
		"брокер, а не мы. В плохой день итог этой таблицы разойдётся со счётом, и " +
		"сверять её надо с движением капитала, а не с самой собой.")
}
