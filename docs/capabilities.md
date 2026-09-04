# What this system does

One page naming every capability in the repository, where it lives, and what
shows it works. Follow a pointer and you land on the code or on the test that
holds the claim up. Why the pieces are shaped this way is in
`architecture.md`; this page is the map, not the explanation.

The fastest single check is `make claims`: it recomputes twenty-five published
numbers from data committed here, with no credentials and no network.

Two rows below belong to the policy gateway, which is a network service and is not
in this repository. `docs/architecture.md` opens with the line between what a reader
can settle here and what needs a running deployment, and this table keeps that line
in the "Where it lives" column rather than blurring it.

## The agent is a declaration

One file per agent under `agent/`. It says when the agent wakes, why, what it
may load and the numbers it works from. Nothing in it says what to trade.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| A session with a cause | Each session carries a task in words and the reason it was woken; the model decides what to do with it | `agent/alpaca-agent-2.yaml`, `sessions:` | `golang/internal/declaration/declaration_test.go` |
| Time in five forms | `at:` a clock time, `within:` a window it may still start in, `every:` a stride, `between:` the hours it is allowed, `cannot_wait:` for a session that must not be queued behind another | `golang/internal/declaration` | `cannotwait_test.go`, `lateststart_test.go`, `nofridayentry_test.go` |
| Hot reload | An edit to the declaration reaches a running agent without a restart | `golang/internal/declaration`, `watch.go` | `watch_test.go` |
| Skills placed where the agent looks | Which instructions a session may load is declared, not baked into the image; the harness writes them into the agent's working directory where it looks for them | `golang/internal/skills`, `agent/skills/` | `skills_test.go`, `shipped_test.go` |
| The numbers live in the declaration | Ceilings, widths and shares are `parameters:` in the agent's own file | `agent/alpaca-agent-2.yaml`, `parameters:` | `golang/internal/api/limits_test.go` |
| Several agents at once | One chain per broker account: harness, gateway endpoint, MCP server, database, page, credential | `compose.yaml`, `architecture.md` | `golang/internal/config/composekeys_test.go` |
| Provenance of every number | Every parameter in a declaration is marked `MEASURED`, `FROM THE RULES` or `PROVISIONAL`, and an unmarked one fails the test, so a number that cost a day of computing is told apart from one that cost a minute of taste | `agent/alpaca-agent-2.yaml`, `parameters:` | `golang/internal/declaration/provenance_test.go` |

## The harness runs the agent

The clock and the room. It decides when a session works and says why it woke it.
It never decides what to trade, and the autonomy requirement rests on that line.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| The clock | Fires each declared session when it is due, and records which cause fired it | `golang/internal/harness` | `harness_test.go` |
| A turn that produced nothing is a failure | A session that ended having said and called nothing is recorded as a failure rather than counted as a run, so its window is not lost | `golang/internal/harness`, `emptyTurns` | `harness_test.go` |
| Wake-ups a session sets for itself | "Wake me at 15:45", or "wake me when SPY trades under 760". Kept on disk, so a restart does not forget a promise | `golang/internal/wakeup` | `wakeup_test.go` |
| Steering a running turn | A session that is due while another turn is in flight is steered into it rather than dropped, when the declaration says it cannot wait | `golang/internal/harness`, `golang/internal/agent` | `cannotwait_test.go`, `protocol_test.go` |
| A person in the loop | Everything the session says is posted to a room, and what a person writes back reaches the session. The agent holds no channel credential | `golang/internal/telegram`, `golang/internal/mailbox` | `telegram_test.go`, `mailbox_test.go` |
| Crash recovery for orders | After a crash, a call left `unknown` is resolved against the broker: an order that reached it is not sent twice | `golang/internal/reconcile`, `make reconcile` | `reconcile_test.go` |

## Limits live outside the model

