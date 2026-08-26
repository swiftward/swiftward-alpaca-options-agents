package sessiontools

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

// The client is the SDK's own, talking to our server over the transport the
// agent uses; only the shortlist behind it is a double.

type shortlistDouble struct {
	found   []screener.Candidate
	takenAt time.Time
}

func (s *shortlistDouble) Candidates(context.Context, int) ([]screener.Candidate, time.Time, error) {
	return s.found, s.takenAt, nil
}

// The list says how old it is.
//
// Rows outlive the sweep that wrote them: if the screener stops, the table keeps
// its last answer for as long as the process lives, and an hour-old list reads
// exactly like a minute-old one. Seven minutes was already enough to turn +7.5
// points of edge into -7.2 on 26 August, so a reader that cannot see the age
// cannot judge what it is holding.
func TestTheCandidateListSaysHowOldItIs(t *testing.T) {
	now := time.Date(2026, 8, 26, 19, 10, 0, 0, time.UTC)
	shortlist := &shortlistDouble{
		found:   []screener.Candidate{{Underlying: "QQQ", Type: "put"}},
		takenAt: now.Add(-7 * time.Minute),
	}

	session := candidateSession(t, shortlist, now)
	out, err := session.CallTool(context.Background(),
		&mcp.CallToolParams{Name: "read_candidates", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, out.IsError)

	answer := readCandidates(t, out)
	assert.Equal(t, float64(420), answer["seconds_old"], "seven minutes, and the session is told so")
	assert.Len(t, answer["candidates"], 1)
}

// An empty list has no age: there is no sweep to date, and reporting zero
// seconds would read as "just now".
func TestAnEmptyCandidateListHasNoAge(t *testing.T) {
	now := time.Date(2026, 8, 26, 19, 10, 0, 0, time.UTC)
	session := candidateSession(t, &shortlistDouble{}, now)

	out, err := session.CallTool(context.Background(),
		&mcp.CallToolParams{Name: "read_candidates", Arguments: map[string]any{}})
	require.NoError(t, err)

	answer := readCandidates(t, out)
	assert.Empty(t, answer["candidates"])
	assert.Equal(t, float64(0), answer["seconds_old"])
}

func candidateSession(t *testing.T, shortlist Shortlist, now time.Time) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{
		Record:    record.NewMemory(),
		Shortlist: shortlist,
		Now:       func() time.Time { return now },
		Running:   &runningDouble{},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(),
		&mcp.StreamableClientTransport{Endpoint: server.URL}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func readCandidates(t *testing.T, out *mcp.CallToolResult) map[string]any {
	t.Helper()

	raw, err := json.Marshal(out.StructuredContent)
	require.NoError(t, err)
	var answer map[string]any
	require.NoError(t, json.Unmarshal(raw, &answer))

	return answer
}
