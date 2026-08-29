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

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/envelope"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

const ruleset = `
ruleset_version: "test-1"
agents:
  options-alpha:
    tools:
      place_option_order:
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 15.0
          unit: percent_of_equity
`

// The page serves the limits through the SAME call that answers the agent.
//
// This is worth a test rather than trust: limits arriving by discovery are this
// project's central claim, and if the page shows a retelling of them, one day the
// retelling will differ from what is actually traded. We would then be showing a
// judge a story about the system rather than the system.
func TestThePageShowsTheSameLimitsTheAgentIsGiven(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envelope.yaml")
	require.NoError(t, os.WriteFile(path, []byte(ruleset), 0o600))

	handler := serving(t, Read{
		Record: record.NewMemory(), EnvelopePath: path, EnvelopeIdentity: "options-alpha",
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	var shown envelope.Envelope
	require.NoError(t, json.Unmarshal(answer.Body.Bytes(), &shown))

	// The same thing, taken straight from the envelope - letter for letter.
	set, err := envelope.Load(path)
	require.NoError(t, err)
	given, err := set.For("options-alpha", "place_option_order")
	require.NoError(t, err)

	assert.Equal(t, given, shown, "the page and the agent must see the same thing")
	assert.Equal(t, "test-1", shown.RulesetVersion, "the ruleset version is named: a refusal is checked against it")
}

// Without an envelope the page says so plainly rather than serving emptiness a
// reader would take for "there are no limits".
func TestWithoutAnEnvelopeThePageSaysSo(t *testing.T) {
	handler := serving(t, Read{Record: record.NewMemory()})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	// 501, not 503: this is not "temporarily unavailable" but "this deployment has
	// no envelope". The difference matters to the reader - they would keep
	// refreshing on the second and not on the first.
	assert.Equal(t, http.StatusNotImplemented, answer.Code)
}

type sweepDouble struct {
	found   []screener.Candidate
	takenAt time.Time
}

func (s sweepDouble) Candidates(context.Context, int) ([]screener.Candidate, time.Time, error) {
	return s.found, s.takenAt, nil
}

// A pass carries its time. Rows outlive the pass that wrote them, so an
// hour-old list reads as a minute old until it says which it is.
func TestTheSweepSaysWhenItWasTaken(t *testing.T) {
	taken := time.Date(2026, 8, 28, 13, 40, 0, 0, time.UTC)
	handler := serving(t, Read{
		Record: record.NewMemory(), OrdersShown: 10,
		Sweep: sweepDouble{found: []screener.Candidate{{Underlying: "SPY"}}, takenAt: taken},
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/sweep", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	var shown struct {
		Candidates []screener.Candidate `json:"candidates"`
		TakenAt    time.Time            `json:"taken_at"`
	}
	require.NoError(t, json.Unmarshal(answer.Body.Bytes(), &shown))
	assert.Len(t, shown.Candidates, 1)
	assert.Equal(t, taken, shown.TakenAt)
}

func serving(t *testing.T, read Read) http.Handler {
	t.Helper()

	read.Log = zaptest.NewLogger(t)
	handler, err := read.Handler()
	require.NoError(t, err)

	return handler
}

// A page route is served by the page, not by a refusal.
//
// /live is not a file, it is a state of the page: routes live in the browser. A
// file server answers such a path with 404, and a visitor who opened a link or
// simply refreshed the tab sees an error instead of what they opened. This has to
// be tested here, because otherwise it breaks silently and is discovered by
// whoever followed the link.
func TestAPageRouteIsAnsweredByThePage(t *testing.T) {
	web := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(web, "index.html"), []byte("<!doctype html>the page"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(web, "app.js"), []byte("// a real file"), 0o600))

	handler := serving(t, Read{Record: record.NewMemory(), WebDir: web})

	for _, route := range []string{"/", "/live", "/whatever/deep"} {
		answer := httptest.NewRecorder()
		handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, route, nil))
		require.Equal(t, http.StatusOK, answer.Code, route)
		assert.Contains(t, answer.Body.String(), "the page", "%s must serve the page", route)
	}

	// A real file stays itself: replacing it with the page would break the build.
	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/app.js", nil))
	assert.Contains(t, answer.Body.String(), "a real file")

	// Data under /api is outside this rule: a refusal there must stay a refusal
	// rather than turn into the page with a 200.
	data := httptest.NewRecorder()
	handler.ServeHTTP(data, httptest.NewRequest(http.MethodGet, "/api/limits", nil))
	assert.Equal(t, http.StatusNotImplemented, data.Code)
}

// An empty pass is served as an empty LIST, not as null.
//
// This is not pedantry: on 28 August the live /live showed a white screen on
// exactly this. No pass had run on the server yet, the handler returned
// `candidates: null`, the page took `.length` on it and died. A reader handed null
// also cannot tell "no pass has run yet" from "this field is broken".
func TestAnEmptySweepIsAnEmptyListNotNull(t *testing.T) {
	handler := serving(t, Read{
		Record: record.NewMemory(), OrdersShown: 10,
		Sweep: sweepDouble{found: nil, takenAt: time.Time{}},
	})

	answer := httptest.NewRecorder()
	handler.ServeHTTP(answer, httptest.NewRequest(http.MethodGet, "/api/sweep", nil))
	require.Equal(t, http.StatusOK, answer.Code)

	assert.Contains(t, answer.Body.String(), `"candidates":[]`,
		"an empty list, not null: the page dies outright on null")
}
