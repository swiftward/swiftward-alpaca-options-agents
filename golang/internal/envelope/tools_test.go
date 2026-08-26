package envelope

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const twoCallers = `
ruleset_version: "test.7"
agents:
  options-alpha:
    tools:
      place_option_order:
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 0.5
          unit: percent_of_equity
  options-alpha-near:
    tools:
      place_option_order:
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 2.5
          unit: percent_of_equity
`

// bearer puts a token on every request, the way the agent's own runtime does when
// it is told which environment variable holds one.
type bearer struct{ token string }

func (b bearer) RoundTrip(req *http.Request) (*http.Response, error) {
	if b.token != "" {
		req = req.Clone(req.Context())
		req.Header.Set("Authorization", "Bearer "+b.token)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// The client here is the SDK's own, talking to our server over the same transport
// the agent uses. Nothing about the protocol is hand-built.
func ask(t *testing.T, path, token string) *mcp.ClientSession {
	t.Helper()

	server := httptest.NewServer(Tools{
		Path:    path,
		Callers: map[string]string{"alpha-token": "options-alpha", "near-token": "options-alpha-near"},
	}.Handler())
	t.Cleanup(server.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:   server.URL,
		HTTPClient: &http.Client{Transport: bearer{token: token}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })

	return session
}

func ruleset(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "envelope.yaml")
	require.NoError(t, os.WriteFile(path, []byte(twoCallers), 0o600))
	return path
}

func envelopeOf(t *testing.T, session *mcp.ClientSession, tool string) Envelope {
	t.Helper()

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_envelope",
		Arguments: map[string]any{"tool": tool},
	})
	require.NoError(t, err)
	require.False(t, result.IsError, result.Content)

	var out Envelope
	require.NoError(t, json.Unmarshal(mustJSON(t, result.StructuredContent), &out))
	return out
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return raw
}

func TestOnlyTheEnvelopeToolIsServed(t *testing.T) {
	session := ask(t, ruleset(t), "alpha-token")

	var names []string
	for tool, err := range session.Tools(context.Background(), nil) {
		require.NoError(t, err)
		names = append(names, tool.Name)
	}
	// It answers what is allowed and does nothing else. Judging an order and
	// refusing it belong to the gateway, and a stand-in that grew them would be a
	// second engine nobody agreed to run.
	assert.Equal(t, []string{"read_envelope"}, names)
}

// Two accounts run the same engine under different ceilings, and which one you
// are is decided by the token you were started with - never by anything the
// session can say. This is the whole reason the limits are not in its text.
func TestTheTokenDecidesWhichLimitsApply(t *testing.T) {
	path := ruleset(t)

	alpha := envelopeOf(t, ask(t, path, "alpha-token"), "place_option_order")
	assert.Equal(t, "options-alpha", alpha.Identity)
	assert.Equal(t, "test.7", alpha.RulesetVersion)
	assert.True(t, alpha.Governed)
	require.Len(t, alpha.Constraints, 1)
	assert.Equal(t, 0.5, alpha.Constraints[0].Value)

	near := envelopeOf(t, ask(t, path, "near-token"), "place_option_order")
	assert.Equal(t, "options-alpha-near", near.Identity)
	assert.Equal(t, 2.5, near.Constraints[0].Value)
}

func TestAnUnknownTokenIsToldNothing(t *testing.T) {
	session := ask(t, ruleset(t), "someone-elses-token")

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_envelope",
		Arguments: map[string]any{"tool": "place_option_order"},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError, "a caller nobody recognises is refused, not answered empty")
}

// Lowering a ceiling is one edit to one file, and the session that is already
// running reads the new number on its next question. Restarting to change a limit
// would make the operator's move invisible, which is the move this exists to show.
func TestAnEditIsSeenWithoutRestarting(t *testing.T) {
	path := ruleset(t)
	session := ask(t, path, "alpha-token")

	assert.Equal(t, 0.5, envelopeOf(t, session, "place_option_order").Constraints[0].Value)

	lowered := []byte(`
ruleset_version: "test.8"
agents:
  options-alpha:
    tools:
      place_option_order:
        - rule: max-loss-per-position
          disclosure: boundary
          subject: position_max_loss
          kind: maximum
          value: 0.2
          unit: percent_of_equity
`)
	require.NoError(t, os.WriteFile(path, lowered, 0o600))

	after := envelopeOf(t, session, "place_option_order")
	assert.Equal(t, 0.2, after.Constraints[0].Value)
	assert.Equal(t, "test.8", after.RulesetVersion, "the version moves with the number, so a refusal can be matched to what was read")
}

func TestAskingWithoutNamingAToolIsRefused(t *testing.T) {
	session := ask(t, ruleset(t), "alpha-token")

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "read_envelope",
		Arguments: map[string]any{"tool": ""},
	})
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
