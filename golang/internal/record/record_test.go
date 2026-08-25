package record

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page reading null cannot tell "nothing happened yet" from "this field does
// not work", so an empty state serializes as empty lists.
func TestEmptyStateSerializesAsLists(t *testing.T) {
	state, err := NewMemory().Read(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	assert.JSONEq(t, `{"turns":[],"calls":[],"intents":[],"refusals":[]}`, string(raw))
}

func TestReadReturnsACopy(t *testing.T) {
	m := NewMemory()
	require.NoError(t, m.AppendIntent(context.Background(), Intent{At: time.Unix(0, 0).UTC(), Session: "entry"}))

	first, err := m.Read(context.Background())
	require.NoError(t, err)
	first.Intents[0].Session = "changed by the caller"

	again, err := m.Read(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "entry", again.Intents[0].Session)
}
