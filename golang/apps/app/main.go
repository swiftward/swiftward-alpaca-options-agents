// Command app runs this project's three roles in one process: the harness that
// holds the clock, the read-side API that serves the demo page, and the MCP
// server the agent uses to record its intent and read its own state.
//
// ROLES chooses which of them run. Everything else is read from the environment
// by internal/config.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	// The timezone database travels with the binary. Trading hours are New York's,
	// and a slim image carries no zone files - without this the declaration fails
	// to load with "unknown time zone", which is a deployment detail dressed as a
	// configuration error.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/account"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/agent"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/api"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/config"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/declaration"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/envelope"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/execution"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/harness"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/mailbox"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/placement"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/reconcile"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/sessiontools"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/skills"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/takeprofit"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/volatility"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

// restingUnderlyings answers which underlyings have an order of ours working.
// It is a small adapter rather than a method on the broker: what the session's
// tools need is a list of names, and the broker answers in orders.
type restingUnderlyings struct {
	broker *marketdata.Broker
	shown  int
}

func (r restingUnderlyings) RestingUnderlyings(ctx context.Context) ([]string, error) {
	orders, err := r.broker.Orders(ctx, r.shown)
	if err != nil {
		return nil, err
	}

	return marketdata.RestingUnderlyings(orders), nil
}

func main() {
	// The level is a setting because the interesting failures are protocol-level
	// and rare: turning the detail on must not need a new image.
	settings := zap.NewProductionConfig()
	if level := os.Getenv("LOG_LEVEL"); level != "" {
		parsed, err := zapcore.ParseLevel(level)
		if err != nil {
			panic(err)
		}
		settings.Level = zap.NewAtomicLevelAt(parsed)
	}

	log, err := settings.Build()
	if err != nil {
		panic(err)
	}
	defer func() { _ = log.Sync() }()

	if err := run(log); err != nil {
		log.Fatal("stopped", zap.Error(err))
	}
}

// fusePercent is how far the account may fall from yesterday's close before the
// day is over, from the declaration in force.
//
// Parsed strictly. The parameters are prose for a model, so this one can be
// written in a way no program can read - and a fuse that quietly took zero from
// "three percent" would cancel every opening order on a flat day.
func fusePercent(declared *declaration.Watcher) (float64, error) {
	written, ok := declared.Current().Parameters["daily_fuse_percent"]
	if !ok {
		return 0, errors.New("the declaration names no daily_fuse_percent")
	}
	percent, err := strconv.ParseFloat(strings.TrimSpace(written), 64)
	if err != nil {
		return 0, fmt.Errorf("daily_fuse_percent %q is not a number", written)
	}
	if percent <= 0 {
		return 0, fmt.Errorf("daily_fuse_percent is %v, which fuses nothing", percent)
	}

	return percent, nil
}

// minEdgePoints is the least a structure may pay above what it must survive, in
// percentage points, from the declaration in force. The ladder holds an order's
// WORST PRICE to it; the session enters on the same number.
//
// Parsed strictly, for the reason above it: the parameters are prose for a model,
// and an edge that quietly took zero from "at least +3" would let every worst
// price through and read as a working gate.
func minEdgePoints(declared *declaration.Watcher) (float64, error) {
	written, ok := declared.Current().Parameters["min_edge_points"]
	if !ok {
		return 0, errors.New("the declaration names no min_edge_points")
	}
	points, err := strconv.ParseFloat(strings.TrimSpace(written), 64)
	if err != nil {
		return 0, fmt.Errorf("min_edge_points %q is not a number", written)
	}
	// "NaN" parses. It then compares false against every edge, so the ladder would
	// refuse every concession it was given rather than let them through, and every
	// opening order would die on patience.
	if math.IsNaN(points) || math.IsInf(points, 0) {
		return 0, fmt.Errorf("min_edge_points is %q: state a number", written)
	}

	return points, nil
}

// whyStopped prefers the error that CANCELLED the run over the error that merely
// noticed it. The workers start before the last of the startup steps, so one
// worker refusing its configuration cancels the group and every step after it
// fails with "context canceled" - which names the victim and buries the cause.
// Measured 27 August: a screener missing SCREENER_KEEP killed the process in
// seven milliseconds and reported a database write.
func whyStopped(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
		return cause
	}

	return err
}

// startFloor is the least a start is ever given, for a deployment that opens no
// conversation at all: reaching the database, the broker and the envelope.
//
// A variable rather than a constant so a test can prove the give-up without
// standing there for a minute.
var startFloor = 90 * time.Second

// startLimit is how long the whole start may take before the process gives up on
// itself. It is DERIVED from the bounds the start actually obeys, not chosen.
//
// Chosen, it was ninety seconds while the settings allowed two hundred: resuming
// yesterday's thread may take THREAD_RESUME_LIMIT, and starting a fresh one after
// that failed may take AGENT_CALL_TIMEOUT - twenty and a hundred and eighty here.
// So a start doing exactly what it was told was killed at ninety, and the process
// came up only on the retry, when the failed thread had already been forgotten
// and one request was left. Four times on 28 August, including the rehearsal.
//
// The watchdog exists to catch a start that is stuck, and a start still inside
// its own bounds is not stuck. Half again on top leaves room for the database and
// the broker, which are reached before any of this.
func startLimitFor(cfg config.Config) time.Duration {
	opening := cfg.ThreadResumeLimit + cfg.AgentCallTimeout
	if opening <= 0 {
		return startFloor
	}
	limit := opening + opening/2
	if limit < startFloor {
		return startFloor
	}

	return limit
}

