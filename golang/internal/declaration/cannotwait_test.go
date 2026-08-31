package declaration

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every window that closes the book declares that it cannot wait.
//
// A session that is due while the agent is working stands in line and tries
// again on the next tick. That is right for a window which can run at any minute
// of its hour, and wrong for the one that has to empty the book before a
// deadline: on 31 August both accounts' closing windows queued behind another
// session, and it cost nothing only because nothing expired that day. The Friday
// window is the sharper case, since the number that is submitted is read at
// 11:00 and not at the close.
//
// The names are the test's subject on purpose. A closing window renamed out of
// this family fails here, which is the moment to decide deliberately rather than
// to discover it on the last judged day.
func TestEveryClosingWindowCannotWait(t *testing.T) {
	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			declared, err := Load(path)
			require.NoError(t, err)

			closing := 0
			for i := range declared.Sessions {
				session := &declared.Sessions[i]
				if !strings.HasPrefix(session.Name, "flatten") {
					continue
				}
				closing++
				assert.True(t, session.CannotWait,
					"%s empties the book against a deadline, so it must not queue behind a running turn",
					session.Name)
			}
			assert.NotZero(t, closing, "this declaration closes nothing before the day ends")
		})
	}
}
