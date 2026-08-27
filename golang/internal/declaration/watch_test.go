package declaration

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const oneSession = `
kind: trading-agent
name: options-alpha
timezone: America/New_York
sessions:
  - name: entry
    cause: "окно входа"
    task: "Продай спред."
    at: "14:20"
    within: 45m
`

func TestAnEditedFileBecomesTheDeclarationInForce(t *testing.T) {
	path := write(t, oneSession)

	watcher, err := Watch(path, nil)
	require.NoError(t, err)
	first := watcher.Current()
	require.Len(t, first.Sessions, 1)

	// Read again with nothing changed: the very same declaration comes back, so a
	// caller can tell "the schedule changed" from "the file was read again".
	again, err := watcher.Reread()
	require.NoError(t, err)
	assert.Same(t, first, again)

	require.NoError(t, os.WriteFile(path, []byte(oneSession+`  - name: defend
    cause: "защита"
    task: "Проверь позиции."
    every: 30m
    between: ["09:40", "15:55"]
`), 0o600))

	fresh, err := watcher.Reread()
	require.NoError(t, err)
	assert.NotSame(t, first, fresh)
	require.Len(t, fresh.Sessions, 2)
	assert.Equal(t, "defend", fresh.Sessions[1].Name)
	assert.Same(t, fresh, watcher.Current())
	assert.Len(t, watcher.Schedule(), 2, "the session's own answer is read from the same declaration")
}

// A file caught half-saved, or edited into something that does not check out,
// must not take the schedule down: the harness keeps waking sessions by what it
// has and says loudly that the disk and the clock disagree.
func TestABrokenEditLeavesTheScheduleAlone(t *testing.T) {
	path := write(t, oneSession)

	watcher, err := Watch(path, nil)
	require.NoError(t, err)
	first := watcher.Current()

	for _, broken := range []string{
		"kind: trading-agent\nname: a\ntimezone: Mars/Olympus\nsessions: []\n",
		"kind: trading-agent\n  name: broken yaml\n",
	} {
		require.NoError(t, os.WriteFile(path, []byte(broken), 0o600))

		fresh, err := watcher.Reread()
		require.Error(t, err)
		assert.Same(t, first, fresh, "the declaration in force is the one that still works")
		assert.Same(t, first, watcher.Current())
	}

	// And the good file that follows is picked up: refusing a broken edit must
	// not mean refusing every edit after it.
	require.NoError(t, os.WriteFile(path, []byte(oneSession+"    days: [mon]\n"), 0o600))
	fresh, err := watcher.Reread()
	require.NoError(t, err)
	assert.NotSame(t, first, fresh)
	assert.Equal(t, []string{"mon"}, fresh.Sessions[0].Days)
}

// Verify is how something outside this package - the skills an agent is given -
// gets a say in whether a declaration is usable at all.
func TestADeclarationThatDoesNotVerifyIsRefused(t *testing.T) {
	path := write(t, oneSession)

	refuse := false
	watcher, err := Watch(path, func(*Declaration) error {
		if refuse {
			return fmt.Errorf(`needs the parameter "short_leg_delta"`)
		}
		return nil
	})
	require.NoError(t, err)
	first := watcher.Current()

	refuse = true
	require.NoError(t, os.WriteFile(path, []byte(oneSession+"  # edited\n"), 0o600))

	fresh, err := watcher.Reread()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "short_leg_delta")
	assert.Same(t, first, fresh)
}

// At start it is a failure to start, exactly as an unreadable file always was:
// an agent whose schedule cannot be used wakes nobody all day and looks like one
// that simply had nothing to do.
func TestVerifyRefusingAtStartStopsTheProcess(t *testing.T) {
	_, err := Watch(write(t, oneSession), func(*Declaration) error {
		return fmt.Errorf(`no skill called "playbook-defence"`)
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "playbook-defence")
}
