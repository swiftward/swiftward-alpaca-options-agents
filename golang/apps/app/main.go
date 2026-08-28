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

// startLimit is how long the whole start may take before the process gives up on
// itself. Generous: it covers reaching the database, the broker and the agent.
// A variable rather than a constant so a test can prove the give-up without
// standing there for a minute.
var startLimit = 90 * time.Second

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

	// One broker connection serves whichever roles this process runs: the read
	// side asks it for money, the harness for prices, the recorders for both.
	//
	// Where OUR reads go can differ from where the session's orders go, and the
	// difference is deliberate. The entrypoint writes BROKER_MCP_URL into the
	// session's own MCP config, so that address is the one an order travels; it
	// belongs on the gateway, which is what can refuse an order and what records
	// that it was made. These reads carry no order, so when the gateway holds a
	// client open - measured on 27 August, get_clock unanswered for a minute and
	// a half, and every reader stalled behind it - they may be pointed straight
	// at the broker without giving anything up.
	brokerURL, brokerToken := cfg.BrokerMCPURL, cfg.BrokerMCPToken
	if cfg.HarnessBrokerMCPURL != "" {
		brokerURL, brokerToken = cfg.HarnessBrokerMCPURL, cfg.HarnessBrokerMCPToken
	}

	var broker *marketdata.Broker
	if brokerURL != "" {
		broker = marketdata.NewBrokerWithToken(brokerURL, brokerToken).ActingFor(cfg.UserHeader, cfg.UserToken)
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
		// Пределы отдаются из ТОГО ЖЕ файла и тем же вызовом, каким на них
		// отвечают агенту. Иначе страница показывала бы пересказ, который однажды
		// разойдётся с тем, по чему на самом деле торгуют.
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
			SayEvery:     cfg.SayEvery,
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
		if shortlist != nil && len(cfg.ScreenerUnderlyings) > 0 {
			tools.Shortlist = shortlist
			// The interval travels with the list so the list can say whether it is
			// fresh. Without it a session is handed an age in seconds and has to
			// guess what is normal - and guessing cost a trade on 27 August.
			tools.SweepEvery = cfg.ScreenerEvery
		}
		if shortlist != nil {
			tools.Asked = shortlist
		}
		// Куда ставить ноги конструкции, у которой худший случай в СЕРЕДИНЕ.
		// Скринер отвечает на другую половину вопроса - какая бумага из всех, - и
		// его мера годится только для вертикали, у которой выплата монотонна.
		// Требует брокера: цена сейчас, книга на одну экспирацию и дневная история
		// самой бумаги. Без него инструмент просто не предлагается.
		if broker != nil {
			// Календарь биржи, а не машины. Команда работает из UTC+8, и по
			// местной дате вечер четверга в Нью-Йорке - уже пятница: до пятничной
			// экспирации не остаётся ни дня, и инструмент отказывал бы весь
			// американский вечер. Ошибка в другую сторону тише и хуже - на день
			// меньше срока делает сигму короче, а все расстояния в сигмах длиннее,
			// и нога ближе разрешённого проходит как разрешённая.
			exchange, err := time.LoadLocation("America/New_York")
			if err != nil {
				return fmt.Errorf("load the exchange calendar: %w", err)
			}
			tools.Placements = placement.Scorer{
				Market: broker,
				Where:  exchange,
				// Три года. Отбор по режиму волатильности выбрасывает большую
				// часть: два года дали 216 пригодных окон против 466, на которых
				// стоял тот же замер в исследовании. Число без выборки под ним
				// хуже отсутствия числа.
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
			Wanted: screener.Wanted{
				MinOutOfTheMoney: cfg.ScreenerNearest, MaxOutOfTheMoney: cfg.ScreenerFurthest,
				MinCreditToRisk: cfg.ScreenerLeastPaid, MostCreditToRisk: cfg.ScreenerMostPaid,
				MaxCostShare: cfg.ScreenerDearest, MostDelta: cfg.ScreenerMostDelta,
				LeastEdge: cfg.ScreenerLeastEdge,
			},
			Every: cfg.ScreenerEvery, Keep: cfg.ScreenerKeep,
			PerMinute:   cfg.ScreenerPerMinute,
			Workers:     cfg.ScreenerWorkers,
			Expirations: cfg.ScreenerExpirations,
			Now:         time.Now, Log: log.Named("screener"),
		}
		log.Info("pricing the universe",
			zap.Int("underlyings", len(cfg.ScreenerUnderlyings)),
			zap.Duration("every", cfg.ScreenerEvery),
			zap.Int("per_minute", cfg.ScreenerPerMinute),
			zap.Int("workers", max(cfg.ScreenerWorkers, 1)))
		group.Go(func() error { return sweep.Run(ctx) })
	}

	// Сторож прибыли. Ходит без ИИ и умеет ровно одно - УМЕНЬШИТЬ книгу.
	//
	// Разделение то же, что у лестницы, перенесённое со входа на выход: решать,
	// что продать и по какой цене, - суждение и принадлежит модели; смотреть на
	// число раз в полминуты и действовать, когда оно пересекло черту, -
	// арифметика по часам. Ход агента стоит полторы минуты, защита ходит раз в
	// тридцать, и 28 августа спред QQQ отдал три четверти кредита незамеченным.
	if cfg.Has(config.RoleHarness) && cfg.TakeProfitAt > 0 {
		if broker == nil {
			return errors.New("TAKE_PROFIT_AT is set but there is no broker to watch the book with")
		}
		// This watch SENDS ORDERS, so it goes to the gateway even though every
		// other harness call may be pointed straight at the broker. `broker`
		// above follows HARNESS_BROKER_MCP_URL, which exists for reads: a read
		// carries no order and loses nothing by skipping the gateway. A close
		// does. Sent around the gateway it would be absent from the record - the
		// entries visible and the exits not, which is exactly where this
		// strategy makes its money - and no rule could refuse it.
		// И отказываемся стартовать, если адреса шлюза нет. Без него сторож
		// поднялся бы, написал в журнал "watching for structures worth closing" и
		// не закрыл бы НИЧЕГО - каждый выкуп падал бы в лог и только. Это тот же
		// тихий отказ, от которого выше защищает нулевая доля, и он опаснее:
		// доля видна в настройках, а мёртвый сторож выглядит живым.
		if cfg.BrokerMCPURL == "" {
			return errors.New("TAKE_PROFIT_AT is set but BROKER_MCP_URL is empty: " +
				"the watch sends orders and they go through the gateway, so without its " +
				"address it would run and close nothing")
		}
		closer := marketdata.NewBrokerWithToken(cfg.BrokerMCPURL, cfg.BrokerMCPToken).
			ActingFor(cfg.UserHeader, cfg.UserToken)
		watch := &takeprofit.Watch{
			Broker: closer, At: cfg.TakeProfitAt, Every: cfg.TakeProfitEvery,
			Now: time.Now, Log: log.Named("takeprofit"),
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
			client, err := agent.Dial(ctx, cfg.AgentCommand, cfg.AgentCallTimeout, log.Named("agent"))
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
