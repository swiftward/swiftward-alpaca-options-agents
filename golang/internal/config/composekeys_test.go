package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// Every service that serves the read side is given the key it refuses to start
// without - in EVERY compose file, not the one somebody remembered.
//
// `PAGE_KEY` went into `compose.yaml` in seven places on 1 September and into
// `compose.prod.yaml` in none, because nobody looked for a second file. That file
// is the one deployments use: the change took the stand's page down on the address
// a judge opens, with the agent still trading behind it. A teammate found it by
// deploying. This is what should have found it.
func TestEveryPageInEveryComposeFileGetsTheKey(t *testing.T) {
	files, err := filepath.Glob("../../../compose*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no compose file found: the path this test reads has moved")

	// More than one, or this proves nothing about the case it exists for.
	require.Greater(t, len(files), 1,
		"one compose file: this guard is about the SECOND one, and it must know there is one")

	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)

			var file struct {
				Services map[string]struct {
					Environment map[string]string `yaml:"environment"`
				} `yaml:"services"`
			}
			require.NoError(t, yaml.Unmarshal(raw, &file))

			serving := 0
			for name, service := range file.Services {
				roles := service.Environment["ROLES"]
				if !strings.Contains(roles, "api") {
					continue
				}
				serving++
				assert.NotEmpty(t, service.Environment["PAGE_KEY"],
					"%s serves the read side and is given no PAGE_KEY: it will refuse to start", name)
			}
			assert.NotZero(t, serving, "no service here serves the read side")
		})
	}
}
