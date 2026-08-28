package envelope

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The shipped ruleset states the position limit as a share of the account, and
// the ladder turns that share into dollars. Reading it wrong is not a small
// error: it is the difference between a ceiling and no ceiling at all.
//
// What is checked is the SHAPE, not the number. It asserted 15 until 28 August,
// when the limit was tuned three times in three minutes - 15 to 5 to 8, each
// with a reason - and left the build red through every one of them. A test that
// fails on a legitimate tuning teaches the team to ignore it, which is worse
// than not having it: the day it fails for a real reason, nobody looks.
//
// So the guards are the ones that cannot be a deliberate choice: the rule is
// there, it is a share of equity and not dollars, it is inside sane bounds, and
// the two identities agree - one drifting from the other is a mistake, never a
// decision.
func TestTheShippedRulesetGivesTheShareTheLadderEnforces(t *testing.T) {
	set, err := Load(filepath.Join("..", "..", "..", "policy", "envelope.yaml"))
	require.NoError(t, err)

	shares := map[string]float64{}
	for _, identity := range []string{"options-alpha", "options-alpha-near"} {
		out, err := set.For(identity, "place_option_order")
		require.NoError(t, err, identity)

		share, err := out.PercentOfEquity("max-loss-per-position")
		require.NoError(t, err, identity)
		assert.Positive(t, share, identity, "a ceiling of zero would refuse every order")
		assert.LessOrEqual(t, share, 100.0, identity, "one position may not risk more than the account holds")

		book, err := out.PercentOfEquity("max-loss-across-portfolio")
		require.NoError(t, err, identity)
		assert.LessOrEqual(t, share, book, identity,
			"one position may not be allowed more than the whole book")

		shares[identity] = share
	}

	assert.InDelta(t, shares["options-alpha"], shares["options-alpha-near"], 1e-9,
		"the two agents run the same limit; one drifting from the other is a slip, not a choice")
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
