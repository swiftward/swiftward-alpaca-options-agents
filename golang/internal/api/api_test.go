package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

func TestHealthAndState(t *testing.T) {
	state := record.NewMemory()
	require.NoError(t, state.AppendIntent(context.Background(), record.Intent{
		At:        time.Date(2026, 9, 2, 19, 30, 0, 0, time.UTC),
		Session:   "event",
		Thesis:    "implied volatility collapses after the report",
		Structure: "four legs on AVGO",
		MaxLoss:   "1% of capital",
	}))

	handler, err := Handler(state, "", zaptest.NewLogger(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/state", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var got record.State
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got.Intents, 1)
	assert.Equal(t, "event", got.Intents[0].Session)
	assert.Equal(t, "1% of capital", got.Intents[0].MaxLoss)
}

func TestServesBuiltPage(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>limits</h1>"), 0o600))

	handler, err := Handler(record.NewMemory(), dir, zaptest.NewLogger(t))
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "limits")
}

// A configured directory that does not exist is a broken deployment, and saying
// so at start beats serving 404s that look like an empty page.
func TestMissingWebDirRefuses(t *testing.T) {
	_, err := Handler(record.NewMemory(), filepath.Join(t.TempDir(), "absent"), zaptest.NewLogger(t))
	require.Error(t, err)
}
