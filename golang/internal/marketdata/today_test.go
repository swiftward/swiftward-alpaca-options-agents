//go:build broker

package marketdata

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWhatWeHoldExpiringToday(t *testing.T) {
	broker := NewBroker(os.Getenv("BROKER_MCP_URL"))
	ctx := context.Background()
	positions, err := broker.Positions(ctx)
	require.NoError(t, err)

	today := time.Now().Format("060102")
	underlyings := map[string]bool{}
	var held []Position
	for _, position := range positions {
		if strings.Contains(position.Symbol, today) {
			held = append(held, position)
			if contract, ok := ContractFrom(position.Symbol); ok {
				underlyings[position.Symbol[:len(position.Symbol)-15]] = true
				_ = contract
			}
		}
	}
	names := make([]string, 0, len(underlyings))
	for name := range underlyings {
		names = append(names, name)
	}
	prices := map[string]float64{}
	if len(names) > 0 {
		prices, _ = broker.LastTrades(ctx, names)
	}

	for _, position := range held {
		contract, ok := ContractFrom(position.Symbol)
		if !ok {
			continue
		}
		name := position.Symbol[:len(position.Symbol)-15]
		price := prices[name]
		money := "вне денег"
		if contract.Type == "put" && price < contract.Strike {
			money = "В ДЕНЬГАХ"
		}
		if contract.Type == "call" && price > contract.Strike {
			money = "В ДЕНЬГАХ"
		}
		t.Logf("%s %s %.1f qty %.0f, %s %.2f - %s",
			name, contract.Type, contract.Strike, position.Quantity, name, price, money)
	}
	t.Logf("%d позиций с экспирацией сегодня", len(held))
}
