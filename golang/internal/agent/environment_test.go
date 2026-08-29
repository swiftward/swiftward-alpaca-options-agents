package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The process the model runs in is built by NAME, and what the harness holds
// besides is not in it.
//
// The harness carries the database's URL and the chat's bot token because the
// harness needs them. The model does not, and a session that read a poisoned
// headline and ran `env` would have them in its own context - from where they
// reach the record and the page a judge opens.
func TestTheAgentIsGivenOnlyWhatItNeeds(t *testing.T) {
	held := map[string]string{
		"PATH":                  "/usr/bin",
		"HOME":                  "/home/agent",
		"CODEX_HOME":            "/mnt/codex",
		"HTTPS_PROXY":           "http://egress:8888",
		"BROKER_MCP_TOKEN":      "the broker's",
		"GATEWAY_TOKEN":         "the gateway's",
		"USER_TOKEN":            "the person's",
		"DATABASE_URL":          "postgres://user:secret@postgres:5432/agents",
		"TELEGRAM_BOT_TOKEN":    "the chat's",
		"ALPACA_API_SECRET_KEY": "the account's",
	}
	lookup := func(name string) (string, bool) { value, set := held[name]; return value, set }

	got := Environment(nil, lookup)

	joined := strings.Join(got, "\n")
	for _, needed := range []string{
		"PATH=/usr/bin", "HOME=/home/agent", "CODEX_HOME=/mnt/codex",
		"HTTPS_PROXY=http://egress:8888",
		// Named by the session's own configuration - `bearer_token_env_var` and
		// `env_http_headers` in docker/agent-entrypoint.sh. Without these the
		// session is offered no tools at all.
		"BROKER_MCP_TOKEN=the broker's", "GATEWAY_TOKEN=the gateway's", "USER_TOKEN=the person's",
	} {
		assert.Contains(t, joined, needed)
	}
	for _, kept := range []string{"DATABASE_URL", "TELEGRAM_BOT_TOKEN", "ALPACA_API_SECRET_KEY"} {
		assert.NotContains(t, joined, kept, "%s is the harness's, not the model's", kept)
	}
}

// A name an operator adds is passed too: a server added to the session's
// configuration tomorrow brings its own credential, and that must not need a
// new image.
func TestAnOperatorCanAddANameWithoutARelease(t *testing.T) {
	held := map[string]string{"PATH": "/usr/bin", "NEW_SERVER_TOKEN": "its own"}
	lookup := func(name string) (string, bool) { value, set := held[name]; return value, set }

	got := Environment([]string{"NEW_SERVER_TOKEN"}, lookup)

	assert.Contains(t, strings.Join(got, "\n"), "NEW_SERVER_TOKEN=its own")
}

// A name that is not set is absent rather than empty. An empty credential is a
// claim to have one, and a server told to read an empty token fails differently
// from one told nothing.
func TestANameThatIsNotSetIsLeftOut(t *testing.T) {
	lookup := func(name string) (string, bool) {
		if name == "PATH" {
			return "/usr/bin", true
		}

		return "", false
	}

	got := Environment(nil, lookup)

	assert.Equal(t, []string{"PATH=/usr/bin"}, got)
}
