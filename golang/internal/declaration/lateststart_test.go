package declaration

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// No session we ship can START after the market has closed.
//
// A window is due at `at` and stays due for `within`, so the last minute it can
// begin is their sum - and a session that begins after the bell reads its clock,
// finds the market shut and does nothing. That is not a wasted turn but a missing
// one: the window that exists to close what must not be held overnight has then
// simply not happened, and the log says a session ran.
//
// It was true of the flatten window until 29 August: 15:50 with twenty minutes
// could begin at 16:09, and the task it carries waits for a limit order before
// falling back to the market. Codex found it by reading; this finds it on every
// declaration we ship, including the ones somebody adds later.
func TestNoSessionCanStartAfterTheClose(t *testing.T) {
	// 16:00 in New York, the hour the exchange stops. The declarations carry
	// local times and their own timezone, so the comparison is in minutes of the
	// day rather than in a zone.
	const closes = 16 * 60

	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			declared, err := Load(path)
			require.NoError(t, err)

			for i := range declared.Sessions {
				session := &declared.Sessions[i]
				latest, described, ok := latestStart(t, session)
				if !ok {
					continue
				}
				assert.LessOrEqual(t, latest, closes,
					"%s can begin at %s, after the market closes: %s",
					session.Name, minutesAsTime(latest), described)
			}
		})
	}
}

// latestStart is the last minute of the day a session can begin, and how that
// number is arrived at. It reports false where the session names no time at all.
func latestStart(t *testing.T, session *Session) (int, string, bool) {
	t.Helper()

	switch {
	case session.At != "":
		at := minutesOf(t, session.At)
		within := 0
		if session.Within != "" {
			d, err := time.ParseDuration(session.Within)
			require.NoError(t, err, "%s: within %q", session.Name, session.Within)
			within = int(d.Minutes())
		}

		return at + within, session.At + " plus " + session.Within, true
	case len(session.Between) == 2:
		return minutesOf(t, session.Between[1]), "the end of " + session.Between[0] + "-" + session.Between[1], true
	default:
		return 0, "", false
	}
}

func minutesOf(t *testing.T, clock string) int {
	t.Helper()

	parsed, err := time.Parse("15:04", clock)
	require.NoError(t, err, "%q is not a time of day", clock)

	return parsed.Hour()*60 + parsed.Minute()
}

func minutesAsTime(minutes int) string {
	return time.Date(2026, 1, 1, minutes/60, minutes%60, 0, 0, time.UTC).Format("15:04")
}