// defaultExecutionStep is one tick on these contracts: the broker quotes them in
// cents, and a step smaller than that is refused.
const defaultExecutionStep = 0.01

// mailboxBase is where a mailbox is served on its own listener. It is a constant
// rather than a setting because the token in the path is the part that varies,
// and a client holding one URL should not also have to be told the shape of it.
const mailboxBase = "/mailbox/"

func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Starting is bounded, because a start that never finishes looks exactly like
	// a start that is going fine. On 26 August this process stopped while opening
	// the conversation with the agent and stayed there for five hours: the
	// scheduler is built after that point, so nothing ran, nothing failed, and
	// nothing was logged. The container read as Up the whole time, so the restart
	// policy - the one thing that would have fixed it - never fired.
	//
	// Dying is the useful answer here. Whatever supervises this process can start
	// it again; nothing can talk a stuck start into finishing.
	started := make(chan struct{})
	startLimit := startLimitFor(cfg)
	go func() {
		select {
		case <-started:
		case <-ctx.Done():
		case <-time.After(startLimit):
			log.Fatal("the process did not finish starting in time",
				zap.Duration("limit", startLimit),
				zap.String("hint", "the last line logged says where it stopped"))
		}
	}()

	// The record outlives the process: a judge opening the page after a restart
	// must still see the week, not an empty screen.
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err = db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()

		// The process that keeps the record stamps it; everything else compares
		// and refuses. See internal/db/account.go for what this stops.
		bind := db.Check
		if cfg.Has(config.RoleHarness) {
			bind = db.Claim
		}
		if err := bind(ctx, pool, cfg.EnvelopeIdentity); err != nil {
			return err
		}
		log.Info("the record is of this account", zap.String("account", cfg.EnvelopeIdentity))
	}

	var state record.Keeper = record.NewMemory()
	// The screener keeps what it prices in the database and nowhere else: a
	// shortlist is worth having only if it survives the process that made it, and
	// the in-memory record exists for runs that keep nothing.
	var shortlist *record.Postgres
	if pool != nil {
		kept, err := record.NewPostgres(pool, cfg.RecordShows)
		if err != nil {
			return err
		}
		state, shortlist = kept, kept
		log.Info("record kept in postgres", zap.Int("shows", cfg.RecordShows))
	} else {
		log.Warn("no DATABASE_URL: the record lives only as long as this process")
	}

	// What the session asked to be woken for outlives the process: a promise the
	// harness forgot would be worse than one it never made.
	var wakeups *wakeup.Store
	if cfg.WakeupFile != "" {
		wakeups, err = wakeup.Open(cfg.WakeupFile)
		if err != nil {
			return err
		}
		log.Info("wake-ups loaded", zap.Int("standing", len(wakeups.List())), zap.String("file", cfg.WakeupFile))
	}

	// One chat serves both roles: the harness posts what the session said, and the
	// mcp role offers the agent a way to speak when no harness is running it.
	var chat *telegram.Bot
	if cfg.Telegram.Configured() {
		chat, err = telegram.New(cfg.Telegram, log.Named("telegram"))
		if err != nil {
			return err
		}
	} else {
		log.Info("no chat configured: nothing will be posted and nobody can write to a session")
	}

	// The histories are kept by whichever process holds the clock: it is the one
	// that runs all day, and both readings are mechanical - no session is woken
	// for either.
	var series *volatility.Postgres
	var line *account.Postgres
	if pool != nil {
		series = volatility.NewPostgres(pool)
		line = account.NewPostgres(pool)
	}

	// The skills the session reads, kept level with the source this deployment
	// was given. Only the role that starts the agent lays them out: nothing else
	// reads that directory.
	//
	// The source is READ from and never written to, which is what lets a checkout
	// be mounted over it: the narrowing a declaration does then still applies,
	// because this process is the one that copies the chosen skills across. The
	// alternative - mounting over the directory the session reads - would hand it
	// every skill in the checkout and make the same declaration behave one way
	// here and another on a deployment.
	var layer *skills.Layer
	if cfg.Has(config.RoleHarness) && cfg.SkillsDir != "" && cfg.AgentDir != "" {
		layer = &skills.Layer{Source: cfg.SkillsDir, AgentDir: cfg.AgentDir}
	}

	// The declaration is read here and re-read while the process runs: the harness
	// wakes sessions by it, and the session reads it to answer when it will be
	// woken next. Both go through the watcher, so an edited file reaches the clock
	// and the session's own answer at the same moment.
	//
	// Putting the skills in place is part of putting a declaration in force, not a
	// step beside it. A declaration that shrinks its `skills:` or drops a
	// `parameters:` while the process runs would otherwise leave the skill it
	// stopped naming sitting in the directory the session reads, invocable with
	// none of the numbers it needs. Here the two cannot come apart: a declaration
	// whose skills cannot be laid out never goes into force, and one that does go
	// into force has had them laid out.
	//
	// At start that makes an unusable declaration a failure to start, exactly as
	// an unreadable one always was. Later it makes it a re-read that is refused
	// with the schedule already in force left running.
	var declared *declaration.Watcher
	if cfg.DeclarationPath != "" {
		declared, err = declaration.Watch(cfg.DeclarationPath, func(d *declaration.Declaration) error {
			return layTheSkills(log, layer, d.Skills, d.Parameters)
		})
		if err != nil {
			return err
		}
	}

	// With no declaration to name them, every skill this image carries is laid
	// out. It is not skipped: the directory outlives the image, so leaving it
	// untouched would leave a session reading whatever an older image put there.
	//
	// A skill that needs numbers cannot be laid out this way, and that is a
	// refusal rather than a skill handed over without them - the skill itself
	// tells a session that finds its numbers missing to open nothing. The numbers
	// live in a declaration, so the way out is to name one.
	if layer != nil && declared == nil {
		if err := layTheSkills(log, layer, nil, nil); err != nil {
			return fmt.Errorf("%w - and this process was started without DECLARATION, "+
				"so there is nothing here that could give it: name a declaration, or point SKILLS_DIR at skills that need no numbers", err)
		}
	}

	// ONE broker for every role this process runs, and it is the gateway. Reads
	// and orders travel the same address on purpose: a read that skips the gateway
	// is a read no rule saw and no record holds, and the record is what a judge is
	// given. There is no second address to point them at - the earlier bypass is
	// gone, because a knob that must never be turned is turned eventually.
	var broker *marketdata.Broker
	if cfg.BrokerMCPURL != "" {
		broker = marketdata.NewBrokerWithToken(cfg.BrokerMCPURL, cfg.BrokerMCPToken).
			ActingFor(cfg.UserHeader, cfg.UserToken)
	}

	group, ctx := errgroup.WithContext(ctx)

	if cfg.Has(config.RoleAPI) {
		read := api.Read{
			Record:      state,
			OrdersShown: cfg.OrdersShown,
			HistoryDays: cfg.HistoryDays,
			WebDir:      cfg.WebDir,
			Key:         cfg.PageKey,
			Log:         log.Named("api"),
		}
		if broker != nil {
			read.Broker = broker
		}
		if line != nil {
			read.History = line
		}
		// The limits are served from the SAME file, through the same call that
		// answers the agent. Otherwise the page would show a retelling that one day
		// differs from what is actually traded.
		if cfg.EnvelopePath != "" && cfg.EnvelopeIdentity != "" {
			read.EnvelopePath, read.EnvelopeIdentity = cfg.EnvelopePath, cfg.EnvelopeIdentity
		}
		if shortlist != nil {
			read.Sweep = shortlist
		}

		handler, err := read.Handler()
		if err != nil {
			return err
		}
		group.Go(func() error { return serve(ctx, cfg.Addr, handler, log.Named("api")) })
	}

	// The harness is built before the mcp role so the session's tools can ask it
	// which turn they are inside. With no harness in this process they ask
	// nobody, and an intent is filed without a turn rather than with a guess.
	var running *harness.Harness
	if cfg.Has(config.RoleHarness) {
		running = &harness.Harness{
			CallTimeout:  cfg.AgentCallTimeout,
			TurnLimit:    cfg.TurnLimit,
			TickEvery:    cfg.TickEvery,
			RereadEvery:  cfg.RereadEvery,
			PriceEvery:   cfg.PriceEvery,
			SayEvery:     cfg.SayEvery,
			DefaultModel: cfg.AgentModel,
			Now:          time.Now,
			Log:          log.Named("harness"),
		}
	}

	// Named out here so the summary at the end can say whether the gate stands.
	intentGate := false
	if cfg.Has(config.RoleMCP) {
		var poster sessiontools.Poster
		if chat != nil && !cfg.Has(config.RoleHarness) {
			// With a harness running, everything the session says is already posted;
			// a tool for it as well would double every message.
			poster = chat
		}

		var standing sessiontools.Wakeups
		if wakeups != nil {
			standing = wakeups
		}

		tools := sessiontools.Tools{
			Record:  state,
			Now:     time.Now,
			Chat:    poster,
			Wakeups: standing,
		}
		if declared != nil {
			tools.Schedule = declared
		}
		if running != nil {
			tools.Running = running
		}
		if series != nil {
			tools.Volatility = series
		}
		if shortlist != nil && len(cfg.ScreenerUnderlyings) > 0 {
			tools.Shortlist = shortlist
			// The interval travels with the list so the list can say whether it is
			// fresh. Without it a session is handed an age in seconds and has to
			// guess what is normal - and guessing cost a trade on 27 August.
			tools.SweepEvery = cfg.ScreenerEvery
		}
		// What is already working, so the screener's list does not offer a second
		// position on an underlying this account is already in. Needs the broker:
		// resting orders are the broker's own state, and a deployment without one
		// leaves the list as the screener priced it.
		if broker != nil {
			tools.Resting = restingUnderlyings{broker: broker, shown: cfg.OrdersShown}
		}

		// The gate on record_intent: an intent is refused unless the envelope was
		// read in the SAME turn. It is wired only where it can actually be
		// satisfied, and where it cannot the log says so rather than the agent
		// discovering it on its first intent in the middle of a trading day.
		//
		// Two things have to be true. There must be an envelope to read - a
		// deployment without one offers the session no read_envelope at all, so
		// the gate would refuse every intent and name a tool that is not in the
		// list. And the driver must report the session's tool calls, because that
		// is what fills the table this is asked of: a mailbox carries what the
		// agent SAYS and not what it called, so the table stays empty however
		// faithfully the session reads its envelope. The same distinction already
		// governs the broker watchdog - see harness.ReportsToolCalls.
		switch {
		case shortlist == nil:
		case cfg.GatewayURL == "":
			log.Warn("intents are recorded without checking the envelope was read this turn",
				zap.String("why", "this deployment has no envelope to read: GATEWAY_URL is empty"))
		case cfg.AgentDriver == config.DriverMailbox:
			log.Warn("intents are recorded without checking the envelope was read this turn",
				zap.String("why", "this driver does not report the session's tool calls, so the record cannot show one"))
		default:
			tools.Asked = shortlist
			intentGate = true
			log.Info("an intent is refused unless the envelope was read in the same turn")
		}
		// Where to place the legs of a structure whose worst case is in the MIDDLE.
		// The screener answers the other half of the question - which underlying of
		// them all - and its measure suits only a vertical, whose payout is
		// monotone. This needs the broker: the price now, the book for one expiry,
		// and the underlying's own daily history. Without it the tool is simply not
		// offered.
		if broker != nil {
			// The exchange's calendar, not the machine's. The team works from UTC+8,
			// and by the local date Thursday evening in New York is already Friday:
			// not a day remains before Friday's expiry, and the tool would refuse
			// through the whole American evening. The error in the other direction
			// is quieter and worse - a day less of term makes sigma shorter and
			// every distance in sigmas longer, so a leg nearer than permitted
			// passes as permitted.
			exchange, err := time.LoadLocation("America/New_York")
			if err != nil {
				return fmt.Errorf("load the exchange calendar: %w", err)
			}
			tools.Placements = placement.Scorer{
				Market: broker,
				Where:  exchange,
				// Three years. Selecting by volatility regime throws most of it
				// away: two years gave 216 usable windows against the 466 the same
				// measurement stood on in the research. A number with too small a
				// sample under it is worse than no number.
				History: 1100,
				Now:     time.Now,
			}
		}

		handler := tools.Handler()
		group.Go(func() error { return serve(ctx, cfg.MCPAddr, handler, log.Named("mcp")) })
	}

	// The envelope stands where the policy gateway will stand. It is served in
	// its own process so that the thing an agent asks about its limits is never
	// the thing that runs the agent.
	if cfg.Has(config.RoleEnvelope) {
		if cfg.EnvelopePath == "" {
			return errors.New("the envelope role needs ENVELOPE_PATH: the limits it serves live in a file, not in this binary")
		}
		// Read once at start so a broken ruleset is a failure to start rather than
		// a session that asks for its limits and is told nothing.
		if _, err := envelope.Load(cfg.EnvelopePath); err != nil {
			return err
		}
		limits := envelope.Tools{Path: cfg.EnvelopePath, Callers: cfg.EnvelopeCallers, Log: log.Named("envelope")}
		handler := limits.Handler()
		group.Go(func() error { return serve(ctx, cfg.EnvelopeAddr, handler, log.Named("envelope")) })
	}

	// The ladder finishes what the session started: it can move a price and cancel
	// an order, and it can open nothing.
	// Whether the day's fuse is enforced rather than only asked of the session.
	// Named here so the guards line below can say so: every protection on that
	// line is legal when off and silent when off, which is why they are read in
	// one place.
	fuseStands := false
	if cfg.Has(config.RoleHarness) && cfg.ExecutionEvery == 0 {
		// Said, because the absence is invisible from the outside: orders rest at
		// the price the session named, nothing walks them to the book, and nothing
		// cancels what the book will not take. That is a legal deployment and a
		// very different one.
		log.Warn("no ladder: orders rest where the session placed them and are never walked or cancelled",
			zap.String("why", "EXECUTION_EVERY is zero"))
	}
	if cfg.Has(config.RoleHarness) && cfg.ExecutionEvery > 0 {
		// The one the ladder actually uses is the one checked: a declared-but-unset
		// broker once passed a check on a different variable, and every step then
		// fell on a nil pointer.
		if broker == nil {
			return errors.New("EXECUTION_EVERY is set but BROKER_MCP_URL is empty: " +
				"the ladder moves orders, and orders go through the gateway")
		}
		step := cfg.ExecutionStep
		if step <= 0 {
			step = defaultExecutionStep
		}
		ladder := &execution.Ladder{
			Broker: broker, Every: cfg.ExecutionEvery, Step: step, Record: state,
			Stride:   cfg.ExecutionStride,
			Patience: cfg.ExecutionPatience, Now: time.Now, Log: log.Named("execution"),
		}
		log.Info("the ladder walks", zap.String("stride", cfg.ExecutionStride),
			zap.Duration("every", cfg.ExecutionEvery), zap.Duration("patience", cfg.ExecutionPatience))
		// What one position may lose, in dollars, from the SAME ruleset the
		// envelope serves and the account the broker reports. The session is told
		// to size by this number and can get it wrong; here it stops being advice.
		//
		// The ruleset is re-read on every pass for the same reason the envelope
		// re-reads it: an operator lowering a ceiling edits one file and the next
		// pass obeys, with nothing restarted.
		if cfg.EnvelopePath != "" && cfg.EnvelopeIdentity != "" {
			ladder.Ceiling = func(ctx context.Context) (float64, error) {
				set, err := envelope.Load(cfg.EnvelopePath)
				if err != nil {
					return 0, fmt.Errorf("read the ruleset: %w", err)
				}
				out, err := set.For(cfg.EnvelopeIdentity, "place_option_order")
				if err != nil {
					return 0, err
				}
				share, err := out.PercentOfEquity("max-loss-per-position")
				if err != nil {
					return 0, err
				}
				account, err := broker.Account(ctx)
				if err != nil {
					return 0, fmt.Errorf("read what the account is worth: %w", err)
				}

				return account.Equity * share / 100, nil
			}
			// And what EVERYTHING may lose together. The per-position limit says
			// nothing about how many positions there are: twenty structures, each
			// inside its own ceiling, still put the whole account at risk.
			ladder.Book = func(ctx context.Context) (float64, error) {
				set, err := envelope.Load(cfg.EnvelopePath)
				if err != nil {
					return 0, fmt.Errorf("read the ruleset: %w", err)
				}
				out, err := set.For(cfg.EnvelopeIdentity, "place_option_order")
				if err != nil {
					return 0, err
				}
				share, err := out.PercentOfEquity("max-loss-across-portfolio")
				if err != nil {
					return 0, err
				}
				account, err := broker.Account(ctx)
				if err != nil {
					return 0, fmt.Errorf("read what the account is worth: %w", err)
				}

				return account.Equity * share / 100, nil
			}
			log.Info("resting orders are held to what one position may lose, and to what the book may lose",
				zap.String("identity", cfg.EnvelopeIdentity))
		}
		// The daily fuse, and the number is the DECLARATION's - the same one the
		// session is given at the head of every turn. It is read here rather than
		// copied into a setting of its own, because two places holding one number
		// is how they come to disagree.
		//
		// Until 29 August nothing enforced it. The playbook said as much in its own
		// words, and on 27 August one account refused an entry at 14:01 and took
		// one at 14:18 on the same two figures. It is the session's rule and now
		// also a backstop under it, in the last place our code holds an order.
		if declared != nil {
			// Read ONCE here, and refuse to start without it.
			//
			// The number was read only inside the closure below, on every pass. So
			// a declaration that named no `daily_fuse_percent` came up reporting
			// "the day's fuse is enforced" and then wrote "could not tell whether
			// the day's fuse has blown" on every pass afterwards - a guard the
			// summary said was standing and that could not answer. A teammate's
			// stand ran exactly that this morning.
			//
			// The comparison that settles how to treat it is our own: a record
			// that cannot say which account it belongs to refuses to start. A
			// missing account name and a missing fuse are the same kind of
			// absence, and a fuse you learn about from an error line on every pass
			// is not different from no fuse, except for the noise.
			//
			// The lazy re-read stays. It is what lets the number be lowered while
			// the day runs, which is the same reason the envelope is re-read.
			if _, err := fusePercent(declared); err != nil {
				return fmt.Errorf("the ladder cannot enforce the day's fuse: %w", err)
			}
			ladder.Fuse = func(ctx context.Context) (bool, string, error) {
				percent, err := fusePercent(declared)
				if err != nil {
					return false, "", err
				}
				account, err := broker.Account(ctx)
				if err != nil {
					return false, "", fmt.Errorf("read what the account is worth: %w", err)
				}
				// Yesterday's close is the thing fallen FROM. Without it there is
				// no fall to measure, and a zero would read as a total loss.
				if account.EquityYesterday <= 0 {
					return false, "", errors.New("the broker gives no equity for yesterday's close")
				}
				fallen := (account.EquityYesterday - account.Equity) / account.EquityYesterday * 100
				if fallen < percent {
					return false, "", nil
				}

				return true, fmt.Sprintf(
					"the account is down %.2f%% from yesterday's close of %.0f, and the fuse is %v%%",
					fallen, account.EquityYesterday, percent), nil
			}
			fuseStands = true
			log.Info("the day's fuse is enforced, not only asked of the session",
				zap.String("from", "daily_fuse_percent in the declaration"))

			// No check at startup yet, unlike the fuse above. Until the declaration
			// carries min_edge_points as a bare number this cannot be read, and a
			// process that refuses to start over it would take both accounts down
			// mid-session. It says so on every pass instead, and the startup check
			// arrives with the declaration that makes it readable.
			ladder.MinEdgePoints = func() (float64, error) { return minEdgePoints(declared) }
			log.Info("a worst price is held to the same edge the session enters on",
				zap.String("from", "min_edge_points in the declaration"))
		}
		if running != nil {
			ladder.Wake = func(ctx context.Context, cause string) {
				running.Tell(ctx, cause, "execution")
			}
			ladder.Say = running.Post
		}
		log.Info("walking unfilled structures toward the book",
			zap.Duration("every", cfg.ExecutionEvery),
			zap.Float64("step", step),
			zap.Duration("patience", cfg.ExecutionPatience))
		group.Go(func() error { return ladder.Run(ctx) })
	}

	// The screener prices the whole permitted universe over and over, so a session
	// chooses from what the market offers rather than from the six names its turn
	// had time for. It reads and cannot order.
	if cfg.Has(config.RoleHarness) && len(cfg.ScreenerUnderlyings) > 0 {
		if broker == nil || shortlist == nil {
			return errors.New("SCREENER_UNDERLYINGS is set but there is no broker to price them or no database to keep them in")
		}
		sweep := &screener.Sweep{
			Broker: broker, Universe: cfg.ScreenerUnderlyings, Record: shortlist,
			// What a structure must clear, read from the declaration on every pass.
			// Every one of these is a trading decision, so it lives where the rest
			// of them live; how often and in how many hands the machine sweeps
			// stays below, in the deployment.
			Thresholds: func() (screener.Wanted, error) {
				return screenerThresholds(declared)
			},
			Every: cfg.ScreenerEvery, Keep: cfg.ScreenerKeep,
			PerMinute:   cfg.ScreenerPerMinute,
			Workers:     cfg.ScreenerWorkers,
			Expirations: cfg.ScreenerExpirations,
			Now:         time.Now, Log: log.Named("screener"),
		}
		// Said at startup, and the numbers with it. Every one of them is legal at
		// almost any value and silent at all of them: a sweep with a threshold
		// nobody meant offers a list nobody would have chosen, and the only place
		// that shows is the trades a week later. The same reason the guards line
		// exists.
		wanted, err := screenerThresholds(declared)
		if err != nil {
			return fmt.Errorf("the screener cannot be told what a structure must clear: %w", err)
		}
		log.Info("pricing the universe",
			zap.Float64("nearest_percent", wanted.MinOutOfTheMoney),
			zap.Float64("furthest_percent", wanted.MaxOutOfTheMoney),
			zap.Float64("least_paid_percent", wanted.MinCreditToRisk),
			zap.Float64("most_paid_percent", wanted.MostCreditToRisk),
			zap.Float64("dearest_percent_of_credit", wanted.MaxCostShare),
			zap.Float64("most_delta", wanted.MostDelta),
			zap.Float64("least_edge_points", wanted.LeastEdge),
			zap.Int("underlyings", len(cfg.ScreenerUnderlyings)),
			zap.Duration("every", cfg.ScreenerEvery),
			zap.Int("per_minute", cfg.ScreenerPerMinute),
			zap.Int("workers", max(cfg.ScreenerWorkers, 1)))
		group.Go(func() error { return sweep.Run(ctx) })
	}

	// The profit watch runs without a model and can do exactly one thing: make the
	// book SMALLER. The split is the ladder's, moved from entry to exit - choosing
	// what to sell and at what price is judgement and belongs to the model;
	// looking at a number every half minute and acting when it crosses a line is
	// arithmetic on a clock. An agent's turn costs a minute and a half, defence
	// comes round every thirty, and on 28 August a QQQ spread gave back three
	// quarters of its credit unnoticed.
	// The take-profit share is a TRADING number, so it lives in the declaration
	// beside every other one rather than in the deployment. Until 29 August it was
	// TAKE_PROFIT_AT in the environment - one trading decision kept in a different
	// place from the rest, which also meant two accounts could not differ in it
	// without differing in their deployment.
	//
	// How OFTEN the book is looked at stays in the environment: that is a property
	// of the machine, not of the trade.
	takeProfitDeclared := false
	if declared != nil {
		if _, ok := declared.Current().Parameters["take_profit_at"]; ok {
			takeProfitDeclared = true
		}
	}
	if cfg.Has(config.RoleHarness) && takeProfitDeclared {
		if broker == nil {
			// The WATCH refuses, not the process: equity snapshots, the screener
			// and the volatility history live here too, and refusing to start
			// would cost the curve a judge sees for the sake of a layer that
			// could close nothing anyway.
			log.Error("the profit watch is off: take_profit_at is declared but BROKER_MCP_URL is empty",
				zap.String("why", "the watch sends orders and they go through the gateway; "+
					"without its address it would run and close nothing"))
			takeProfitDeclared = false
		}
	}

	if cfg.Has(config.RoleHarness) && !takeProfitDeclared {
		// Off is a legal setting, and it is the one nobody notices: winners are
		// then held to expiry and the log says nothing about why. It is said out
		// loud here so a deployment started from a bare .env cannot look like one
		// that watches.
		log.Warn("no profit watch: the declaration names no take_profit_at, so a winning structure is held to expiry")
	}

	if cfg.Has(config.RoleHarness) && takeProfitDeclared {
		exchange, err := time.LoadLocation("America/New_York")
		if err != nil {
			return fmt.Errorf("load the exchange calendar: %w", err)
		}
		watch := &takeprofit.Watch{
			Broker: broker, Every: cfg.TakeProfitEvery,
			// Read on every pass, so lowering the share is one edit in the
			// declaration and the next pass obeys - the same property the
			// envelope's ceilings have.
			At: func() (float64, error) {
				written, ok := declared.Current().Parameters["take_profit_at"]
				if !ok {
					return 0, errors.New("the declaration names no take_profit_at")
				}
				share, err := strconv.ParseFloat(strings.TrimSpace(written), 64)
				if err != nil {
					return 0, fmt.Errorf("take_profit_at %q is not a number", written)
				}

				return share, nil
			},
			// The same record the ladder writes to. Without it a closing order
			// this watch sends is in the record only if the ladder later meets
			// it, and one cancelled before that pass is in it nowhere.
			Record: state,
			Now:    time.Now, Where: exchange, Log: log.Named("takeprofit"),
		}
		group.Go(func() error { return watch.Run(ctx) })
	}

	if cfg.Has(config.RoleHarness) && cfg.AccountEvery > 0 {
		if line == nil || broker == nil {
			return errors.New("ACCOUNT_EVERY is set but the recorder has no database or no broker to read")
		}
		recorder := &account.Recorder{
			Broker: broker, Store: line, Every: cfg.AccountEvery,
			Now: time.Now, Log: log.Named("account"),
		}
		log.Info("keeping the account history", zap.Duration("every", cfg.AccountEvery))
		group.Go(func() error { return recorder.Run(ctx) })
	}

	if cfg.Has(config.RoleHarness) && len(cfg.VolatilityUnderlyings) > 0 {
		if series == nil || broker == nil {
			return errors.New("VOLATILITY_UNDERLYINGS is set but the recorder has no database or no broker to read")
		}
		recorder := &volatility.Recorder{
			Market:      broker,
			Store:       series,
			Underlyings: cfg.VolatilityUnderlyings,
			Every:       cfg.VolatilityEvery,
			Now:         time.Now,
			Log:         log.Named("volatility"),
		}
		log.Info("keeping the volatility history",
			zap.Strings("underlyings", cfg.VolatilityUnderlyings),
			zap.Duration("every", cfg.VolatilityEvery))
		group.Go(func() error { return recorder.Run(ctx) })
	}

	if cfg.Has(config.RoleHarness) {
		// A turn open at startup was interrupted by whatever ended the last
		// process. Saying so is the answer to "did we restart mid-work?".
		if left, err := state.CloseTurnsLeftOpen(ctx, time.Now()); err != nil {
			return whyStopped(ctx, err)
		} else if left > 0 {
			log.Warn("turns were left open by an earlier process", zap.Int("turns", left))
		}

		// A call in flight when a process dies may or may not have reached the
		// broker. Saying which would be a guess, so the record says unknown.
		if left, err := state.CloseCallsLeftOpen(ctx, time.Now()); err != nil {
			return whyStopped(ctx, err)
		} else if left > 0 {
			log.Warn("tool calls were in flight when an earlier process ended",
				zap.Int("calls", left))
		}

		// Unknown is the honest thing to write down and the wrong thing to leave
		// written: the broker knows whether the order arrived, and every order this
		// project sends carries a name to ask by.
		if shortlist != nil && broker != nil {
			settled, err := reconcile.Ask(ctx, broker, shortlist,
				time.Now().AddDate(0, 0, -7), log.Named("reconcile"))
			if err != nil {
				log.Error("could not ask the broker about orders left unknown", zap.Error(err))
			} else if settled > 0 {
				log.Info("orders left unknown by an earlier process were settled",
					zap.Int("orders", settled))
			}
		}

		h := running
		h.Record = state
		if wakeups != nil {
			h.Wakeups = wakeups
		}
		if broker != nil {
			h.Prices = broker
		}
		if chat != nil {
			h.Chat = chat
		}
		if declared != nil {
			h.Declaration = declared.Current()
			// One tick, both files. The declaration is re-read, and the skills are
			// brought level with what the source holds now - a session opens
			// SKILL.md while it works, so editing the text of a technique reaches a
			// session already running rather than waiting for a restart.
			//
			// A declaration that CHANGED had its skills laid by the watcher, as
			// part of going into force; laying them a second time here could only
			// fail on a source that moved in between, and that failure would tell
			// the clock to keep the old schedule while the session's own
			// read_schedule already answered with the new one. So that case is left
			// alone and this covers the other two: an unchanged declaration under an
			// edited skill, and a declaration that could not be read at all - which
			// must not also freeze the text of the techniques, since a half-saved
			// declaration is exactly what an operator has while editing.
			h.Reread = func() (*declaration.Declaration, error) {
				before := declared.Current()
				current, err := declared.Reread()
				if err == nil && current != before {
					return current, nil
				}

				return current, errors.Join(err, layTheSkills(log, layer, current.Skills, current.Parameters))
			}
		}

		// The agent is held open for the whole run: that is what lets a person
		// reach work already in progress instead of waiting for it to end.
		//
		// How it is reached is a choice; who it is is not made here. With the
		// mailbox the agent is not started at all - it comes and takes its turns -
		// so there is no process to dial, nothing to resume, and the opening below
		// costs nothing. Everything past this block is the same either way, which
		// is what makes two agents on two drivers worth comparing.
		switch cfg.AgentDriver {
		case config.DriverMailbox:
			box := mailbox.New(cfg.MailboxToken, cfg.MailboxHold, cfg.MailboxStale, log.Named("mailbox"))

			threadID, err := box.Open(ctx)
			if err != nil {
				return err
			}
			log.Info("mailbox ready",
				zap.String("thread_id", threadID),
				zap.String("addr", cfg.MailboxAddr))

			// Giving up on turns nobody came for is the mailbox's own work, and it
			// has to happen when no poll is arriving - which is the case it exists
			// for.
			group.Go(func() error { return box.Run(ctx) })
			group.Go(func() error {
				return serve(ctx, cfg.MailboxAddr, box.Handler(mailboxBase), log.Named("mailbox"))
			})

			h.Conversation = box
		default:
			// By name, never inherited: see agent.Environment. The harness holds
			// credentials the model has no use for, and a session that ran `env`
			// would put them where the record and the page can be read.
			environment := agent.Environment(cfg.AgentEnvKeep, os.LookupEnv)
			client, err := agent.Dial(ctx, cfg.AgentCommand, environment, cfg.AgentCallTimeout, log.Named("agent"))
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			conversation := agent.NewConversation(client, agent.ThreadOptions{
				Model:   cfg.AgentModel,
				Sandbox: cfg.AgentSandbox,
				Dir:     cfg.AgentDir,
			}, cfg.ThreadFile, cfg.AgentCallTimeout, cfg.ThreadResumeLimit)

			// The conversation is opened before the room does: resuming a long
			// thread takes as long as it takes, and doing it under the first
			// message would answer that message with a timeout.
			log.Info("opening the conversation with the agent")
			threadID, err := conversation.Open(ctx)
			if err != nil {
				return err
			}
			log.Info("conversation ready", zap.String("thread_id", threadID))

			h.Conversation = conversation
		}

		group.Go(func() error { return h.Run(ctx) })
	}

	close(started)
	// One line naming every guard and whether it is standing. Each of them is
	// legal when off and invisible when off, so before this the only way to know
	// was to hunt three separate warnings and the absence of a fourth. On the
	// morning of a judged day the question "is everything on" should be one line
	// to read, not an investigation.
	if cfg.Has(config.RoleHarness) {
		log.Info("guards",
			zap.Bool("profit_watch", takeProfitDeclared && broker != nil),
			zap.Bool("turn_limit", cfg.TurnLimit > 0),
			zap.Bool("ladder", cfg.ExecutionEvery > 0 && broker != nil),
			zap.Bool("daily_fuse", fuseStands),
			zap.Bool("intent_gate", intentGate),
			zap.Bool("envelope", cfg.GatewayURL != ""),
			zap.Bool("record", state != nil),
			zap.Bool("screener", shortlist != nil && len(cfg.ScreenerUnderlyings) > 0))
	}
	log.Info("started", zap.Any("roles", cfg.Roles))

	return group.Wait()
}

