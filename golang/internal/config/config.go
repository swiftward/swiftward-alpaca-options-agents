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

// Driver is how the harness reaches the agent. It says nothing about which agent
// that is - a driver is a way of speaking, not a vendor.
type Driver string

const (
	// DriverCodex starts the agent as a child process and speaks to it over its
	// own protocol on a pipe.
	DriverCodex Driver = "codex"
	// DriverMailbox parks turns for a client that polls. The client may be
	// anything that can run a command and read its output, which includes the
	// harnesses a person sits in.
	DriverMailbox Driver = "mailbox"
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
	// AgentEnvKeep names the environment variables the agent's process is given
	// ON TOP of the ones it always gets - see agent.Environment. The child is
	// built by name rather than inheriting: the harness holds a database URL and
	// a chat token the model has no use for, and a session that ran `env` would
	// put them in its own context. A server added to the session's configuration
	// tomorrow names its credential here rather than in a new image.
	AgentEnvKeep []string
	// AgentDir and AgentSandbox are what every session is given to work in;
	// AgentModel overrides the agent's own configured model.
	AgentDir     string
	AgentSandbox string
	AgentModel   string
	// SkillsDir is where this image carries the skills it can offer a session.
	// The declaration says which of them this agent gets. Empty means the process
	// lays out no skills, which is what every role but the harness does.
	SkillsDir string
	// ThreadFile is where the conversation's identifier is kept between runs.
	// Empty means a restart begins a new conversation.
	ThreadFile string
	// WakeupFile is where the session's standing wake-ups are kept between runs.
	// Empty means the session is offered no way to ask for one.
	WakeupFile string
	// BrokerMCPURL is the broker's own server, read by the harness only to know
	// when a price wake-up has come true.
	BrokerMCPURL string
	// BrokerMCPToken authenticates the harness where BrokerMCPURL names a policy
	// gateway rather than the broker's own server. Empty where it names the
	// broker, which asks for nothing.
	BrokerMCPToken string
	// UserToken names the person the agent acts for. It travels in the gateway's
	// own header beside the machine's credential, so the record reads as "this
	// agent, for this person". Empty where nobody stands behind the agent.
	UserToken string

	// UserHeader is the header the endpoint was declared to read the person from.
	// It comes from the same value the declaration uses, so the client writes
	// where the gateway looks instead of the two agreeing by coincidence.
	UserHeader string
	// Execution walks an unfilled structure toward the price the book shows. Zero
	// interval means the ladder does not run and an order rests where the session
	// placed it.
	ExecutionEvery    time.Duration
	ExecutionStep     float64
	ExecutionPatience time.Duration
	TakeProfitEvery time.Duration
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
	// ScreenerKeep is how long a sweep's findings are kept after a newer sweep
	// replaced them. They are the only record of what the option book offered:
	// the broker publishes no history of two-sided option quotes.
	ScreenerKeep time.Duration
	// ScreenerPerMinute is the broker's limit on requests, which the sweep never
	// exceeds. The free plan allows 200.
	ScreenerPerMinute int
	// ScreenerWorkers is how many underlyings the sweep asks about at once.
	// Empty or zero means one, which is how the sweep behaved before this
	// existed. What should stop a sweep is the broker's limit above, not the
	// shape of our own loop.
	ScreenerWorkers int
	// ScreenerDearest is the most the round trip may cost, as a percent of the
	// credit. This is the number that separates what earns from what loses.
	ScreenerDearest float64
	// ScreenerExpirations bounds how far out to look, in days.
	ScreenerExpirations int
	// ThreadResumeLimit bounds the one request that resumes an earlier
	// conversation. The harness wakes nobody until it returns, and a fresh thread
	// is a cheap fallback, so this is far shorter than AgentCallTimeout.
	ThreadResumeLimit time.Duration
	// TickEvery is how often the harness asks its schedule whether anything is
	// due. Zero leaves the harness on its own default of a minute.
	//
	// It is separated from the other two because the three cost different things.
	// This one asks nobody anything: the schedule is a map in memory, and no
	// request leaves the process. What it is NOT is free - unless RereadEvery
	// says otherwise, a tick also re-reads the declaration and re-stamps the
	// skill tree, which reads every skill file twice and parses the mount table.
	// Lower this alone and that reading follows it.
	TickEvery time.Duration
	// RereadEvery is how often the declaration and the skills are brought level
	// with the disk. Zero means every tick, which is what this did before the
	// three were told apart - so leaving it empty changes nothing.
	//
	// Set it whenever TICK_EVERY goes below a minute. It is the only one of the
	// three that touches the filesystem, and its cost grows with the skill tree,
	// not with anything the schedule needs.
	RereadEvery time.Duration
	// PriceEvery is how often the prices a wake-up watches are read. Zero means
	// every tick, which is what this program did before the two were separated.
	//
	// This one is the expensive half: one request per read, and the ceiling is
	// 200 a minute per account - shared with the screener, the ladder and the
	// agent's own calls. A ten-second read is six a minute, three percent of it;
	// a one-second read is thirty percent, which is a third of the budget taken
	// from the defence to learn a price nobody asked for.
	//
	// Time wake-ups are NOT held back by this: skipping a read only means the
	// price map is empty this tick, and a wake-up waiting for a time does not
	// look at prices at all.
	PriceEvery time.Duration
	// TurnLimit bounds how long one turn may run before the harness interrupts it.
	// Empty means a turn runs until the agent ends it.
	TurnLimit time.Duration
	// AgentCallTimeout bounds one request to the agent. Without it a hung agent
	// takes the chat down with it: the loop that reads messages is the same loop
	// that talks to the agent.
	AgentCallTimeout time.Duration

	// AgentDriver is HOW the harness reaches the agent, as opposed to which agent
	// it is. Empty means DriverCodex, which is what this program did before there
	// was a choice.
	//
	// The choice exists because starting the agent as a child process is the one
	// thing here that decides which agents may ever run: a session a person sits
	// in, or a harness on another machine, cannot be started as our child and so
	// could not be woken by this clock at all. DriverMailbox parks the turn for a
	// client that polls instead, and everything else - the schedule, the
	// wake-ups, the room, the record - is the same code either way. That sameness
	// is the point: two agents driven differently are still comparable, because
	// only the driver differs.
	AgentDriver Driver
	// MailboxAddr is where the mailbox is served. It is its own listener rather
	// than a path on an existing one because the thing that polls it is outside
	// this deployment, and what is exposed outward should be one address that
	// carries nothing else.
	MailboxAddr string
	// MailboxToken is the one credential, and it is also the identity: the token
	// in the URL names which agent's turns those are. Ten agents means ten
	// mailboxes with ten tokens, and nothing joins them.
	MailboxToken string
	// MailboxHold is the longest one poll is held open before it answers that
	// there is nothing. Empty takes the mailbox's own default.
	MailboxHold time.Duration
	// MailboxStale is how long a parked turn may go unclaimed before it is given
	// up on and said out loud. Empty takes the mailbox's own default.
	MailboxStale time.Duration

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
	EnvelopeAddr string
	EnvelopePath string
	// EnvelopeIdentity is which agent in the ruleset this process is. The ladder
	// needs it to read the same position limit the session is told to size by.
	EnvelopeIdentity string
	EnvelopeCallers  map[string]string

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

	screenKeep, err := parseDuration("SCREENER_KEEP", k.String("screener_keep"))
	if err != nil {
		return Config{}, err
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

	// Half a minute by default. The point of the watch is to be there when the
	// number crosses the line; thirty-minute defence windows have already missed it.
	takeProfitEvery := 30 * time.Second
	if raw := k.String("take_profit_every"); raw != "" {
		takeProfitEvery, err = time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("TAKE_PROFIT_EVERY %q is not a duration like 30s: %w", raw, err)
		}
	}

	tickEvery, err := parseDuration("TICK_EVERY", k.String("tick_every"))
	if err != nil {
		return Config{}, err
	}

	rereadEvery, err := parseDuration("REREAD_EVERY", k.String("reread_every"))
	if err != nil {
		return Config{}, err
	}

	priceEvery, err := parseDuration("PRICE_EVERY", k.String("price_every"))
	if err != nil {
		return Config{}, err
	}

	driver, err := parseDriver(k.String("agent_driver"), roles,
		k.String("mailbox_addr"), k.String("mailbox_token"))
	if err != nil {
		return Config{}, err
	}

	mailboxHold, err := parseDuration("MAILBOX_HOLD", k.String("mailbox_hold"))
	if err != nil {
		return Config{}, err
	}

	mailboxStale, err := parseDuration("MAILBOX_STALE", k.String("mailbox_stale"))
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
		AgentEnvKeep:          parseSymbols(k.String("agent_env_keep")),
		AgentDir:              k.String("agent_dir"),
		AgentSandbox:          k.String("agent_sandbox"),
		SkillsDir:             k.String("skills_dir"),
		AgentModel:            k.String("agent_model"),
		ThreadFile:            k.String("thread_file"),
		WakeupFile:            k.String("wakeup_file"),
		BrokerMCPURL:          k.String("broker_mcp_url"),
		UserToken:             k.String("user_token"),
		UserHeader:            k.String("user_header_name"),
		BrokerMCPToken:        k.String("broker_mcp_token"),
		TickEvery:             tickEvery,
		RereadEvery:           rereadEvery,
		PriceEvery:            priceEvery,
		AgentCallTimeout:      callTimeout,
		ScreenerUnderlyings:   screened,
		ScreenerEvery:         screenEvery,
		ScreenerKeep:          screenKeep,
		ScreenerPerMinute:     k.Int("screener_per_minute"),
		ScreenerWorkers:       k.Int("screener_workers"),
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
		TakeProfitEvery:       takeProfitEvery,
		OrdersShown:           ordersShown,
		HistoryDays:           historyDays,
		GatewayURL:            k.String("gateway_url"),
		GatewayToken:          k.String("gateway_token"),
		EnvelopeAddr:          k.String("envelope_addr"),
		EnvelopePath:          k.String("envelope_path"),
		EnvelopeIdentity:      k.String("envelope_identity"),
		EnvelopeCallers:       callers,
		AgentDriver:           driver,
		MailboxAddr:           k.String("mailbox_addr"),
		MailboxToken:          k.String("mailbox_token"),
		MailboxHold:           mailboxHold,
		MailboxStale:          mailboxStale,
		Telegram: telegram.Config{
			Token:        k.String("telegram_bot_token"),
			ChatID:       k.Int64("telegram_chat_id"),
			TopicID:      k.Int("telegram_topic_id"),
			AllowUserIDs: allowUserIDs,
		},
	}, nil
}

