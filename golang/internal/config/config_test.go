package config

import (
	"testing"

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
