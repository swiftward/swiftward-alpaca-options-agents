# swiftward-alpaca-options-agents

An autonomous agent that sells and buys defined-risk option structures on Alpaca -
credit spreads out of the money, backspreads for the day the market moves hard, and
two event bets - and that runs four trading days a week without anybody at a
keyboard.

**The model decides what to trade. It never decides what it may lose.**

The three ceilings that bound a loss - what one position may lose, what everything betting the same way may lose together, what the whole book may lose - are not in its prompt and not in its file. It asks for them while it works, sizes to the answer, and sees a tightened ceiling on its next turn without a restart. They live where it cannot reach them: the agent holds a risk-engine address and a token, and no broker key. The engine at that address is what refuses an order that breaches a ceiling - the refusal names the rule, and it lands in the record beside the call it stopped. What holds the route is that address and that credential rather than the network, and `docs/architecture.md` says where a deployment moving real money would put the wall instead. Its own trading numbers are in its declaration, where an operator can read and change them.

A window may name the OCCASION - a company reports on Wednesday, a macro number lands on Friday - because a calendar is not a judgement. What to do about it is the session's: whether there is a trade at all, which structure, which strikes, how large, and whether to sit the window out. Nothing in the declaration says what to buy or sell.

| | |
|---|---|
| **submitted account** | `PA3BXFR0ZVYC`, Alpaca paper - simulated funds, real market data, no real money |
| **result** | **$102,588.74, up 2.59%** from $100,000, at the close of Thursday 3 September, which is the equity the organiser measures. Four trading days can compare entries fairly - everyone has the same four - and cannot tell anyone whether a strategy works. For that there are 646 trading days of option prices committed here, and one command that recomputes what they say |
| **the market over the same window** | SPY **+0.76%**, open of 31 August to close of 3 September |
| **the rule this is measured by** | the organiser: "evaluation based on portfolio's total equity as of EOD Thursday Sep 3rd". The broker records that close in the account's `last_equity`, documented as "Equity as of previous trading day at 16:00:00 ET" - read on Friday, that is Thursday's close: $102,588.74. lablab measures again at Friday 15:00 UTC, and the account goes on trading after both. All four readings and the rule behind each are in `docs/write-up.md` |
| **the window** | 4 trading days: 31 August, 1, 2 and 3 September. Not our choice and not our limit - it is the organiser's measurement window, verbatim: "Monday, August 31 at 9:30 a.m. ET to Friday, September 4 at 9:30 a.m. ET ... evaluation based on portfolio's total equity as of EOD Thursday Sep 3rd". Every entry in this hackathon has the same four days |
| **check it yourself** | the figure is settled at the broker, not here: open the account by the id above, or [alpaca.swiftward.dev](https://alpaca.swiftward.dev), which reads it live through the same process the agent trades through. The account goes on trading after the measured window closes, so the live page moves past the figure above - that is the point of it, and the page names which reading is settled. `make account-claims PAGE=...` then checks the trading against these documents, with no credential of ours |
| **what is here** | as much test code as code, and every rule in this repository that can refuse a trade has a test that goes red when the rule is removed; 25 of 25 published numbers recompute with no credentials and no network |
| **the risk engine** | a service of its own, called over an API, standing on the path every order takes. Its rules are declarations rather than code, each carrying how much of itself it tells the agent, and an operator changes one while the agent runs. It is what refuses an order, and the refusals it wrote are in this repository's record beside the calls they stopped - the engine itself is not, and [`docs/architecture.md`](docs/architecture.md) opens with what that means for each claim here |

Built for the Alpaca AI Trading Agents Hackathon, 28 August - 4 September 2026. The
account is measured twice: the organiser at Thursday's close, above, and lablab when
submissions close on Friday at 15:00 UTC with the market open - that second number is
not ours to state before it is taken.

Neither number is a backtest and neither is a projection: open the page, or open the
account, and it is the same book. Over four sessions a return ranks draws rather
than systems - a genuine edge beats a coin flip only about seven times in ten over
twenty trades, and nobody here had twenty - so what is worth ranking over four days
is what does not move with a good week: whether the worst case was computed before
each order, whether the limits came from somewhere the model cannot edit, and
whether the published numbers recompute on a stranger's machine. All three are
checkable here in minutes.

Twenty-five of the numbers this project publishes recompute from data committed here, with no credentials and no network. Which twenty-five, and which numbers are modelled or come from our own account record instead, is listed in `research/README.md`:

```
$ make claims
PASS  the history covers 646 trading days                        646     646
PASS  1 days to expiry pays 10.72 a trade                      10.72   10.72
PASS  closing at 0.35 of the credit returns 6722                 6722    6722
...
25 claims, 0 failed
```

**Watch it work: [alpaca.swiftward.dev](https://alpaca.swiftward.dev)** - the read
side, serving the submitted account through the same process the agent trades
through: the account, every position, and every order with its legs. `/live` is the
page that moves. The same read side, pointed at an agent whose record we keep, also
serves what that agent did and meant to do - every turn with what woke it, every
intent before its order, every tool call with its arguments and answer. The deck is [`docs/slides.pdf`](docs/slides.pdf) and the video is
[three minutes on YouTube](https://youtu.be/AWgiXKl8ysI). Point a second command at the live page and it checks the
trading against what these documents say, with no credential of ours:

```
make account-claims PAGE=<the page's address>
```

Where to go from here:

- **[`docs/algorithm.md`](docs/algorithm.md)** - how a trade is decided, end to end, and what each control can and cannot stop. Start here.
- **[`docs/capabilities.md`](docs/capabilities.md)** - every capability, where it lives, and what shows it works.
- **[`docs/failures-this-avoids.md`](docs/failures-this-avoids.md)** - the ways an options agent goes wrong, and what this one does instead.
- **[`testbed/`](testbed/README.md)** - the stand that plays the agent conditions the market will not produce on request: thirteen trials, eleven on a staged book and two that displace the real one.
- **[`docs/architecture.md`](docs/architecture.md)** - how the pieces fit together, and how to check every claim it makes.
- **[`research/README.md`](research/README.md)** - which published numbers recompute, which are modelled, and which come from the account.
- **[`docs/api.md`](docs/api.md)** - the five read routes anyone can query without a credential, and what each answers. A copy of all five is committed at [`docs/account-evidence/`](docs/account-evidence/README.md) and checked by `make account-claims DIR=docs/account-evidence`, with no network.

## What it is

**The agent is a file.** One declaration per agent holds every session with the
reason it exists and the window it may run in, which playbooks that agent may load
at all, and every number it trades on. Each number carries where it came from -
`MEASURED`, `FROM THE RULES` or `PROVISIONAL` - and a test refuses an unmarked one.
Edit the file with the market open and the next session reads it: no restart, no
deploy, no image. Nothing in it says what to buy or sell; a window may name the
occasion - a company reporting, a macro release - because a calendar is not a
judgement.

**A new technique is a file, not a release.** A playbook is a `SKILL.md` with a
front matter naming the numbers it needs; a declaration grants it to an agent by
name. The agent lays out exactly the skills its declaration names and refuses to
start when one of them asks for a number the declaration does not give - so a
technique cannot arrive half-configured, and the set an agent carries is narrowed
deliberately, because every skill's description goes into every turn's prompt.
Adding a way to trade is writing a file and naming it; nothing is compiled and
nothing is deployed.

**How it learns is a file, not a model that drifts.** A measurement changes a
number, the number lives in the declaration, and the declaration says where it came
from - `MEASURED` with the script and the date, `FROM THE RULES`, or `PROVISIONAL`
where nobody has measured it yet. The defence rule here was deleted because 672
trades said it lost to doing nothing; the delta ceiling moved from 0.45 to 0.30 on a
grid over 646 days; the backspread placement was rewritten when the sweep behind it
turned out to have chosen its own sample. Every one of those is a diff you can read
and a number you can recompute. Nothing about the agent's behaviour changes without
a line in a file changing first, and that is on purpose: a system that improves
itself in a way nobody can point at cannot be audited after a bad day.

**The agent is in two cages, and both are enumerable.** The logical one is the
ceilings it reads and cannot edit. The physical one is the network: the agent sits
alone on a private network with no route to the internet of its own, and it has
exactly three ways out, each of them a service that records what went through it -
the risk engine for orders and market data, the model gateway for its own thinking,
and a forward proxy for anything else, which answers only for the hosts named in
`docker/egress/filter.txt` and refuses and logs the rest. Widening that list is a
change to this repository, not a decision a session can make, and everything the
agent can reach is a service written down in `compose.yaml` - which is what makes
the reach enumerable rather than merely bounded. `docs/architecture.md` names what
is on that private network beside it, and where a deployment moving real money would
put a wall that configuration cannot.

**It runs on one server, and any server will do.** The whole stack is a compose
file - the agent, its broker server, the record, the page, the migrations, the
egress proxy - so it comes up on a laptop or on the cheapest box a team already
rents. No managed service, no vendor console, nothing to provision. `make local-up`
builds it from this checkout; `make prod-up` runs the same stack from published
images.

**Three things wake it, and it remembers all of them.** The schedule; a person
writing in the chat; and a wake-up the session set for itself - `wake me at 15:45`,
`wake me when SPY trades under 760` - which survive a restart, because a promise
nobody kept is worse than one never made. One conversation runs across all three, so
the session that closes a position remembers opening it. A window may name a cheaper
model for work that does not need the expensive one: the one that only reads the
news does.

**Its limits are read, not told.** The three ceilings that bound a loss are in no
prompt and no file of the agent's. It asks for them while it works, and the risk
engine its orders pass through refuses one that breaches them, naming the rule. Each
rule also carries how much of itself it discloses: a boundary hands over the number,
existence says only that a rule is there. Tell a session the cap and it splits one
order into four; tell it a rule exists and there is nothing to route around.

**Declared, and then guaranteed rather than hoped for.** The rule this agent trades
by is one configuration; anything in it can be declared differently. What does not
change is which half of the work is code: that a window fires when due and is dead
outside the room it was given, that two sessions never hold the account together,
that a wake-up survives a restart, that an agent whose skill needs a number the
declaration does not give refuses to start, that a decided order is walked and
cancelled by the stride and patience declared, that a winner is bought back at the
declared share every thirty seconds with no turn and no model, and that a resting
order breaching a ceiling is cancelled. `docs/algorithm.md` sets the three columns
side by side - what is declared, what the engine guarantees, what the session is
trusted with.

**The model decides what to trade and nothing else.** Pricing six hundred structures
and walking a limit price a cent at a time are arithmetic on a clock - they are code.
Reading the news, judging whether two positions are really one bet, sitting an hour
out: judgement, and the session's. The line is drawn on one question, and
`docs/algorithm.md` opens by naming it.

**The crossing is charged before anything is ranked.** Every pairing pays the cost of
getting in before it is compared with any other - half the bid-ask of both legs,
because an order goes out at the midpoint and is walked toward the book, and half is
what it concedes in expectation, 0.0229 on average over this project's own 34 fills.
Over 646 trading days the same rule with and without that charge differ by more than
the whole strategy earns, and the research behind every threshold charges the full
crossing, so the published expectancy is a floor. What survives is ranked by one
measure weighing what a structure pays against how often it survives - and it keeps
working on expiry day, where the broker computes no delta and most of the money is.

**Filling it is arithmetic, and arithmetic is not the model's job.** The session
names the structure, the size and the worst price it accepts. A ladder walks the
limit toward the book - a cent a step, or the distance left divided by the steps
before patience ends, whichever the declaration says - stops at the price the session
named, and cancels what the book will not take. Before every concession it re-prices
the structure from that pass's quotes and refuses a give that breaks the rule the
entry was made on.

**The thresholds are measurements, and the awkward ones are published.** 646 trading
days of option prices are committed here. One result says the obvious defence -
closing when the price reaches the strike you sold - is worse than doing nothing, by
0.62 a trade, and explains why; the agent trades the exit that measured better
instead. The number behind that better exit is modelled rather than traded, and
`research/README.md` lists which of our published numbers are measured, which are
modelled, and which come from the account, so nobody has to guess which they are
reading. `make claims` recomputes twenty-five of them on your machine in a minute.

**The instrument that questions it is separate from it.** A stand beside the agent
takes the REAL option book and moves one number along a curve, repricing every
contract by its own live implied volatility - at zero displacement it equals the live
market to the cent. Any tool it serves can also be taken away mid-session, because a
tool that goes quiet while the market moves leaves no trace. Thirteen trials have
gone through it, and what they caught is written down, including the defects that
were ours.

## How it is built

**One binary, four roles.** Go, and one program that runs as the harness holding
the clock, the read side serving the page, the session's own tools, or the service
that answers what a caller may do. One image, four things, and a deployment picks
which by naming the roles.

**One chain per account.** Harness, gateway endpoint, Alpaca MCP server, database,
page and credential all carry the same agent's name, so a row in the record, a
refusal and a container in `docker ps` are read as the same agent without a table.
Adding an account is adding a chain rather than editing a setting.

**The gate is one command and it is not optional.** `make check` runs the style
check, an English-only check over every file, the tests, the race detector, and both
builds. Beside it: `make test-db` runs the record's tests against a real Postgres,
and `make test-broker` holds the shapes Alpaca answers in against the real server.
Three GitHub workflows carry it - the gate on every push, the deploy, the published
images.

**Tests as an instrument rather than a decoration.** There is as much test code
here as code. That is not the point of it: every rule in this repository that can
refuse a trade has a test that goes red when the rule is removed, and each was checked that way - the
rule disabled, the test watched to fail, the rule restored. A suite that stays green
when a gate is deleted has measured nothing. The test stand is a separate Go module,
deliberately outside the workspace so it can never share a build with the thing it
questions, and it runs inside the same gate.

**Measured where it mattered.** One MCP session is opened and kept: opening costs
2.68 seconds and a call on an open one 0.85, and a sweep asks about 290 things - so
a session per call turned a four-minute pass into seventeen. The screener walks the
universe in parallel; the profit watch is a thirty-second loop rather than a model
turn; the record is Postgres, one database per account, with the schema applied by a
migration service before anything reads it.

**Everything is written down.** `docs/algorithm.md` takes the trading apart end to
end, `docs/capabilities.md` maps every capability to the code behind it and the test
that holds it, `docs/architecture.md` shows how to check every claim it makes, and
`research/README.md` says which published numbers recompute, which are modelled, and
which come from the account.

## Layout

| Path | What is in it |
|---|---|
| `golang/apps/app` | one binary, four roles: `harness` holds the clock, `api` serves the read side and the built page, `mcp` carries the session's own tools, `envelope` answers what a caller may do |
| `golang/internal` | the packages behind it |
| `typescript/web` | the demo page: what the agent did, what it meant to do, where it was stopped |
| `docker` | one Dockerfile per service, including the egress proxy and its allowlist |
| `postgres/migrations` | schema, applied by the `migrate` service before anything reads it |
| `agent` | what the agent itself reads: its instructions, its skills and the declaration of when it wakes |
| `docs` | the capability index (`capabilities.md`), the submission write-up (`write-up.md`), the architecture and how the stack is deployed |
| `AGENTS.md` | how work is done in this repository, for a person or an agent editing it |

## Running it

```
cp .env.example .env     # fill in the keys
make local-up            # the whole stack, built from this checkout, agents included
```

`make prod-up` runs the same stack from published images. Both bring the agents up; a bare `docker compose up` does not, because the agents sit behind a compose profile.

Before a trading day, and it is worth doing while the market is closed:

```
make rehearse            # send a trading day's reads and print what was refused
```

It goes down the road the screener uses - each agent's own entry point, its own
credential, the same batch size, every caller at once. Reads work at the weekend,
so it answers on a Sunday the question a Monday would ask. What it exists for is
the failure a green test suite cannot see: a limit somewhere on the call path
sized for one caller, met by four.

## Licence

MIT. See `LICENSE`.
