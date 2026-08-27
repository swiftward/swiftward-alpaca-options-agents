package declaration

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every number a declaration trades on says where it came from.
//
// The point is not tidiness. On 27 August a full day of measurement found that
// two of these numbers had been making the account LOSE - the delta ceiling was
// set from a single sweep and the edge threshold sat on the wrong side of where
// expectancy changes sign - and neither carried any mark of how it had been
// chosen, so nobody could tell a measured number from one somebody liked the
// sound of.
//
// So each parameter carries one of two marks in the comment above it:
//
//	ПОСЧИТАНО - a measurement stands behind it. Say which script and when.
//	ИЗ ПРАВИЛ - it follows from something outside us: the platform's deadline,
//	            the broker's own limit. Not measured because there is nothing to
//	            measure; say which rule.
//	ВРЕМЕННО  - nobody measured it yet. Say so out loud; it is in the queue.
//
// Unmarked is what this test refuses. Refusing is cheap and the alternative is
// what we already lived through: numbers that look equally authoritative
// whether they cost a day of computing or a minute of taste.
func TestEveryTradedNumberSaysWhereItCameFrom(t *testing.T) {
	shipped, err := filepath.Glob("../../../agent/*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, shipped, "no declarations found: the path this test reads has moved")

	// A parameter line: two spaces, a name, a colon. Nested keys go deeper and
	// are not parameters of their own.
	parameter := regexp.MustCompile(`^ {2}([a-z_]+):`)

	for _, path := range shipped {
		t.Run(filepath.Base(path), func(t *testing.T) {
			file, err := os.Open(path)
			require.NoError(t, err)
			defer func() { _ = file.Close() }()

			var (
				inParameters bool
				marked       bool
				bare         []string
			)
			lines := bufio.NewScanner(file)
			for lines.Scan() {
				line := lines.Text()
				trimmed := strings.TrimSpace(line)

				switch {
				case line == "parameters:":
					inParameters = true
					continue
				// Any other top-level key ends the block.
				case inParameters && line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
					inParameters = false
				}
				if !inParameters {
					continue
				}

				if strings.HasPrefix(trimmed, "#") {
					if strings.Contains(line, "ПОСЧИТАНО") ||
						strings.Contains(line, "ИЗ ПРАВИЛ") ||
						strings.Contains(line, "ВРЕМЕННО") {
						marked = true
					}

					continue
				}
				if found := parameter.FindStringSubmatch(line); found != nil {
					if !marked {
						bare = append(bare, found[1])
					}
					marked = false

					continue
				}
				// A blank line separates one parameter's comment from the next
				// parameter's, so a mark does not carry across the gap.
				if trimmed == "" {
					marked = false
				}
			}
			require.NoError(t, lines.Err())

			// Only ours is required to pass. The experiment declarations belong to
			// whoever runs those accounts, and marking someone else's numbers is
			// their call, not this test's - widen this once they agree.
			if filepath.Base(path) != "agent.yaml" {
				if len(bare) > 0 {
					t.Logf("не наш файл, поэтому только к сведению - без пометки: %s", strings.Join(bare, ", "))
				}

				return
			}

			require.Empty(t, bare,
				"these numbers are traded on and say nothing about where they came from: %s.\n"+
					"Put ПОСЧИТАНО (with the script and the date), ИЗ ПРАВИЛ (with the rule), "+
					"or ВРЕМЕННО in the comment above each.",
				strings.Join(bare, ", "))
		})
	}
}
