package marketdata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shapes the broker answers in, read from real answers rather than from the
// documentation. What the server actually sends is the only thing worth holding
// a test against.

// A chain that did not fit in one answer is refused, not used.
//
// The broker returns a page token when the strikes asked for exceed the limit,
// and the answer carries no sign of which part of the book it holds. Using it
// would make the screener report "nothing here" for a name whose best structure
// merely fell off the page - a silence indistinguishable from a real one.
func TestATruncatedChainIsRefused(t *testing.T) {
	var truncated chainAnswer
	require.NoError(t, json.Unmarshal([]byte(`{"data":{
		"next_page_token":"UVFRMjYwODMxQzAwNzAxMDAw",
		"snapshots":{"QQQ260828P00701000":{"latestQuote":{"bp":0.71,"ap":0.79}}}
	}}`), &truncated))

	_, _, err := truncated.chain()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not fit")

	// The same answer without a token is read normally.
	var whole chainAnswer
	require.NoError(t, json.Unmarshal([]byte(`{"data":{
		"snapshots":{"QQQ260828P00701000":{"latestQuote":{"bp":0.71,"ap":0.79,"bs":12,"as":8}}}
	}}`), &whole))

	contracts, quotes, err := whole.chain()
	require.NoError(t, err)
	require.Len(t, contracts, 1)
	assert.InDelta(t, 701, contracts[0].Strike, 1e-9)
	assert.Equal(t, 12, quotes["QQQ260828P00701000"].BidSize)
	assert.Equal(t, 8, quotes["QQQ260828P00701000"].AskSize)
}
