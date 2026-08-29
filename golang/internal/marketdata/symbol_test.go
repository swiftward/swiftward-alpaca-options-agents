package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAContractSymbolSaysWhatItIs(t *testing.T) {
	for _, c := range []struct {
		symbol     string
		underlying string
		day        string
		kind       string
		strike     float64
	}{
		{"QQQ260827P00706000", "QQQ", "2026-08-27", "put", 706},
		{"SPY260825C00766500", "SPY", "2026-08-25", "call", 766.5},
		{"AAPL260828P00302500", "AAPL", "2026-08-28", "put", 302.5},
		{"MU260826C00955000", "MU", "2026-08-26", "call", 955},
	} {
		t.Run(c.symbol, func(t *testing.T) {
			read, ok := ContractFrom(c.symbol)
			require.True(t, ok)
			assert.Equal(t, c.day, read.Expiration.Format(time.DateOnly))
			assert.Equal(t, c.kind, read.Type)
			assert.InDelta(t, c.strike, read.Strike, 1e-9)
			assert.Equal(t, c.underlying, c.symbol[:len(c.symbol)-15])
		})
	}
}

// A symbol that does not parse is refused rather than half-read: a wrong strike
// in the room or in the record is worse than no strike.
func TestASymbolThatDoesNotParseIsRefused(t *testing.T) {
	for _, symbol := range []string{
		"", "QQQ", "QQQ260827P0070600", "260827P00706000",
		"QQQ261327P00706000", "QQQ260827X00706000", "QQQ260827P00000000",
		"QQQ260827PABCDEFGH",
	} {
		_, ok := ContractFrom(symbol)
		assert.False(t, ok, "%q should be refused", symbol)
	}
}
