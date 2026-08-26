// Package config reads the settings this binary needs. Nothing here holds a
// broker credential: the only outward path is the policy gateway, named by its
// address and a token.
package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

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
	// RoleEnvelope stands in for the policy gateway while the gateway is not yet
	// in front of the broker: it serves the envelope and nothing else.
	RoleEnvelope Role = "envelope"
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
	// AgentDir and AgentSandbox are what every session is given to work in;
	// AgentModel overrides the agent's own configured model.
	AgentDir     string
	AgentSandbox string
	AgentModel   string
	// ThreadFile is where the conversation's identifier is kept between runs.
	// Empty means a restart begins a new conversation.
	ThreadFile string
	// WakeupFile is where the session's standing wake-ups are kept between runs.
	// Empty means the session is offered no way to ask for one.
	WakeupFile string
	// BrokerMCPURL is the broker's own server, read by the harness only to know
	// when a price wake-up has come true.
	BrokerMCPURL string
	// Execution walks an unfilled structure toward the price the book shows. Zero
	// interval means the ladder does not run and an order rests where the session
	// placed it.
	ExecutionEvery    time.Duration
	ExecutionStep     float64
	ExecutionPatience time.Duration
	// AccountEvery is how often the account's value is written down. Zero means
	// no history is kept here.
	AccountEvery time.Duration
	// OrdersShown bounds the order list the page carries; HistoryDays is how far
	// back its equity line is drawn.
	OrdersShown int
	HistoryDays int
	// VolatilityUnderlyings are the symbols whose option volatility is recorded
	// all day. Empty means the history is not kept on this deployment.
	VolatilityUnderlyings []string
	// VolatilityEvery is how often a reading is taken.
	VolatilityEvery time.Duration
	// SayEvery is the pause between what the room hears from one session. Empty
	// means everything is posted, which a busy day turns into a rate limit.
	SayEvery time.Duration
	// ScreenerUnderlyings is what the screener prices, over and over. Empty means
	// no screener runs.
	ScreenerUnderlyings []string
	// ScreenerEvery is how often the whole universe is swept.
	ScreenerEvery time.Duration
	// ScreenerPerMinute is the broker's limit on requests, which the sweep never
	// exceeds. The free plan allows 200.
	ScreenerPerMinute int
	// ScreenerNearest and ScreenerFurthest bound how far the sold strike may sit
	// from the price, in percent.
	ScreenerNearest, ScreenerFurthest float64
	// ScreenerLeastPaid is the least a listed structure may pay, credit against
	// risk, in percent.
	ScreenerLeastPaid float64
	// ScreenerMostPaid is where paying too much stops being an opportunity and
	// starts being a broken quote.
	ScreenerMostPaid float64
	// ScreenerMostDelta is how likely the sold strike may be to finish in the
	// money. Distance in percent is a different measure and not a substitute.
	ScreenerMostDelta float64
	// ScreenerLeastEdge is the least a structure may pay above what it must
	// survive, in percentage points.
	ScreenerLeastEdge float64
	// ScreenerDearest is the most the round trip may cost, as a percent of the
	// credit. This is the number that separates what earns from what loses.
	ScreenerDearest float64
	// ScreenerExpirations bounds how far out to look, in days.
	ScreenerExpirations int
	// ThreadResumeLimit bounds the one request that resumes an earlier
	// conversation. The harness wakes nobody until it returns, and a fresh thread
	// is a cheap fallback, so this is far shorter than AgentCallTimeout.
	ThreadResumeLimit time.Duration
	// TurnLimit bounds how long one turn may run before the harness interrupts it.
	// Empty means a turn runs until the agent ends it.
	TurnLimit time.Duration
	// AgentCallTimeout bounds one request to the agent. Without it a hung agent
	// takes the chat down with it: the loop that reads messages is the same loop
	// that talks to the agent.
	AgentCallTimeout time.Duration

	// Shared.
	DatabaseURL string
	// RecordShows is how many turns, calls and intents the page carries. The
	// record is a week long by the end; a page is read in one screen.
	RecordShows  int
	GatewayURL   string
	GatewayToken string

	// Envelope role. The limits in force and who is under them. Both are read
	// from outside the binary: an agent whose limits are compiled into the thing
	// that reads them has not discovered anything.
	EnvelopeAddr    string
	EnvelopePath    string
	EnvelopeCallers map[string]string

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

	callTimeout, err := parseTimeout(k.String("agent_call_timeout"), roles)
	if err != nil {
		return Config{}, err
	}

	underlyings := parseSymbols(k.String("volatility_underlyings"))

	callers, err := parseCallers(k.String("envelope_callers"), roles)
	if err != nil {
		return Config{}, err
	}

	every, err := parseEvery(k.String("volatility_every"), underlyings)
	if err != nil {
		return Config{}, err
	}

	screened := parseSymbols(k.String("screener_underlyings"))
	screenEvery, err := parseDuration("SCREENER_EVERY", k.String("screener_every"))
	if err != nil {
		return Config{}, err
	}
	if len(screened) > 0 && screenEvery <= 0 {
		return Config{}, fmt.Errorf("SCREENER_UNDERLYINGS is set but SCREENER_EVERY is not: a sweep with no interval never runs")
	}

	resumeLimit, err := parseDuration("THREAD_RESUME_LIMIT", k.String("thread_resume_limit"))
	if err != nil {
		return Config{}, err
	}
	turnLimit, err := parseTurnLimit(k.String("turn_limit"))
	if err != nil {
		return Config{}, err
	}

	sayEvery, err := parseTurnLimit(k.String("say_every"))
	if err != nil {
		return Config{}, fmt.Errorf("SAY_EVERY is not a duration: %w", err)
	}

	shows := k.Int("record_shows")
	if shows <= 0 {
		shows = defaultRecordShows
	}

	accountEvery, err := parseAccountEvery(k.String("account_every"))
	if err != nil {
		return Config{}, err
	}

	executionEvery, executionPatience, err := parseExecution(
		k.String("execution_every"), k.String("execution_patience"))
	if err != nil {
		return Config{}, err
	}

	ordersShown := k.Int("orders_shown")
	if ordersShown <= 0 {
		ordersShown = defaultOrdersShown
	}

	historyDays := k.Int("history_days")
	if historyDays <= 0 {
		historyDays = defaultHistoryDays
	}

	return Config{
		Roles:                 roles,
		VolatilityUnderlyings: underlyings,
		VolatilityEvery:       every,
		Addr:                  k.String("addr"),
		WebDir:                k.String("web_dir"),
		MCPAddr:               k.String("mcp_addr"),
		DeclarationPath:       k.String("declaration"),
		AgentCommand:          k.String("agent_command"),
		AgentDir:              k.String("agent_dir"),
		AgentSandbox:          k.String("agent_sandbox"),
		AgentModel:            k.String("agent_model"),
		ThreadFile:            k.String("thread_file"),
		WakeupFile:            k.String("wakeup_file"),
		BrokerMCPURL:          k.String("broker_mcp_url"),
		AgentCallTimeout:      callTimeout,
		ScreenerUnderlyings:   screened,
		ScreenerEvery:         screenEvery,
		ScreenerPerMinute:     k.Int("screener_per_minute"),
		ScreenerNearest:       k.Float64("screener_nearest"),
		ScreenerFurthest:      k.Float64("screener_furthest"),
		ScreenerLeastPaid:     k.Float64("screener_least_paid"),
		ScreenerMostPaid:      k.Float64("screener_most_paid"),
		ScreenerMostDelta:     k.Float64("screener_most_delta"),
		ScreenerLeastEdge:     k.Float64("screener_least_edge"),
		ScreenerDearest:       k.Float64("screener_dearest"),
		ScreenerExpirations:   k.Int("screener_expirations"),
		ThreadResumeLimit:     resumeLimit,
		TurnLimit:             turnLimit,
		SayEvery:              sayEvery,
		DatabaseURL:           k.String("database_url"),
		RecordShows:           shows,
		AccountEvery:          accountEvery,
		ExecutionEvery:        executionEvery,
		ExecutionStep:         k.Float64("execution_step"),
		ExecutionPatience:     executionPatience,
		OrdersShown:           ordersShown,
		HistoryDays:           historyDays,
		GatewayURL:            k.String("gateway_url"),
		GatewayToken:          k.String("gateway_token"),
		EnvelopeAddr:          k.String("envelope_addr"),
		EnvelopePath:          k.String("envelope_path"),
		EnvelopeCallers:       callers,
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
		return nil, fmt.Errorf("ROLES is empty: name at least one of harness, api, mcp, envelope")
	}
	var out []Role
	for _, part := range strings.Split(raw, ",") {
		switch role := Role(strings.TrimSpace(part)); role {
		case RoleHarness, RoleAPI, RoleMCP, RoleEnvelope:
			out = append(out, role)
		default:
			return nil, fmt.Errorf("unknown role %q: expected harness, api, mcp or envelope", part)
		}
	}
	return out, nil
}

