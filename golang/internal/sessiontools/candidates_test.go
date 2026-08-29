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

// A list a few minutes old is FRESH, because the sweeps come at an interval and
// a pass takes time of its own. This is the case that cost a trade on 27 August:
// the entry window read the list twice, saw the age go from 280 seconds to 309,
// read the rise as the screener having stopped, and sent nothing. Age rises
// between two reads inside one interval by design.
func TestAListOlderThanOneSweepIsStillFresh(t *testing.T) {
	now := time.Date(2026, 8, 27, 16, 30, 0, 0, time.UTC)
	shortlist := &shortlistDouble{
		found:   []screener.Candidate{{Underlying: "QQQ", Type: "call"}},
		takenAt: now.Add(-309 * time.Second),
	}

	session := candidateSessionEvery(t, shortlist, now, 5*time.Minute)
	out, err := session.CallTool(context.Background(),
		&mcp.CallToolParams{Name: "read_candidates", Arguments: map[string]any{}})
	require.NoError(t, err)

	answer := readCandidates(t, out)
	assert.Equal(t, float64(309), answer["seconds_old"])
	assert.Equal(t, true, answer["fresh"],
		"309 seconds against a five-minute interval is one ordinary cycle, not a stopped screener")
}

// A screener that has actually stopped shows up as an age that keeps climbing
// past any interval, and THAT is what must read as no list at all.
func TestAListTheScreenerStoppedRefreshingIsNotFresh(t *testing.T) {
	now := time.Date(2026, 8, 27, 16, 30, 0, 0, time.UTC)
	shortlist := &shortlistDouble{
		found:   []screener.Candidate{{Underlying: "QQQ", Type: "call"}},
		takenAt: now.Add(-40 * time.Minute),
	}

	session := candidateSessionEvery(t, shortlist, now, 5*time.Minute)
	out, err := session.CallTool(context.Background(),
		&mcp.CallToolParams{Name: "read_candidates", Arguments: map[string]any{}})
	require.NoError(t, err)

	answer := readCandidates(t, out)
	assert.Equal(t, false, answer["fresh"], "forty minutes is eight intervals, and that is a stopped screener")
}

func candidateSession(t *testing.T, shortlist Shortlist, now time.Time) *mcp.ClientSession {
	t.Helper()

	return candidateSessionEvery(t, shortlist, now, 0)
}

func candidateSessionEvery(t *testing.T, shortlist Shortlist, now time.Time, every time.Duration) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{
		Record:     record.NewMemory(),
		Shortlist:  shortlist,
		SweepEvery: every,
		Now:        func() time.Time { return now },
		Running:    &runningDouble{},
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
