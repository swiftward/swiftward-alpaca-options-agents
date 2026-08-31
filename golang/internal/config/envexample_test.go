package config_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every variable the stack cannot start without is in `.env.example`.
//
// The README's first two lines are `cp .env.example .env` and `make local-up`,
// so that file IS the instructions. It fell eighteen variables behind
// `compose.yaml` without anything noticing, because the machine that runs the
// stack has a `.env` that has them and never reads the example. From a fresh
// clone `docker compose config` did not render at all.
//
// Only variables written WITHOUT a default are checked: `${X:-something}` says
// in the file itself what happens when nobody sets it.
func TestEveryRequiredVariableIsInTheExample(t *testing.T) {
	compose, err := os.ReadFile("../../../compose.yaml")
	require.NoError(t, err, "the path this test reads has moved")
	example, err := os.ReadFile("../../../.env.example")
	require.NoError(t, err)

	offered := map[string]bool{}
	for _, line := range strings.Split(string(example), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "# ")
		if name, _, found := strings.Cut(line, "="); found {
			offered[strings.TrimSpace(name)] = true
		}
	}

	var missing []string
	needed := regexp.MustCompile(`\$\{([A-Z_][A-Z0-9_]*)\}`)
	for _, match := range needed.FindAllStringSubmatch(string(compose), -1) {
		if !offered[match[1]] {
			missing = append(missing, match[1])
		}
	}
	sort.Strings(missing)
	assert.Empty(t, uniq(missing),
		"compose.yaml requires these and .env.example never mentions them, so a fresh clone cannot start")
}

func uniq(names []string) []string {
	seen, out := map[string]bool{}, []string{}
	for _, name := range names {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}

	return out
}