// serve runs one HTTP listener until ctx ends, then gives in-flight requests the
// shutdown grace the caller allows. The grace is short on purpose: every request
// this process serves is a read.
func serve(ctx context.Context, addr string, handler http.Handler, log *zap.Logger) error {
	if addr == "" {
		return errors.New("no address configured for this role")
	}
	server := &http.Server{Addr: addr, Handler: handler}

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			log.Error("shutdown", zap.Error(err))
		}
	}()

	log.Info("listening", zap.String("addr", addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

const shutdownGrace = 5 * time.Second

// layTheSkills makes the directory the agent reads hold what the declaration
// named, and says so when that changed anything. A pass that found nothing moved
// is silent: this runs once a minute, and a line a minute saying "still the same
// two skills" would bury the one that matters.
//
// A nil layer is a process that lays out no skills - every role but the one that
// starts the agent.
func layTheSkills(log *zap.Logger, layer *skills.Layer, wanted []string, given map[string]string) error {
	if layer == nil {
		return nil
	}

	laid, err := layer.Lay(wanted, given)
	if err != nil {
		return err
	}
	if !laid.Changed {
		return nil
	}
	if laid.Adopted {
		// Nothing was deleted: the directory already held exactly this set, and
		// only the note saying who laid it was written. Said out loud because it
		// is the one case where a directory this process had no proof of writing
		// becomes one it may rebuild - which is what an existing deployment meets
		// on its first start after the skills stopped being copied by the
		// entrypoint.
		log.Info("the skills directory already held exactly this set and was adopted rather than rebuilt",
			zap.String("dir", laid.Dir), zap.Strings("skills", laid.Names))
		return nil
	}
	if len(laid.Names) == 0 {
		// A session with no skills is a legal state - deleting the last one
		// deletes the directory, and git keeps no empty directory - but it is also
		// what a mistyped SKILLS_DIR looks like, and that one is silent unless
		// somebody says it out loud.
		log.Warn("the agent was given no skills at all",
			zap.String("dir", laid.Dir), zap.String("read_from", layer.Source))
		return nil
	}
	log.Info("skills laid out for the agent",
		zap.String("dir", laid.Dir), zap.Strings("skills", laid.Names))

	return nil
}

// screenerThresholds reads what a structure must clear out of the declaration.
//
// Parsed strictly and refusing on the first thing it cannot read: the parameters
// are prose for a model, and a threshold that quietly reads zero does not offer
// nothing - it offers EVERYTHING, as though the whole book had qualified. That is
// the dangerous direction, so a missing or unreadable number stops the pass
// instead of loosening it.
func screenerThresholds(declared *declaration.Watcher) (screener.Wanted, error) {
	if declared == nil {
		return screener.Wanted{}, errors.New("there is no declaration to read the screener's thresholds from")
	}
	parameters := declared.Current().Parameters
	number := func(name string) (float64, error) {
		written, ok := parameters[name]
		if !ok {
			return 0, fmt.Errorf("the declaration names no %s", name)
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(written), 64)
		if err != nil {
			return 0, fmt.Errorf("%s is %q, which is not a number", name, written)
		}

		return value, nil
	}

	var (
		wanted screener.Wanted
		err    error
	)
	for _, read := range []struct {
		name string
		into *float64
	}{
		{"screener_nearest", &wanted.MinOutOfTheMoney},
		{"screener_furthest", &wanted.MaxOutOfTheMoney},
		{"screener_least_paid", &wanted.MinCreditToRisk},
		{"screener_most_paid", &wanted.MostCreditToRisk},
		{"screener_dearest", &wanted.MaxCostShare},
		{"screener_most_delta", &wanted.MostDelta},
		{"screener_least_width", &wanted.LeastWidth},
		{"screener_least_edge", &wanted.LeastEdge},
	} {
		if *read.into, err = number(read.name); err != nil {
			return screener.Wanted{}, err
		}
	}

	return wanted, nil
}
