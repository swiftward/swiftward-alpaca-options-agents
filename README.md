# swiftward-alpaca-options-agents

An autonomous agent that sells and buys defined-risk option structures on Alpaca -
credit spreads out of the money, backspreads for the day the market moves hard, and
two event bets - and that runs four trading days a week without anybody at a
keyboard.

**The model decides what to trade. It never decides what it may lose.**

The three ceilings that bound a loss - what one position may lose, what everything betting the same way may lose together, what the whole book may lose - are not in its prompt and not in its file. It asks for them while it works, sizes to the answer, and sees a tightened ceiling on its next turn without a restart. They live where it cannot reach them, and the service its orders pass through is what refuses one that breaches them: the refusal names the rule, and it lands in the record beside the call it stopped. Its own trading numbers are in its declaration, where an operator can read and change them.

A window may name the OCCASION - a company reports on Wednesday, a macro number lands on Friday - because a calendar is not a judgement. What to do about it is the session's: whether there is a trade at all, which structure, which strikes, how large, and whether to sit the window out. Nothing in the declaration says what to buy or sell.

| | |
|---|---|
| **submitted account** | `PA3BXFR0ZVYC`, Alpaca paper - simulated funds, real market data, no real money |
| **result** | **$102,061.24, up 2.06%** from $100,000, at the close of Thursday 3 September, which is the equity Alpaca measures. Four trading days are not a measurement of a strategy - too short to separate skill from a good draw - and the evidence for the strategy is the 646 trading days committed here |
| **the market over the same window** | SPY **+0.76%**, open of 31 August to close of 3 September |
| **the window** | 4 trading days: 31 August, 1, 2 and 3 September |
| **check it yourself** | [alpaca.swiftward.dev](https://alpaca.swiftward.dev) reads the broker live; `make account-claims PAGE=...` checks the trading against these documents, with no credential of ours |
| **what is here** | as much test code as code, and every rule that can refuse a trade has a test that goes red when the rule is removed; 25 of 25 published numbers recompute with no credentials and no network |
| **the risk engine** | the service every order passes through, called over an API: rules declared rather than coded, each carrying how much of itself it discloses; an append-only record of what it refused and why; limits changed on a running agent without a restart. [`docs/architecture.md`](docs/architecture.md) shows where it sits and what it answers |

Built for the Alpaca AI Trading Agents Hackathon, 28 August - 4 September 2026. The
account is measured twice: Alpaca at Thursday's close, above, and lablab again when
submissions close on Friday at 15:00 UTC with the market open - that second number is
not ours to state before it is taken.

Neither number is a backtest and neither is a projection: open the page, or open the
account, and it is the same book.

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
intent before its order, every tool call with its arguments and answer. The five-minute video and the slides are linked from the submission on
lablab.ai. Point a second command at that page and it checks the trading against
what these documents say, with no credential of ours:

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

## What it is

**The agent is a file.** One declaration per agent holds every session with the
reason it exists and the window it may run in, which playbooks that agent may load
at all, and every number it trades on. Each number carries where it came from -
`MEASURED`, `FROM THE RULES` or `PROVISIONAL` - and a test refuses an unmarked one.
Edit the file with the market open and the next session reads it: no restart, no
deploy, no image. Nothing in it says what to buy or sell; a window may name the
occasion - a company reporting, a macro release - because a calendar is not a
judgement.

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
here as code. That is not the point of it: every rule that can refuse a trade has a
test that goes red when the rule is removed, and each was checked that way - the
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
