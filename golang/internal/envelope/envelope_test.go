package envelope

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "envelope.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

const oneRule = `
ruleset_version: "test.1"
agents:
  options-alpha:
    tools:
      place_option_order:
`

// The whole disclosure model rests on this: a rule that may only say it exists
// must not carry a number in some other field. A file that tries is rejected
// where it can still be read by a person, not where an agent reads a boundary it
// was never meant to see.
func TestExistenceMayCarryNoNumber(t *testing.T) {
	_, err := Load(write(t, oneRule+`
        - rule: entry-frequency
          disclosure: existence
          subject: position_max_loss
          kind: maximum
          value: 3
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may carry no subject, kind, value or unit")
}

func TestBoundaryWithoutAValueIsRefused(t *testing.T) {
	_, err := Load(write(t, oneRule+`
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must carry a value")
}

// A subject nobody agreed on means one thing to whoever writes the rule and
// another to the session reading it, and neither can see the difference.
func TestSubjectAndUnitComeFromClosedLists(t *testing.T) {
	_, err := Load(write(t, oneRule+`
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_size
          kind: maximum
          value: 0.5
          unit: percent_of_equity
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `subject "position_size" is not one of`)

	_, err = Load(write(t, oneRule+`
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 0.5
          unit: dollars
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unit "dollars" is not one of`)
}

func TestAnEnvelopeWithoutItsVersionIsRefused(t *testing.T) {
	_, err := Load(write(t, `
agents:
  options-alpha:
    tools: {}
`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ruleset_version is required")
}

// A caller nobody recognises must not be handed an empty envelope: empty reads
// exactly like "this tool is not governed", and the session would trade on it.
func TestAnUnknownCallerIsRefusedRatherThanAnsweredEmpty(t *testing.T) {
	set, err := Load(write(t, oneRule+`
        - rule: entry-frequency
          disclosure: existence
`))
	require.NoError(t, err)

	_, err = set.For("someone-else", "place_option_order")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "under no ruleset")
}

// An ungoverned tool answers, and its empty list crosses as [] rather than null:
// a session told `null` cannot tell "nothing was disclosed" from "the field is
// missing", and both readings end in a trade.
func TestAToolUnderNoRuleAnswersGovernedFalse(t *testing.T) {
	set, err := Load(write(t, oneRule+`
        - rule: entry-frequency
          disclosure: existence
`))
	require.NoError(t, err)

	out, err := set.For("options-alpha", "get_clock")
	require.NoError(t, err)
	assert.False(t, out.Governed)
	assert.Empty(t, out.Constraints)
	assert.Equal(t, "test.1", out.RulesetVersion)
	assert.Equal(t, "options-alpha", out.Identity)

	written, err := json.Marshal(out)
	require.NoError(t, err)
	assert.Contains(t, string(written), `"constraints":[]`)
}

// Two reads of an unchanged ruleset must differ nowhere, or a session comparing
// them sees a change that did not happen.
func TestTheSameRulesetAnswersTheSameBytes(t *testing.T) {
	path := write(t, oneRule+`
        - rule: permitted-expirations
          disclosure: boundary
          subject: expiration
          kind: range
          value: {min: 1, max: 5}
          unit: trading_days_from_today
        - rule: entry-frequency
          disclosure: existence
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 0.5
          unit: percent_of_equity
`)
	first, err := Load(path)
	require.NoError(t, err)
	second, err := Load(path)
	require.NoError(t, err)

	one, err := first.For("options-alpha", "place_option_order")
	require.NoError(t, err)
	two, err := second.For("options-alpha", "place_option_order")
	require.NoError(t, err)

	a, err := json.Marshal(one)
	require.NoError(t, err)
	b, err := json.Marshal(two)
	require.NoError(t, err)
	assert.Equal(t, string(a), string(b))

	names := []string{}
	for _, c := range one.Constraints {
		names = append(names, c.Rule)
	}
	assert.Equal(t, []string{"entry-frequency", "max-loss-per-position", "permitted-expirations"}, names)
}

// The ruleset this repository ships must carry the numbers the sessions used to
// carry in their own text. This is the guard on the move itself: the numbers left
// the tasks in the same change that put them here, and if one of them is edited
// away the tests say so rather than the market.
func TestTheShippedRulesetCarriesWhatTheTasksGaveUp(t *testing.T) {
	set, err := Load(filepath.Join("..", "..", "..", "policy", "envelope.yaml"))
	require.NoError(t, err)

	// Raised from 0.5 and 2.5 on 26 August: the day's measurements said the
	// binding constraint was what the round trip costs, not the size, so the size
	// went up and the cost filter went down. Changing these is a decision, and
	// this test is what makes it one rather than a slip.
	// Raised to 15 on 26 August, by Kostya's decision after the accounts were
	// measured at 26.4 percent of risk against 50 permitted. His words: a fifth
	// of the account at risk each day arrives nowhere, and the week is for
	// winning, not for surviving.
	for identity, expected := range map[string]float64{
		"options-alpha":      15,
		"options-alpha-near": 15,
	} {
		out, err := set.For(identity, "place_option_order")
		require.NoError(t, err, identity)
		assert.True(t, out.Governed, identity)

		by := map[string]Constraint{}
		for _, c := range out.Constraints {
			by[c.Rule] = c
		}

		assert.Equal(t, expected, by["max-loss-per-position"].Value, identity)
		// Raised to 80 on 26 August. A hundred is the wall and it is the broker's,
		// not ours: the sum of maximum losses IS the collateral the broker holds,
		// so at a hundred the options buying power is zero and nothing is left to
		// defend with, roll with, or place the Friday event bet with.
		assert.Equal(t, 80, by["max-loss-across-portfolio"].Value, identity)
		// Widened from 20 on 26 August so the permitted list and what the screener
		// prices are the same names. They had drifted apart the moment the screener
		// appeared, and the cost was visible: it ranked TQQQ at 40.9 percent and
		// DIA at 25.8, the session asked, and the envelope refused both for being
		// off a list nobody had revisited.
		assert.Len(t, by["permitted-underlyings"].Value, 284, identity)
		// Opened to today on 26 August, and it was the day's largest single find.
		// Measured at 17:15 with two and three quarter hours left: QQQ a fifth of
		// a percent out paid 49 percent of its risk with the crossing at 6 percent
		// of the credit, against 8 to 15 percent at a crossing of 20 to 100 on
		// everything one to five days out. Shutting expiry day out was mine, made
		// that morning because the broker computes no delta then - which is a
		// reason to measure the structure differently, not to refuse the book that
		// pays most on the day.
		assert.Equal(t,
			map[string]any{"min": 0, "max": 5},
			by["permitted-expirations"].Value, identity)

		// Counted by the engine, so it can never show a number - by construction,
		// not by an oversight somebody could correct.
		assert.Equal(t, Existence, by["entry-frequency"].Disclosure, identity)
		assert.Nil(t, by["entry-frequency"].Value, identity)
	}
}