// parseDriver reads how the harness is to reach the agent, and refuses a
// mailbox that could not be reached.
//
// The refusal is the point. A mailbox with no address is served nowhere and a
// mailbox with no token refuses everyone, and in both cases the harness would
// start, keep its clock, wake its sessions on time and park every one of them
// for a client that can never arrive. That failure looks exactly like a quiet
// day. So it is made a failure to start instead: a declared way of reaching the
// agent that does not reach the agent must not be allowed to look like working.
func parseDriver(raw string, roles []Role, addr, token string) (Driver, error) {
	switch driver := Driver(strings.TrimSpace(raw)); driver {
	case "", DriverCodex:
		return DriverCodex, nil
	case DriverMailbox:
		var missing []string
		if strings.TrimSpace(addr) == "" {
			missing = append(missing, "MAILBOX_ADDR")
		}
		if strings.TrimSpace(token) == "" {
			missing = append(missing, "MAILBOX_TOKEN")
		}
		// Only the harness drives an agent. A process running the api or mcp role
		// alone has no clock and no turns, and asking it for a mailbox address
		// would refuse a deployment that is entirely correct.
		if len(missing) > 0 && hasRole(roles, RoleHarness) {
			return "", fmt.Errorf("AGENT_DRIVER=mailbox needs %s: without it the harness parks every turn where nobody can take it",
				strings.Join(missing, " and "))
		}

		return DriverMailbox, nil
	default:
		return "", fmt.Errorf("unknown AGENT_DRIVER %q: expected codex or mailbox", raw)
	}
}

func hasRole(roles []Role, want Role) bool {
	for _, role := range roles {
		if role == want {
			return true
		}
	}

	return false
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

func (c Config) Has(role Role) bool { return hasRole(c.Roles, role) }
