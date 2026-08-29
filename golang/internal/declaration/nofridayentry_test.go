package declaration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Nothing we ship may OPEN a position on a Friday.
//
// It is the event's arithmetic rather than a preference. Alpaca takes the
// account's equity at the end of Thursday 3 September; LabLab counts at 15:00
// UTC on the Friday, which is 11:00 in New York. A position opened on that
// Friday therefore cannot reach the first number at all, and has four hours in
// which to hurt the second one.
//
// Until 29 August every entry window carried mon-fri, so all three declarations
// would have gone hunting for structures on submission day. It reads as an
// ordinary weekday list, which is exactly why it needs a test rather than a
// comment: the next person to add a window will copy the line above it.
//
// Closing on a Friday is not touched. The windows that flatten, defend or take
// a profit are what the deadline calls for, and one of them exists only for that
// Friday.
func TestNothingOpensOnTheSubmissionFriday(t *testing.T) {
	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			declared, err := Load(path)
			require.NoError(t, err)

			opens := 0
			for i := range declared.Sessions {
				session := &declared.Sessions[i]
				if !opensAPosition(session.Name) {
					continue
				}
				opens++

				assert.NotContains(t, session.Days, "fri",
					"%s can open a position on the submission Friday, when Alpaca has already taken its number and LabLab's clock has four hours left",
					session.Name)
			}

			// Without this the test passes on a declaration whose windows were all
			// renamed, having examined nothing - the shape of green that says
			// nothing at all.
			assert.NotZero(t, opens,
				"no session here looks like one that opens a position: the names this test matches on have changed")
		})
	}
}

// opensAPosition says whether a session's job is to OPEN something. It reads the
// name because that is what the declaration gives us: a session's task is prose
// for the model, and the name is the only part a program can rely on.
func opensAPosition(name string) bool {
	for _, opener := range []string{"entry", "convexity", "earnings-crush", "event-convexity"} {
		if !strings.HasPrefix(name, opener) {
			continue
		}
		// `earnings-crush-exit` and `event-convexity-exit` close what those
		// windows opened, and closing on the Friday is what the deadline asks for.
		if strings.HasSuffix(name, "-exit") {
			return false
		}

		return true
	}

	return false
}
