package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shape asserted here is the one the broker actually returned on
// 24 August 2026; a wake-up depends on reading it correctly.
func TestPricesFromTheBrokersAnswer(t *testing.T) {
	answer := map[string]any{
		"_alpaca_mcp_security": map[string]any{"trust": "untrusted_tool_output"},
		"data": map[string]any{
			"trades": map[string]any{
				"SPY": map[string]any{"p": 763.65, "s": 40},
				"QQQ": map[string]any{"p": 0.0},
			},
		},
	}

	prices, err := pricesFrom(answer)
	require.NoError(t, err)

	assert.Equal(t, 763.65, prices["SPY"])
	_, present := prices["QQQ"]
	assert.False(t, present, "a price of zero is not a reading, and a wake-up must not fire on it")
}

func TestAnEmptyAnswerIsNoPrices(t *testing.T) {
	prices, err := pricesFrom(map[string]any{"data": map[string]any{}})
	require.NoError(t, err)
	assert.Empty(t, prices)
}
