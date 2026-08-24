package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A page reading null cannot tell "nothing happened yet" from "this field does
// not work", so an empty state serializes as empty lists.
func TestEmptyStateSerializesAsLists(t *testing.T) {
	raw, err := json.Marshal(NewMemory().Read())
	require.NoError(t, err)
	assert.JSONEq(t, `{"ruleset":"none","limits":[],"intents":[],"refusals":[]}`, string(raw))
}

func TestReadReturnsACopy(t *testing.T) {
	m := NewMemory()
	m.AppendIntent(Intent{At: time.Unix(0, 0).UTC(), Session: "entry"})

	first := m.Read()
	first.Intents[0].Session = "changed by the caller"

	assert.Equal(t, "entry", m.Read().Intents[0].Session)
}
