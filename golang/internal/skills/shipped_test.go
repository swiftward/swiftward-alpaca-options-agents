package skills_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/skills"
)

// Declarations and skills are two sets of files that have to agree, and nothing
// in either says so: a declaration names a skill by a string, and a skill names
// the parameters it needs by strings. Nobody notices a typo in either until the
// agent refuses to start - which, on a schedule, means it wakes nobody all day
// and looks exactly like an agent that had nothing to do.
//
// So the real files are read here, not copies: a copy would drift from what
// ships. Run with -count=1 - Go caches a test result against the Go files it
// compiled, and none of these are among them.
func TestEveryDeclarationGetsSkillsThatExistAndParametersTheyNeed(t *testing.T) {
	const source = "../../../agent/skills"

	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			declared, err := declaration.Load(path)
			require.NoError(t, err)

			chosen, err := skills.Check(source, declared.Skills, declared.Parameters)
			require.NoError(t, err, "this declaration would refuse to start")
			require.NotEmpty(t, chosen, "an agent with no skills at all is not what this project ships")
			require.NotEmpty(t, declared.Parameters, "this declaration hands its skills no numbers")

			// Every parameter given is a parameter something asks for. One nobody
			// requires is either a typo in the name or a leftover from a skill that
			// is gone, and both read as a number in force that is not.
			asked := map[string]bool{}
			for _, skill := range chosen {
				for _, needed := range skill.Requires {
					asked[needed] = true
				}
			}
			for name := range declared.Parameters {
				assert.True(t, asked[name] || readByTheHarness[name],
					"parameter %q is given to no skill that asks for it and no part of the harness reads it",
					name)
			}

			// Every number given reaches the session as one block, which the
			// harness puts in front of whatever woke it - a scheduled window, the
			// session's own wake-up or a person in the chat.
			numbers := declared.Numbers()
			require.NotEmpty(t, numbers)
			for name, value := range declared.Parameters {
				assert.Contains(t, numbers, name+" = "+value)
			}

			// And no window repeats a number that now stands above it. A number
			// written twice is a number that will one day disagree with itself, and
			// this is the check that would have caught it.
			for i := range declared.Sessions {
				assert.NotContains(t, declared.Sessions[i].Task, "Numbers this agent runs on",
					"session %s repeats the block the harness already puts in front of it",
					declared.Sessions[i].Name)
			}
		})
	}
}

// readByTheHarness names the parameters our own processes read rather than the
// session. They are declared beside the session's numbers because they are
// TRADING decisions and every trading decision belongs in one file - but no
// skill asks for them, so the check above would call each one a leftover.
//
// Listed rather than waved through: a typo in one of these names is exactly the
// failure that check exists to catch, and it would otherwise pass here and be
// found on the first winner of the week.
var readByTheHarness = map[string]bool{
	// The share of the credit at which the profit watch buys a winner back.
	// Read on every pass by internal/takeprofit.
	"take_profit_at": true,
	// How far the account may fall from yesterday's close before the day is
	// over. Read on every pass by internal/execution, and asked of the session
	// too - playbook-premium-harvest requires it - so it is here only for a
	// declaration that drops that playbook and keeps the fuse.
	"daily_fuse_percent": true,
}
