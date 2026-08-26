//go:build broker

package marketdata

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Do the two ways this project reads a price agree?
//
// The screener prices structures from get_option_chain and leaves a ranked list.
// The session then re-reads the same legs with get_option_snapshot before it
// orders - and on 26 August the two disagreed enough to flip a decision: a QQQ
// call spread stood in the list at +7.5 points of edge and measured -7.2 on the
// session's own reading, so it refused. A list nobody can act on costs a turn
// every time it is read.
//
// This asks the broker both ways at once, for the same symbols, and prints the
// difference. Places nothing.
//
// Answered on 26 August for that very spread: the two agreed to the cent -
// 3.03/3.17 and 2.71/2.74 from both. So the disagreement was not the source, it
// was AGE. The sweep took 86 seconds on a five-minute tick, so a row could be
// seven minutes old, and seven minutes is enough for a one-wide call spread to
// move thirteen cents of credit - which is the whole distance between +7.5 and
// -7.2 points of edge. The tick went to two minutes.
//
// Run it again before blaming a feed: it is cheap, and "the price moved" is a
// far more common answer than "the data is wrong".
func TestWhetherTheChainAndTheSnapshotAgree(t *testing.T) {
	url := os.Getenv("BROKER_MCP_URL")
	require.NotEmpty(t, url, "BROKER_MCP_URL")
	symbols := os.Getenv("DRIFT_SYMBOLS")
	if symbols == "" {
		t.Skip("DRIFT_SYMBOLS not set")
	}

	ctx := context.Background()
	broker := NewBroker(url)

	wanted := strings.Split(symbols, ",")
	snapshot, err := broker.Quotes(ctx, wanted)
	require.NoError(t, err)

	// The chain for the underlying of the first symbol, wide enough to hold them.
	first, ok := ContractFrom(wanted[0])
	require.True(t, ok)
	underlying := wanted[0][:len(wanted[0])-15]

	_, chain, err := broker.Chain(ctx, underlying,
		first.Strike-20, first.Strike+20, first.Expiration, 1000)
	require.NoError(t, err)

	t.Log("symbol | chain bid/ask | snapshot bid/ask | same?")
	for _, symbol := range wanted {
		fromChain, inChain := chain[symbol]
		fromSnap, inSnap := snapshot[symbol]
		if !inChain || !inSnap {
			t.Logf("%s: chain=%v snapshot=%v", symbol, inChain, inSnap)
			continue
		}
		same := "yes"
		if fromChain.Bid != fromSnap.Bid || fromChain.Ask != fromSnap.Ask {
			same = "NO"
		}
		t.Log(fmt.Sprintf("%s | %.2f/%.2f | %.2f/%.2f | %s",
			symbol, fromChain.Bid, fromChain.Ask, fromSnap.Bid, fromSnap.Ask, same))
	}
}
