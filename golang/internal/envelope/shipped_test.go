package envelope_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/envelope"
)

// The envelope we ship is loaded here, not a copy of it, and every value in an
// `enum` rule is a STRING.
//
// This began as a suspected defect and is not one, which is worth writing down
// so nobody finds it again. The permitted list carries a bare `ON` - ON
// Semiconductor - and YAML 1.1 turns bare ON, OFF, YES and NO into booleans. A
// scan in Python said all three callers permitted `true` instead of the name.
// Our loader is `gopkg.in/yaml.v3`, which is YAML 1.2, and reads every one of
// them as text; checked directly against the library rather than assumed. The
// name is quoted in the file anyway, because it costs nothing and the file is
// read by eyes as well as by that one parser.
//
// The guard stays because the class is real: a value in this list that stops
// being text stops being a name the gateway can match, and nothing in the file
// would look wrong to a reader.
func TestTheShippedEnvelopeHasNoValueYAMLQuietlyRetyped(t *testing.T) {
	set, err := envelope.Load("../../../policy/envelope.yaml")
	require.NoError(t, err, "the envelope we ship must load")
	require.NotEmpty(t, set.Agents)

	for identity, agent := range set.Agents {
		for tool, constraints := range agent.Tools {
			for _, rule := range constraints {
				values, isList := rule.Value.([]any)
				if !isList {
					continue
				}
				for i, value := range values {
					text, isText := value.(string)
					assert.True(t, isText,
						"%s on %s: %s value %d is %T (%v), not text - a bare ON, OFF, YES or NO becomes a boolean",
						identity, tool, rule.Rule, i, value, value)
					assert.NotEmpty(t, text,
						"%s on %s: %s value %d is empty", identity, tool, rule.Rule, i)
				}
			}
		}
	}
}

// Every caller is governed by the same list of underlyings.
//
// Three callers hold three copies of it. They are meant to be identical, and a
// name added to one and forgotten in another is a rule that means one thing for
// one account and another for the next - measured on the same day, compared, and
// wrong.
func TestEveryCallerPermitsTheSameUnderlyings(t *testing.T) {
	set, err := envelope.Load("../../../policy/envelope.yaml")
	require.NoError(t, err)

	lists := map[string][]any{}
	for identity, agent := range set.Agents {
		for _, constraints := range agent.Tools {
			for _, rule := range constraints {
				if rule.Rule != "permitted-underlyings" {
					continue
				}
				values, isList := rule.Value.([]any)
				require.True(t, isList, "%s: permitted-underlyings is not a list", identity)
				lists[identity] = values
			}
		}
	}
	require.NotEmpty(t, lists, "no caller declares which underlyings it may trade")

	var first string
	for identity := range lists {
		if first == "" || identity < first {
			first = identity
		}
	}
	for identity, values := range lists {
		assert.Equal(t, lists[first], values,
			"%s permits a different set of underlyings from %s", identity, first)
	}
}
