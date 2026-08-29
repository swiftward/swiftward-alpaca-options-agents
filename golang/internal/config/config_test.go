package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoles(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []Role
		wantErr string
	}{
		{name: "all_three", raw: "harness,api,mcp", want: []Role{RoleHarness, RoleAPI, RoleMCP}},
		{name: "spaces_are_trimmed", raw: " api , mcp ", want: []Role{RoleAPI, RoleMCP}},
		{name: "single", raw: "api", want: []Role{RoleAPI}},
		{name: "empty_refuses", raw: "   ", wantErr: "ROLES is empty"},
		{name: "unknown_refuses", raw: "api,trader", wantErr: `unknown role "trader"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRoles(tc.raw)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestHas(t *testing.T) {
	cfg := Config{Roles: []Role{RoleAPI, RoleMCP}}
	assert.True(t, cfg.Has(RoleAPI))
	assert.True(t, cfg.Has(RoleMCP))
	assert.False(t, cfg.Has(RoleHarness))
}

// The allowlist arrives as one environment string. If it is not split into ids,
// every message is dropped and the channel looks broken rather than closed.
func TestAllowedUserIDsAreReadFromOneEnvString(t *testing.T) {
	t.Setenv("ROLES", "harness")
	t.Setenv("AGENT_CALL_TIMEOUT", "30s")
	t.Setenv("TELEGRAM_BOT_TOKEN", "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	t.Setenv("TELEGRAM_CHAT_ID", "-1003770330300")
	t.Setenv("TELEGRAM_TOPIC_ID", "7543")
	t.Setenv("TELEGRAM_ALLOW_USER_IDS", "3813730,298310358")

	cfg, err := Load()
	require.NoError(t, err)

	assert.EqualValues(t, -1003770330300, cfg.Telegram.ChatID)
	assert.Equal(t, 7543, cfg.Telegram.TopicID)
	assert.Equal(t, []int64{3813730, 298310358}, cfg.Telegram.AllowUserIDs)
}

func TestCallTimeoutIsRequiredByTheHarness(t *testing.T) {
	t.Setenv("ROLES", "harness")
	t.Setenv("AGENT_CALL_TIMEOUT", "")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AGENT_CALL_TIMEOUT")
}

func TestCallTimeoutIsNotRequiredWithoutTheHarness(t *testing.T) {
	t.Setenv("ROLES", "api,mcp")
	t.Setenv("AGENT_CALL_TIMEOUT", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Zero(t, cfg.AgentCallTimeout)
}

func TestARefusedCallTimeoutNamesItself(t *testing.T) {
	t.Setenv("ROLES", "harness")
	t.Setenv("AGENT_CALL_TIMEOUT", "half a minute")

	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a duration")
}

// Every field the process acts on is read from the environment. A field declared
// and never filled is the quietest defect there is: the feature simply is not
// offered, and nothing says why.
func TestEveryDeclaredSettingIsRead(t *testing.T) {
	t.Setenv("ROLES", "harness,api,mcp")
	t.Setenv("AGENT_CALL_TIMEOUT", "90s")
	t.Setenv("ADDR", ":8080")
	t.Setenv("MCP_ADDR", ":8081")
	t.Setenv("WEB_DIR", "/srv/web")
	t.Setenv("DECLARATION", "/agent/agent.yaml")
	t.Setenv("AGENT_COMMAND", "codex")
	t.Setenv("AGENT_DIR", "/work")
	t.Setenv("AGENT_SANDBOX", "danger-full-access")
	t.Setenv("AGENT_MODEL", "gpt-5.6-luna")
	t.Setenv("THREAD_FILE", "/work/.thread")
	t.Setenv("WAKEUP_FILE", "/work/wakeups.json")
	t.Setenv("BROKER_MCP_URL", "http://alpaca-mcp:8000/mcp")
	t.Setenv("DATABASE_URL", "postgres://agents@postgres/agents")
	t.Setenv("GATEWAY_URL", "https://gateway.example/mcp")
	t.Setenv("GATEWAY_TOKEN", "secret")
	t.Setenv("RECORD_SHOWS", "25")
	t.Setenv("VOLATILITY_UNDERLYINGS", "spy, avgo")
	t.Setenv("VOLATILITY_EVERY", "5m")
	t.Setenv("ENVELOPE_ADDR", ":8090")
	t.Setenv("ENVELOPE_PATH", "/agent/envelope.yaml")
	t.Setenv("ENVELOPE_CALLERS", "alpha-token=alpaca-agent-1, near-token=alpaca-agent-2")

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.Addr)
	assert.Equal(t, ":8081", cfg.MCPAddr)
	assert.Equal(t, "/srv/web", cfg.WebDir)
	assert.Equal(t, "/agent/agent.yaml", cfg.DeclarationPath)
	assert.Equal(t, "codex", cfg.AgentCommand)
	assert.Equal(t, "/work", cfg.AgentDir)
	assert.Equal(t, "danger-full-access", cfg.AgentSandbox)
	assert.Equal(t, "gpt-5.6-luna", cfg.AgentModel)
	assert.Equal(t, "/work/.thread", cfg.ThreadFile)
	assert.Equal(t, "/work/wakeups.json", cfg.WakeupFile)
	assert.Equal(t, "http://alpaca-mcp:8000/mcp", cfg.BrokerMCPURL)
	assert.Equal(t, "postgres://agents@postgres/agents", cfg.DatabaseURL)
	assert.Equal(t, "https://gateway.example/mcp", cfg.GatewayURL)
	assert.Equal(t, "secret", cfg.GatewayToken)
	assert.Equal(t, 90*time.Second, cfg.AgentCallTimeout)
	assert.Equal(t, 25, cfg.RecordShows)
	assert.Equal(t, []string{"SPY", "AVGO"}, cfg.VolatilityUnderlyings)
	assert.Equal(t, 5*time.Minute, cfg.VolatilityEvery)
	assert.Equal(t, ":8090", cfg.EnvelopeAddr)
	assert.Equal(t, "/agent/envelope.yaml", cfg.EnvelopePath)
	assert.Equal(t, map[string]string{
		"alpha-token": "alpaca-agent-1",
		"near-token":  "alpaca-agent-2",
	}, cfg.EnvelopeCallers)
}

// Two agents under one token are one agent as far as the rules are concerned,
// and neither of them would ever notice.
func TestOneTokenCannotBelongToTwoCallers(t *testing.T) {
	_, err := parseCallers("shared=alpaca-agent-1,shared=alpaca-agent-2", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one token is given to both")
}

// The envelope role recognising nobody answers nobody. That is a stack started
// wrong, and it says so at boot rather than at the first entry window.
func TestTheEnvelopeRoleNeedsSomeoneToRecognise(t *testing.T) {
	_, err := parseCallers("", []Role{RoleEnvelope})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "would recognise nobody")

	callers, err := parseCallers("", []Role{RoleHarness})
	require.NoError(t, err, "a process that serves no envelope needs no callers")
	assert.Empty(t, callers)
}

// A deployment that records volatility says how often. Choosing an interval here
// would silently outrank the one an operator wrote.
func TestRecordingVolatilityWithoutAnIntervalIsRefused(t *testing.T) {
	t.Setenv("ROLES", "harness")
	t.Setenv("AGENT_CALL_TIMEOUT", "90s")
	t.Setenv("VOLATILITY_UNDERLYINGS", "SPY")
	t.Setenv("VOLATILITY_EVERY", "")

	_, err := Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "VOLATILITY_EVERY")
}

func TestWatchingNothingNeedsNoInterval(t *testing.T) {
	t.Setenv("ROLES", "api")
	t.Setenv("AGENT_CALL_TIMEOUT", "")
	t.Setenv("VOLATILITY_UNDERLYINGS", "")

	cfg, err := Load()

	require.NoError(t, err)
	assert.Empty(t, cfg.VolatilityUnderlyings)
	assert.Zero(t, cfg.VolatilityEvery)
}

// Without the setting the page still has to carry something: an unset knob that
// means zero would serve an empty record and look like an agent that never ran.
func TestTheRecordShowsSomethingByDefault(t *testing.T) {
	t.Setenv("ROLES", "api")
	t.Setenv("AGENT_CALL_TIMEOUT", "")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, defaultRecordShows, cfg.RecordShows)
}

// A driver names how the agent is reached, and a mailbox that could not be
// reached is a failure to start rather than a harness that looks busy.
//
// The failure it prevents is the quiet one: with no address or no token the
// clock still runs, sessions still wake on time, and every one of them is parked
// where nobody can ever take it. That is indistinguishable from a day on which
// nothing happened.
func TestParseDriver(t *testing.T) {
	harnessOnly := []Role{RoleHarness}
	readOnly := []Role{RoleAPI, RoleMCP}

	cases := []struct {
		name    string
		raw     string
		roles   []Role
		addr    string
		token   string
		want    Driver
		wantErr string
	}{
		{name: "empty_is_how_this_behaved_before", raw: "", roles: harnessOnly, want: DriverCodex},
		{name: "codex_named", raw: "codex", roles: harnessOnly, want: DriverCodex},
		{
			name: "mailbox_with_both", raw: "mailbox", roles: harnessOnly,
			addr: ":8090", token: "t", want: DriverMailbox,
		},
		{
			name: "mailbox_without_an_address_refuses", raw: "mailbox", roles: harnessOnly,
			token: "t", wantErr: "MAILBOX_ADDR",
		},
		{
			name: "mailbox_without_a_token_refuses", raw: "mailbox", roles: harnessOnly,
			addr: ":8090", wantErr: "MAILBOX_TOKEN",
		},
		{
			name: "mailbox_naming_neither_says_so_once", raw: "mailbox", roles: harnessOnly,
			wantErr: "MAILBOX_ADDR and MAILBOX_TOKEN",
		},
		{
			// A process with no clock drives nobody, and asking it for a mailbox
			// address would refuse a deployment that is entirely correct.
			name: "no_harness_needs_nothing", raw: "mailbox", roles: readOnly, want: DriverMailbox,
		},
		{name: "unknown_refuses", raw: "acp", roles: harnessOnly, wantErr: `unknown AGENT_DRIVER "acp"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseDriver(tc.raw, tc.roles, tc.addr, tc.token)
			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// One broker address, and it is the gateway. An earlier build carried a second -
// `HarnessBrokerMCPURL` - so the harness could point its reads straight at the
// broker; a read that skips the gateway is a read no rule saw and no record
// holds. Counting the fields is what makes this test able to FAIL: adding any
// second broker address back turns it red, which asserting on the surviving one
// would not.
func TestConfigCarriesExactlyOneBrokerAddress(t *testing.T) {
	var found []string
	fields := reflect.TypeOf(Config{})
	for i := range fields.NumField() {
		name := fields.Field(i).Name
		if strings.Contains(name, "Broker") && strings.HasSuffix(name, "URL") {
			found = append(found, name)
		}
	}
	assert.Equal(t, []string{"BrokerMCPURL"}, found,
		"every call goes to the gateway; a second broker address is how reads leave the record")
}

// Zero is not "no delay", it is "leave it as it was", and the operator has to
// learn the difference at startup rather than from a log a day later. An empty
// value is legal; a written zero is not.
func TestAnIntervalWrittenAsZeroIsRefusedRatherThanTakenAsUnset(t *testing.T) {
	for _, raw := range []string{"0", "0s", "-10s"} {
		_, err := parseDuration("TICK_EVERY", raw)
		assert.Error(t, err, "TICK_EVERY=%q has to be refused", raw)
	}

	every, err := parseDuration("TICK_EVERY", "")
	assert.NoError(t, err)
	assert.Zero(t, every, "empty means unset, and that is legal")

	every, err = parseDuration("TICK_EVERY", "10s")
	assert.NoError(t, err)
	assert.Equal(t, 10*time.Second, every)
}
