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
	"net/http"
	"os"
	"os/signal"
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
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/execution"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/harness"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/sessiontools"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/telegram"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/volatility"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/wakeup"
)

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

// defaultExecutionStep is one tick on these contracts: the broker quotes them in
// cents, and a step smaller than that is refused.
const defaultExecutionStep = 0.01

func run(log *zap.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// The record outlives the process: a judge opening the page after a restart
	// must still see the week, not an empty screen.
	var pool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		pool, err = db.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
	}

	var state record.Keeper = record.NewMemory()
	if pool != nil {
		kept, err := record.NewPostgres(pool, cfg.RecordShows)
		if err != nil {
			return err
		}
		state = kept
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

	// The declaration is read once: the harness wakes sessions by it, and the
	// session reads it to answer when it will be woken next.
	var declared *declaration.Declaration
	if cfg.DeclarationPath != "" {
		declared, err = declaration.Load(cfg.DeclarationPath)
		if err != nil {
			return err
		}
	}

	// One broker connection serves whichever roles this process runs: the read
	// side asks it for money, the harness for prices, the recorders for both.
	var broker *marketdata.Broker
	if cfg.BrokerMCPURL != "" {
		broker = marketdata.NewBroker(cfg.BrokerMCPURL)
	}

	group, ctx := errgroup.WithContext(ctx)

	if cfg.Has(config.RoleAPI) {
		read := api.Read{
			Record:      state,
			OrdersShown: cfg.OrdersShown,
			HistoryDays: cfg.HistoryDays,
			WebDir:      cfg.WebDir,
			Log:         log.Named("api"),
		}
		if broker != nil {
			read.Broker = broker
		}
		if line != nil {
			read.History = line
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
			DefaultModel: cfg.AgentModel,
			Now:          time.Now,
			Log:          log.Named("harness"),
		}
	}

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

		handler := tools.Handler()
		group.Go(func() error { return serve(ctx, cfg.MCPAddr, handler, log.Named("mcp")) })
	}

	// The ladder finishes what the session started: it can move a price and cancel
	// an order, and it can open nothing.
	if cfg.Has(config.RoleHarness) && cfg.ExecutionEvery > 0 {
		if broker == nil {
			return errors.New("EXECUTION_EVERY is set but there is no broker to walk orders at")
		}
		step := cfg.ExecutionStep
		if step <= 0 {
			step = defaultExecutionStep
		}
		ladder := &execution.Ladder{
			Broker: broker, Every: cfg.ExecutionEvery, Step: step, Record: state,
			Patience: cfg.ExecutionPatience, Now: time.Now, Log: log.Named("execution"),
		}
		log.Info("walking unfilled structures toward the book",
			zap.Duration("every", cfg.ExecutionEvery),
			zap.Float64("step", step),
			zap.Duration("patience", cfg.ExecutionPatience))
		group.Go(func() error { return ladder.Run(ctx) })
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
			return err
		} else if left > 0 {
			log.Warn("turns were left open by an earlier process", zap.Int("turns", left))
		}

		// A call in flight when a process dies may or may not have reached the
		// broker. Saying which would be a guess, so the record says unknown.
		if left, err := state.CloseCallsLeftOpen(ctx, time.Now()); err != nil {
			return err
		} else if left > 0 {
			log.Warn("tool calls were in flight when an earlier process ended",
				zap.Int("calls", left))
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
			h.Declaration = declared
		}

		// The agent is held open for the whole run: that is what lets a person
		// reach work already in progress instead of waiting for it to end.
		{
			client, err := agent.Dial(ctx, cfg.AgentCommand, cfg.AgentCallTimeout, log.Named("agent"))
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			conversation := agent.NewConversation(client, agent.ThreadOptions{
				Model:   cfg.AgentModel,
				Sandbox: cfg.AgentSandbox,
				Dir:     cfg.AgentDir,
			}, cfg.ThreadFile, cfg.AgentCallTimeout)

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
