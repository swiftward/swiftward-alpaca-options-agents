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
				assert.True(t, asked[name],
					"parameter %q is given to no skill that asks for it", name)
			}

			// The numbers are in front of every session, not only the ones that
			// trade: a session cannot follow a technique without seeing them, and
			// which session ends up doing that is the task's to decide.
			for i := range declared.Sessions {
				prompt := declared.Sessions[i].Prompt()
				for name := range declared.Parameters {
					assert.Contains(t, prompt, name, declared.Sessions[i].Name)
				}
			}
		})
	}
}