// defaultRecordShows is how much of the record the page carries when nobody said
// otherwise: enough for a day of work to be read in one screen.
const defaultRecordShows = 50

// defaultOrdersShown and defaultHistoryDays are what the page carries when
// nobody said otherwise: a day of orders, and the length of the event.
const (
	defaultOrdersShown = 25
	defaultHistoryDays = 14
)

// parseExecution reads how the ladder walks. A deployment that walks orders says
// both how often and for how long; asking for one without the other is a bound
// this code would have to invent, and an invented deadline silently outranks the
// one an operator wrote.
func parseExecution(every, patience string) (time.Duration, time.Duration, error) {
	if strings.TrimSpace(every) == "" {
		return 0, 0, nil
	}

	stepEvery, err := time.ParseDuration(every)
	if err != nil {
		return 0, 0, fmt.Errorf("EXECUTION_EVERY is not a duration: %w", err)
	}
	if strings.TrimSpace(patience) == "" {
		return 0, 0, fmt.Errorf("EXECUTION_EVERY is set: say how long an order may wait with EXECUTION_PATIENCE")
	}
	waits, err := time.ParseDuration(patience)
	if err != nil {
		return 0, 0, fmt.Errorf("EXECUTION_PATIENCE is not a duration: %w", err)
	}
	if stepEvery <= 0 || waits <= 0 {
		return 0, 0, fmt.Errorf("EXECUTION_EVERY and EXECUTION_PATIENCE must be longer than nothing")
	}
	if waits < stepEvery {
		return 0, 0, fmt.Errorf("EXECUTION_PATIENCE (%s) is shorter than one step (%s): the order would be cancelled before it walks", waits, stepEvery)
	}

	return stepEvery, waits, nil
}