A limit is disclosed to the agent and enforced on the path to the broker. The two
halves live in different places, and this table separates them: everything below is
in this repository except the last two rows, which belong to the policy gateway -
a network service the agent reaches at `BROKER_MCP_URL`, described in
`architecture.md` and not published here.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| No broker key in the agent | The agent holds a gateway address and a token, and nothing else; the broker keys are the environment of Alpaca's own MCP server and reach nothing the agent can call | `golang/internal/config`, `compose.yaml` | `envexample_test.go`, `config_test.go` |
| The session reads its limits at runtime | `read_envelope` answers what applies right now and by which version of the rules; no number the agent sizes with is written in its prompt | `golang/internal/envelope/tools.go` | `tools_test.go` |
| One identity per agent | Which limits a caller gets is decided by the bearer token it was started with, and the session never sees that map; neither agent can ask for the other's | `golang/internal/envelope` | `envelope_test.go` |
| A ceiling changed while the agent runs | An operator edits a limit and the next read returns the new one, with no restart | `golang/internal/envelope` | `percent_test.go`, `shipped_test.go` |
| A refusal that names the boundary | The gateway refuses an order and says which rule refused it, in a form the agent acts on rather than retries | the policy gateway | `docs/architecture.md`, and the refusals in `tool_calls` |
| The limit written into the tool description | The gateway states the boundary in the description the agent reads, so it plans an order it will be allowed to place | the policy gateway | `docs/architecture.md` |

## The agent asks what it is allowed to do

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| The skill that tells it to ask | Reading the envelope before sizing is an instruction the agent loads, not a step in our code: the session decides, and the record shows whether it read | `agent/skills/read-my-envelope` | `golang/internal/skills/shipped_test.go` |
| The answer names its own version | A limit read at 09:35 and one read at 14:00 are distinguishable, so a session can tell that the rules moved | `golang/internal/envelope` | `shipped_test.go` |
| An intent requires the read | A closing intent is excused an envelope that could not ANSWER, never one that was never CALLED, and the record marks which | `postgres/migrations/0020_intent_envelope_checked.sql` | `golang/internal/record/record_test.go` |

## Execution

The session decides what to sell, how large, and the worst price it accepts.
Walking the price is arithmetic on a clock, and it belongs in code.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| The ladder | Walks a decided order toward the worst price the session named and stops there. Two declared strides: `tick` moves one cent a step, `arrive` divides what is left by the steps before patience ends | `golang/internal/execution` | `execution_test.go`, `TestTheStrideIsDeclaredAndTheOldOneStillWalksATick` |
| The re-check on every concession | A worst price that would fall below the rule the entry was made on is not walked to, so a walk cannot spend its way out of the reason it opened. An exit is never judged this way | `golang/internal/execution/execution.go` | `TestAWorstPriceBelowTheEntryRuleIsNotWalkedTo`, `TestOnlyAnOrderThatPlainlyOpensIsHeldToTheEntryRule` |
| The per-position ceiling | A resting order that would take the position past its ceiling is cut back | `golang/internal/execution/toobig.go` | `toobig_test.go` |
| The fuse | A day in which the account has fallen by the share the declaration names is over: the fuse refuses further entries | `golang/internal/execution/execution.go`, `Fuse` | `fuse_test.go` |
| One fill, once | A restart in the middle of a walk does not double a fill | `postgres/migrations/0009_fill_once.sql`, `golang/internal/record` | `record_test.go` |

## Getting out

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| The profit watch | Buys a structure back at a declared share of the credit it was opened for, checked every thirty seconds, with no model in the loop | `golang/internal/takeprofit` | `takeprofit_test.go`, `step_test.go` |
| The one order off the gateway's path, declared | The watch places its close itself. A close can only make the book smaller, and every leg carries `position_intent` of `buy_to_close` or `sell_to_close`, so the broker rejects it outright if it would open anything | `golang/internal/marketdata`, `CloseStructure` | `TestEveryLegOfAClosingOrderSaysItCloses`, and it goes red when the intent is flipped to `sell_to_open` |
| A guard that knows the shapes | Every structure a declaration can open has a verdict from the watch; adding a shape fails the test until somebody says what the watch does with it | `golang/internal/structures`, `golang/internal/takeprofit` | `shapes_test.go` |
| Closing windows | `flatten` and `flatten-before-the-deadline` are declared sessions marked `cannot_wait`, so they are not queued behind an entry | `agent/alpaca-agent-2.yaml`, `sessions:` | `cannotwait_test.go` |
| Defence that closes past the bought strike, never on a touch | The defence session names where the price stands against each pair of strikes, and may close only once the price has passed the BOUGHT leg - where the loss is already capped. Closing on a touch of the sold strike is measurably worse than doing nothing, and the declaration carries the run that says so | `agent/alpaca-agent-tikhon.yaml`, session `defend` | `research/exit_rules.py`, `make claims` |
| Expiry handled explicitly | Same-day expiry, the greeks on an expiry day and a watch on what is about to expire are cases with their own answers rather than a default | `golang/internal/marketdata` | `expiryday_test.go`, `expirydaygreeks_test.go`, `expirywatch_test.go`, `sameday_test.go` |

