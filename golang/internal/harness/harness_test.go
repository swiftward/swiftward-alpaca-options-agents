package harness

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestRunRefusesWithoutDeclaration(t *testing.T) {
	err := Run(context.Background(), "", zaptest.NewLogger(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DECLARATION")
}

func TestRunRefusesUnreadableDeclaration(t *testing.T) {
	err := Run(context.Background(), filepath.Join(t.TempDir(), "absent.yaml"), zaptest.NewLogger(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read declaration")
}

// A declaration that exists still cannot schedule anything: the format is not
// implemented, and this asserts the refusal is explicit rather than an idle loop.
func TestRunRefusesUnimplementedFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(path, []byte("kind: trading-agent\n"), 0o600))

	err := Run(context.Background(), path, zaptest.NewLogger(t))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}