// parseTurnLimit reads how long one turn may run. Empty means no bound, which is
// legal: a deployment with one session and nobody waiting needs none.
// parseDuration reads one declared deadline. An empty value means the operator
// did not declare it, and every caller decides for itself whether that is legal -
// substituting a number here would silently outrank the declaration.
func parseDuration(name, raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s is not a duration: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be longer than nothing", name)
	}

	return value, nil
}

func parseTurnLimit(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}

	limit, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("TURN_LIMIT is not a duration: %w", err)
	}
	if limit <= 0 {
		return 0, fmt.Errorf("TURN_LIMIT must be longer than nothing")
	}

	return limit, nil
}

// parseAccountEvery reads how often the account's value is written down. Empty
// means the history is not kept on this deployment.
func parseAccountEvery(raw string) (time.Duration, error) {
	if strings.TrimSpace(raw) == "" {
		return 0, nil
	}

	every, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("ACCOUNT_EVERY is not a duration: %w", err)
	}
	if every <= 0 {
		return 0, fmt.Errorf("ACCOUNT_EVERY must be longer than nothing")
	}

	return every, nil
}

// parseSymbols reads the list of underlyings, upper-cased because that is how
// the broker names them and how the history is keyed.
// parseCallers reads which bearer token belongs to which caller, written as
// "token=identity,token=identity". It lives in the environment and not in the
// ruleset file, because the ruleset is committed and a token is a secret.
//
// A caller nobody can be resolved to is refused an envelope, so an envelope role
// with no callers at all serves nothing and is a misconfiguration, not a quiet
// open door.
func parseCallers(raw string, roles []Role) (map[string]string, error) {
	callers := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		token, identity, ok := strings.Cut(part, "=")
		token, identity = strings.TrimSpace(token), strings.TrimSpace(identity)
		if !ok || token == "" || identity == "" {
			return nil, fmt.Errorf("ENVELOPE_CALLERS: %q is not token=identity", part)
		}
		if already, taken := callers[token]; taken {
			return nil, fmt.Errorf("ENVELOPE_CALLERS: one token is given to both %q and %q", already, identity)
		}
		callers[token] = identity
	}

	for _, role := range roles {
		if role == RoleEnvelope && len(callers) == 0 {
			return nil, fmt.Errorf("ENVELOPE_CALLERS is empty: the envelope role would recognise nobody")
		}
	}

	return callers, nil
}

func parseSymbols(raw string) []string {
	var symbols []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.ToUpper(strings.TrimSpace(part)); trimmed != "" {
			symbols = append(symbols, trimmed)
		}
	}

	return symbols
}

// parseEvery reads how often a volatility reading is taken. A deployment that
// records nothing needs no interval; one that records says how often, rather
// than inheriting a number chosen here.
func parseEvery(raw string, underlyings []string) (time.Duration, error) {
	if len(underlyings) == 0 {
		return 0, nil
	}
	if strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("VOLATILITY_UNDERLYINGS is set: say how often to read it with VOLATILITY_EVERY, for example 5m")
	}

	every, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("VOLATILITY_EVERY is not a duration: %w", err)
	}
	if every <= 0 {
		return 0, fmt.Errorf("VOLATILITY_EVERY must be longer than nothing")
	}

	return every, nil
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

// parseTimeout reads the bound on one call to the agent. The harness cannot run
// without it and says so rather than inventing a number: a deadline chosen here
// would silently outrank the one an operator wrote.
func parseTimeout(raw string, roles []Role) (time.Duration, error) {
	needed := false
	for _, role := range roles {
		if role == RoleHarness {
			needed = true
		}
	}
	if strings.TrimSpace(raw) == "" {
		if needed {
			return 0, fmt.Errorf("AGENT_CALL_TIMEOUT is required by the harness role, for example 30s")
		}
		return 0, nil
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("AGENT_CALL_TIMEOUT holds %q, which is not a duration: %w", raw, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("AGENT_CALL_TIMEOUT is %s: a call with no time to complete never completes", timeout)
	}

	return timeout, nil
}

func (c Config) Has(role Role) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}
