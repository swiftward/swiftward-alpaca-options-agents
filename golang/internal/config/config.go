// Package config reads the settings this binary needs. Nothing here holds a
// broker credential: the only outward path is the policy gateway, named by its
// address and a token.
package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
)

// Role is one of the three jobs this binary performs. Which ones run is chosen
// at start; nothing else in the process depends on the choice.
type Role string

const (
	RoleHarness Role = "harness"
	RoleAPI     Role = "api"
	RoleMCP     Role = "mcp"
)

type Config struct {
	Roles []Role

	// API role.
	Addr   string
	WebDir string

	// MCP role.
	MCPAddr string

	// Harness role. With no declaration and no chat there is nothing to wake for.
	DeclarationPath string
	// AgentCommand is the agent binary the harness starts.
	AgentCommand string
	// AgentDir and AgentSandbox are what every session is given to work in.
	AgentDir     string
	AgentSandbox string

	// Shared.
	DatabaseURL  string
	GatewayURL   string
	GatewayToken string

	// The chat the session posts to. Absent means the agent is offered no way to
	// post at all, rather than a tool that fails when called.
	Telegram telegram.Config
}

func Load() (Config, error) {
	k := koanf.New(".")
	if err := k.Load(env.Provider("", ".", strings.ToLower), nil); err != nil {
		return Config{}, fmt.Errorf("read environment: %w", err)
	}

	roles, err := parseRoles(k.String("roles"))
	if err != nil {
		return Config{}, err
	}

	allowUserIDs, err := parseUserIDs(k.String("telegram_allow_user_ids"))
	if err != nil {
		return Config{}, err
	}

	return Config{
		Roles:           roles,
		Addr:            k.String("addr"),
		WebDir:          k.String("web_dir"),
		MCPAddr:         k.String("mcp_addr"),
		DeclarationPath: k.String("declaration"),
		AgentCommand:    k.String("agent_command"),
		AgentDir:        k.String("agent_dir"),
		AgentSandbox:    k.String("agent_sandbox"),
		DatabaseURL:     k.String("database_url"),
		GatewayURL:      k.String("gateway_url"),
		GatewayToken:    k.String("gateway_token"),
		Telegram: telegram.Config{
			Token:        k.String("telegram_bot_token"),
			ChatID:       k.Int64("telegram_chat_id"),
			TopicID:      k.Int("telegram_topic_id"),
			AllowUserIDs: allowUserIDs,
		},
	}, nil
}

func parseRoles(raw string) ([]Role, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("ROLES is empty: name at least one of harness, api, mcp")
	}
	var out []Role
	for _, part := range strings.Split(raw, ",") {
		switch role := Role(strings.TrimSpace(part)); role {
		case RoleHarness, RoleAPI, RoleMCP:
			out = append(out, role)
		default:
			return nil, fmt.Errorf("unknown role %q: expected harness, api or mcp", part)
		}
	}
	return out, nil
}

// parseUserIDs reads the allowlist out of one environment string. The values
// arrive as text and stay text until they are numbers here: an id that does not
// parse REFUSES the whole configuration, because the alternative is a shorter
// allowlist than the operator wrote and no sign of it.
func parseUserIDs(raw string) ([]int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var out []int64
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("TELEGRAM_ALLOW_USER_IDS holds %q, which is not a user id", part)
		}
		out = append(out, id)
	}

	return out, nil
}

func (c Config) Has(role Role) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}
