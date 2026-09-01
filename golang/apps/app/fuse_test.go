package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
)

// A declaration that names no fuse is refused, and it is refused where the
// summary is written rather than on every pass afterwards.
//
// The number was read only inside the closure the ladder calls, so a declaration
// without it came up reporting "the day's fuse is enforced" and then wrote "could
// not tell whether the day's fuse has blown" on every pass. A teammate's stand ran
// exactly that. The comparison that settles it is our own: a record that cannot
// say which account it belongs to refuses to start, and a missing fuse is the same
// kind of absence.
func TestAFuseThatCannotBeReadIsRefusedWhereItIsClaimed(t *testing.T) {
	written := func(t *testing.T, parameters string) *declaration.Watcher {
		t.Helper()
		path := filepath.Join(t.TempDir(), "agent.yaml")
		require.NoError(t, os.WriteFile(path, []byte(`
kind: trading-agent
name: alpaca-agent-1
timezone: America/New_York
parameters:
`+parameters+`
sessions:
  - name: entry
    cause: "the entry window"
    task: "Sell a spread."
    at: "14:20"
    within: 45m
`), 0o600))
		watcher, err := declaration.Watch(path, nil)
		require.NoError(t, err)

		return watcher
	}

	t.Run("a number is read", func(t *testing.T) {
		percent, err := fusePercent(written(t, `  daily_fuse_percent: "5"`))
		require.NoError(t, err)
		assert.InDelta(t, 5.0, percent, 1e-9)
	})

	t.Run("no fuse at all", func(t *testing.T) {
		_, err := fusePercent(written(t, `  take_profit_at: "0.35"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daily_fuse_percent")
	})

	t.Run("prose a program cannot read", func(t *testing.T) {
		_, err := fusePercent(written(t, `  daily_fuse_percent: "three percent"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a number",
			"a fuse that quietly took zero from prose would cancel every opening order on a flat day")
	})

	t.Run("a fuse that fuses nothing", func(t *testing.T) {
		_, err := fusePercent(written(t, `  daily_fuse_percent: "0"`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fuses nothing")
	})
}
