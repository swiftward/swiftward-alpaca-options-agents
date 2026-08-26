package envelope

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shipped ruleset states the position limit as a share of the account, and
// the ladder turns that share into dollars. Reading it wrong is not a small
// error: it is the difference between a fifteen thousand dollar ceiling and no
// ceiling at all.
func TestTheShippedRulesetGivesTheShareTheLadderEnforces(t *testing.T) {
	set, err := Load(filepath.Join("..", "..", "..", "policy", "envelope.yaml"))
	require.NoError(t, err)

	for _, identity := range []string{"options-alpha", "options-alpha-near"} {
		out, err := set.For(identity, "place_option_order")
		require.NoError(t, err, identity)

		share, err := out.PercentOfEquity("max-loss-per-position")
		require.NoError(t, err, identity)
		assert.InDelta(t, 15, share, 1e-9, identity)
	}
}

// A rule stated in something other than a share of the account is refused, not
// read as one. The same rule could be stated in dollars one day, and reading a
// dollar figure as a percentage raises the ceiling a thousandfold in silence.
func TestAShareStatedInAnotherUnitIsRefused(t *testing.T) {
	dollars := Envelope{Constraints: []Constraint{
		{Rule: "max-loss-per-position", Value: 15000.0, Unit: "dollars"},
	}}
	_, err := dollars.PercentOfEquity("max-loss-per-position")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dollars")

	// And a rule that is simply absent is an error, never a zero.
	empty := Envelope{}
	_, err = empty.PercentOfEquity("max-loss-per-position")
	require.Error(t, err)
}
