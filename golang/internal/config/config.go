// Package config reads the settings this binary needs. Nothing here holds a
// broker credential: the only outward path is the policy gateway, named by its
// address and a token.
package config

import (
	"fmt"
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

	// Harness role. Empty means the role cannot run: there is nothing to wake for.
	DeclarationPath string

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

	return Config{
		Roles:           roles,
		Addr:            k.String("addr"),
		WebDir:          k.String("web_dir"),
		MCPAddr:         k.String("mcp_addr"),
		DeclarationPath: k.String("declaration"),
		DatabaseURL:     k.String("database_url"),
		GatewayURL:      k.String("gateway_url"),
		GatewayToken:    k.String("gateway_token"),
		Telegram: telegram.Config{
			Token:   k.String("telegram_bot_token"),
			ChatID:  k.Int64("telegram_chat_id"),
			TopicID: k.Int("telegram_topic_id"),
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

func (c Config) Has(role Role) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}