## Finding what to trade

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| The sweep | Walks the permitted universe in parallel and prices every permitted pairing at the sides of the book a real order would cross | `golang/internal/screener`, `golang/internal/placement` | `sweep_test.go`, `parallel_test.go`, `placement_test.go` |
| One measure to rank by | Candidates are ranked by a survival measure rather than by premium, so a rich structure that rarely survives loses to a thinner one that does | `golang/internal/screener/survival.go` | `survival_test.go` |
| Refusals are counted by reason | A pass that offers nothing says out of how many, and tallies why each was refused - no answer, too few contracts, and the rest - into the pass's own log line | `golang/internal/screener/sweep.go`, `Refused` | `sweep_test.go`, `golang/internal/placement/review_test.go` |
| Thresholds read on every pass | What a structure must clear to be offered at all is read from the declaration in force on each pass, and a pass that cannot read them offers nothing rather than falling back to a bound of its own. The values behind them come from the research scripts | `golang/internal/screener/sweep.go`, `Thresholds` | `TestTheThresholdsAreReadOnEveryPass`, `TestAPassWhoseThresholdsCannotBeReadOffersNothing`, `research/threshold.py` |
| Volatility history | A series kept in Postgres because it is worth what its length is | `golang/internal/volatility` | `volatility_test.go`, `postgres_test.go` |
| Account value over time | The line the week is judged on, kept snapshot by snapshot | `golang/internal/account` | `golang/internal/db/account_test.go` |

## The record

Eleven tables. Everything the agent was told, said, called and got back.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| Turns and their causes | Every turn, and what woke it | `turns`, `turn_causes` | `golang/internal/record/record_test.go` |
| Intents | What the session meant to do before it did it, with the underlying's price at the moment it decided | `intents` | `postgres/migrations/0018_intent_underlying_price.sql`, `record_test.go` |
| The agent's own words | What the session said, kept beside what it did | `said` | `postgres/migrations/0017_said.sql` |
| Every tool call | Arguments and answer, in the broker's own words, refusals included | `tool_calls` | `postgres/migrations/0008_tool_call_answer.sql`, `record_test.go` |
| Execution steps | Each rung of a ladder, and what replaced it | `execution_steps` | `postgres/migrations/0019_execution_steps_replaced_by.sql` |
| Candidates | What the sweep found, with the edge it was ranked on and where that edge came from | `candidates` | `postgres/migrations/0015_candidate_edge_from.sql` |
| Account snapshots | The equity line | `account_snapshots` | `golang/internal/db/account_test.go` |
| One record, one account | The record names the account it belongs to, because a process pointed at the wrong database served another account's numbers | `record_account`, `golang/internal/db` | `account_test.go` |
| Reading the record back | The session can ask what it did; the answers are shaped for the protocol rather than copied from the tables | `golang/internal/sessiontools` | `stateanswer_test.go`, `candidates_test.go` |

## The page

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| What the agent did | Every turn with what woke it, what the session said, and each tool call under it | `typescript/web/src/Live.tsx`, `GET /api/state` | `golang/internal/api/api_test.go` |
| Where it was stopped | A turn carries how many of its calls were refused, and each call shows its own status; a failed turn is marked rather than dropped | `typescript/web/src/Live.tsx` | `api_test.go` |
| The money | Equity over time and the day's result | `typescript/web/src/Equity.tsx`, `GET /api/money`, `GET /api/equity` | `api_test.go` |
| The limits in force | What the agent is allowed to do, read live | `GET /api/limits` | `limits_test.go` |
| The read side decides nothing | It serves the record and orders nothing; the page's broker credential cannot place an order | `golang/internal/api` | `api_test.go` |
| A section falls, the page stays | One bad field killed the whole page on 28 August. Now a crash stops at the section and prints the error rather than showing an empty one | `typescript/web/src/Boundary.tsx` | the comment on the class, and the white screen it was written for |
| A public page or a key, never neither | The page refuses to start on an empty key unless it was told to be public, and refuses a key set together with public | `golang/internal/api/key.go` | `key_test.go` |

