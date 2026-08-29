package sessiontools

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/placement"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

type scorerDouble struct {
	asked  placement.Ask
	answer placement.Answer
	fail   error
}

func (s *scorerDouble) Score(_ context.Context, ask placement.Ask) (placement.Answer, error) {
	s.asked = ask
	if s.fail != nil {
		return placement.Answer{}, s.fail
	}

	return s.answer, nil
}

func withScorer(t *testing.T, scorer Placements) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{
		Record: record.NewMemory(), Now: time.Now, Running: &runningDouble{}, Placements: scorer,
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func call(t *testing.T, session *mcp.ClientSession, with map[string]any) *mcp.CallToolResult {
	t.Helper()

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "score_placements", Arguments: with,
	})
	require.NoError(t, err)

	return res
}

func good() map[string]any {
	return map[string]any{
		"underlying": "spy", "expiration": "2026-09-04", "kind": "CALL",
		"short_least_sigma": 1.5, "valley_least_sigma": 2.5, "worst_case_most": 2000.0,
	}
}

// The limits belong to the declaration. If the tool carried its own defaults the
// same number would live in two places, and the day they differ nobody could say
// which one the trade was made under.
func TestTheLimitsMustComeFromTheCaller(t *testing.T) {
	for name, drop := range map[string]string{
		"no distance for the sold leg": "short_least_sigma",
		"no distance for the valley":   "valley_least_sigma",
	} {
		t.Run(name, func(t *testing.T) {
			scorer := &scorerDouble{}
			with := good()
			delete(with, drop)

			res := call(t, withScorer(t, scorer), with)
			require.True(t, res.IsError, "a missing limit is refused, not filled in")
			assert.Empty(t, scorer.asked.Underlying, "and the scorer is never reached")
		})
	}
}

func TestItPassesTheAskThroughAsGiven(t *testing.T) {
	scorer := &scorerDouble{answer: placement.Answer{Underlying: "SPY", Windows: 466}}
	with := good()
	with["short_most_sigma"] = 3.5
	with["bought"] = 3
	with["most"] = 2

	res := call(t, withScorer(t, scorer), with)
	require.False(t, res.IsError)

	assert.Equal(t, "SPY", scorer.asked.Underlying, "the symbol is upper-cased for the broker")
	assert.Equal(t, "call", scorer.asked.Kind, "and the kind is lower-cased")
	assert.Equal(t, time.Date(2026, 9, 4, 0, 0, 0, 0, time.UTC), scorer.asked.Expiration)
	assert.InDelta(t, 1.5, scorer.asked.ShortLeastSigma, 1e-9)
	assert.InDelta(t, 2.5, scorer.asked.ValleyLeastSigma, 1e-9)
	assert.InDelta(t, 3.5, scorer.asked.ShortMostSigma, 1e-9)
	assert.InDelta(t, 2000, scorer.asked.WorstCaseMost, 1e-9)
	assert.Equal(t, 3, scorer.asked.Bought)
	assert.Equal(t, 2, scorer.asked.Most)

	var answered placement.Answer
	require.NoError(t, json.Unmarshal(mustJSON(t, res.StructuredContent), &answered))
	assert.Equal(t, 466, answered.Windows)
}

// Two is the backspread, and five rows is an answer a session can read. Neither
// is a limit, so neither belongs in the declaration.
func TestTheShapeOfTheQuestionHasDefaults(t *testing.T) {
	scorer := &scorerDouble{}
	res := call(t, withScorer(t, scorer), good())
	require.False(t, res.IsError)

	assert.Equal(t, 2, scorer.asked.Bought)
	assert.Equal(t, 5, scorer.asked.Most)
}

func TestADateThatIsNotADateIsRefused(t *testing.T) {
	scorer := &scorerDouble{}
	with := good()
	with["expiration"] = "next friday"

	res := call(t, withScorer(t, scorer), with)
	require.True(t, res.IsError)
	assert.Empty(t, scorer.asked.Underlying)
}

// What the scorer could not answer reaches the session as the scorer's own
// words. A session told "it did not work" cannot tell a broken chain from a
// window it should have asked differently about.
func TestTheScorersRefusalReachesTheSession(t *testing.T) {
	scorer := &scorerDouble{fail: errors.New("too few windows in weather like today's")}

	res := call(t, withScorer(t, scorer), good())
	require.True(t, res.IsError)
	require.NotEmpty(t, res.Content)
	text, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "too few windows")
}

// A deployment with no broker offers no scoring: a tool a session can see is a
// tool it assumes will answer.
func TestWithoutAMarketTheToolIsNotOffered(t *testing.T) {
	session := connect(t, record.NewMemory(), time.Now)

	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		assert.NotEqual(t, "score_placements", tool.Name)
	}
}

func TestWithAMarketTheToolIsOffered(t *testing.T) {
	session := withScorer(t, &scorerDouble{})

	tools, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "score_placements" {
			found = true
		}
	}
	assert.True(t, found)
}