## Proof

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| `make claims` | Recomputes 25 published numbers from data in the repository. No credentials, no network | `research/claims.py` | run it |
| The research behind each threshold | 646 trading days of option prices, and a script per question: entry windows, exit rules, the take-profit share, the cost of crossing the book | `research/` | `research/README.md`, `make claims` |
| `make day` | The three numbers a trading day is judged by, per account, straight from the record | `postgres/the-day.sql` | run it against a live record |
| `make rehearse` | Sends a trading day's reads from every agent at once and prints what was refused, so a limit sized for one caller and met by four is found before Monday | `golang/apps/rehearse` | run it on a Sunday |
| `make account-claims` | Reads the page a judge already has open - no credential, no broker key - and checks the trading against what these documents say: every order a structure rather than a naked leg, every leg declaring whether it opens or closes, one server behind every order, and no intent recorded knowing its limits had not been read | `tools/account-claims.py` | run it against the deployment |
| `make reconcile` | Answers, for every call left `unknown` that named its order, whether the order reached the broker. One that never carried a name stays `unknown`, because the record does not guess | `golang/apps/reconcile` | `TestAnOrderThatNeverLandedIsSaidSo`, `TestAnOrderWithoutANameStaysUnknown` |

## The test stand

A stand that plays the agent conditions the market will not produce on request.
It holds no broker credential and sits beside the trading path, never on it.

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| A staged market | Prices, clock and option book come from a file: the price reaching a sold strike, one leg left, a tool that stops answering mid-session | `testbed/scenarios/` | `testbed/proxy/scenario_test.go` |
| Taking a tool away mid-session | Any tool the stand serves can be made to answer with a stated message for a stated stretch and normally outside it. A market that misbehaves is half of what breaks an agent; the other half is a tool that stops answering while the market keeps moving, and that half leaves no trace because nothing crashes | `testbed/scenarios/`, `faults` | `testbed/proxy/scenario_test.go` |
| A scenario that would mislead is refused at load | Steps out of order, no start date, a price of nothing, a symbol that is not a contract, a fault with no message, a tool the stand does not serve. Each of those is a test that would never fire while looking like one that passed | `testbed/proxy/scenario.go` | `scenario_test.go` |
| An overlay on the real book | Every read goes to the real broker and one number is moved along a curve; each contract is repriced from its own live implied volatility. At zero displacement the overlay equals the live market to the cent | `testbed/proxy/overlay.go` | `overlay_test.go` |
| Thirteen recorded trials, and five measurements beside them | Each one asks a single question and says what separates a right answer from a wrong one | `testbed/trials/` | `testbed/trials/*/README.md` |
| Two trials that need no agent | Arithmetic on the declaration and on the market's own record | `testbed/trials/defence-corridor.py`, `testbed/trials/hold-or-close.py` | run them |
| Kept out of the agent's build | The stand is a separate Go module, deliberately outside `go.work` | `testbed/Makefile` | `make -C testbed test` |

## The gate

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| `make check` | Style, the language check, the tests, the race detector and both builds, in one command | `Makefile` | run it |
| The English-only check | The repository opens with its whole history, so a line in another language is caught before it is committed | `make english` | run it |
| Tests against a real Postgres | The record's tests run against the database it actually uses | `make test-db` | run it |
| Tests against the broker | The shapes the broker answers in are held up against the real server | `make test-broker`, `golang/internal/marketdata/broker_test.go` | run it |
| Continuous integration | The gate on every push, the deploy, and the published images | `.github/workflows/ci.yml`, `deploy.yml`, `publish.yml` | the workflow runs |

## What it runs on

| Capability | What it does | Where it lives | What shows it works |
|---|---|---|---|
| One binary, four roles | `harness` holds the clock, `api` serves the read side and the page, `mcp` carries the session's own tools, `envelope` answers what a caller may do | `golang/apps/app` | `make build` |
| One chain per account | Harness, gateway endpoint, MCP server, database, page and credential all carry the same agent name, so a row in the record and a container in `docker ps` read as the same agent | `compose.yaml`, `architecture.md` | `golang/internal/config/composekeys_test.go` |
| The egress allowlist | A forward proxy that refuses and logs any host nobody declared | `docker/egress/filter.txt`, `tinyproxy.conf` | the refusals in its log |
| Migrations before anything reads | The schema is applied by the `migrate` service before a reader starts | `postgres/migrations/`, `compose.yaml` | `make migrate` |
| The same stack from published images | `make prod-up` runs what `make local-up` builds | `compose.prod.yaml` | `deploy.md` |
