# swiftward-alpaca-options-agents

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

Three places to go from here:

- **[`docs/algorithm.md`](docs/algorithm.md)** - how a trade is decided, end to end, and what each control can and cannot stop. Start here.
- **[`docs/capabilities.md`](docs/capabilities.md)** - every capability, where it lives, and what shows it works.
- **[`docs/failures-this-avoids.md`](docs/failures-this-avoids.md)** - the ways an options agent goes wrong, and what this one does instead.
- **[`testbed/`](testbed/README.md)** - the stand that plays the agent conditions the market will not produce on request: thirteen trials, eleven on a staged book and two that displace the real one, and what they caught.

## What is unusual about it

Each of these can be checked rather than taken on trust.

**The agent is a file, and changing what it does is editing that file.** One
declaration per agent holds every session with the reason it exists, the window it
may run in and what it is asked; which playbooks that agent may load at all; and
every number it trades on - the delta ceiling, the edge threshold, the take-profit
share, the day's fuse, how the execution ladder walks. Each of those numbers
carries a mark saying where it came from, `MEASURED`, `FROM THE RULES` or
`PROVISIONAL`, and a test refuses an unmarked one. Nothing in it says what to
trade. Edit it while the market is open and the next session reads the new rule -
no restart, no deploy, no image.

**The model decides what to trade and nothing else.** Pricing six hundred
structures and walking a limit price a cent at a time are arithmetic on a clock,
and they are code. Reading the news, judging whether two positions are really the
same bet, sitting an hour out - those are judgement, and they are the session's.
The line is drawn on one question, and `docs/algorithm.md` opens by naming it.

**Its limits are read, not told.** No ceiling the agent sizes with is written in
its prompt. It asks a service for them at runtime, is refused by that same service
when it tries to exceed them, and sees a tightened limit on its next turn without a
restart.

**The thresholds are measurements, and the awkward ones are published.** 646
trading days of option prices are committed here, priced with the crossing charged
at every entry. One result says the obvious defence - close when the price reaches
the strike you sold - is worse than doing nothing, by 0.62 a trade, and explains
why. The agent does the counter-intuitive thing the measurement points at, and
`make claims` recomputes the measurement on your machine in a minute.

**The agent trades the exit the measurement chose, and the number behind it is
labelled for what it is.** Of three exits measured over 672 trades, closing on a
touch of the sold strike is the worst - 2.32 a trade against 2.94 for doing nothing
- and closing a full width PAST the sold strike, through the bought leg where the
loss is already capped, is the best at 3.46. The defence closes only there. And the
3.46 is modelled rather than traded: option prices through time exist in our data
only in the entry window, so a deep in-the-money spread is repriced by Black-Scholes
at a volatility held constant from entry. `research/README.md` lists which of our
published numbers are measured, which are modelled, and which come from the account,
so nobody has to guess which kind they are reading.

**The crossing is charged before anything is ranked.** The screener walks the
permitted universe in parallel, and every pairing pays the cost of getting in before
it is compared with any other: half the bid-ask of both legs, because an order goes
out at the midpoint and is walked toward the book, and half is what it concedes in
expectation - 0.0229 on average over this project's own 34 fills. That is not
fastidiousness. Over 646 trading days the same rule with and without the crossing
charged differ by more than the whole strategy earns, and the research behind every
threshold charges the FULL crossing, so the published expectancy is a floor rather
than a forecast. What survives is ranked by one measure that weighs what a structure
pays against how often it survives, and it keeps working on the day of expiry -
where the broker computes no delta and most of the money is - by taking the same
quantity from the price of volatility instead.

**Filling it is arithmetic, and arithmetic is not the model's job.** The session
names the structure, the size and the worst price it will accept. From there a
ladder walks the limit toward the book - a cent a step, or the distance left
divided by the steps before patience ends - stops at the price the session named,
and cancels what the book will not take. Before every concession it re-prices the
structure from that pass's own quotes and refuses the give if it falls below the
rule the entry was made on: the order keeps the price that cleared the rule, and
only the concession is refused. A turn of the agent costs a minute and a half; a
limit price moving by a cent has to happen in seconds; each is done by the thing
that is good at it.

**The instrument that questions it is separate from it.** A stand beside the agent
takes the REAL option book and moves one number along a curve, repricing every
contract by its own live implied volatility - at zero displacement it equals the
live market to the cent. Thirteen trials have gone through it, and what they caught
is written down, including the defects that were ours.

## What it does

The agent trades defined-risk options structures on Alpaca. The session holds no broker key and one outward address - the risk engine - and that engine does three things a prompt cannot:

- **states the limit in the tool description** the agent reads, so the agent plans an order it will be allowed to place;
- **refuses in a form a program can act on** - the refusal names the boundary that stopped it, so the agent adjusts instead of retrying;
- **accepts a limit change on a running session** - an operator tightens a ceiling and the live agent sees the new one without a restart.

What holds that route is the agent's configuration and its credential rather than the network: on the demonstration stack the agent and Alpaca's own server share a network, so a session that decided to address it directly could, and `docs/architecture.md` says where that separation would have to be made before real money moved.

One order does not take that path either, and it is named rather than left to be found: the profit watch closes a structure itself, without a model, and `marketdata.CloseStructure` says why at the point where it crosses the boundary. A close can only make the book smaller, so there is nothing for the gateway to refuse - and rather than rest on that reasoning, every leg carries `position_intent` of `buy_to_close` or `sell_to_close`, so the BROKER rejects the order outright if it would open anything.

The gateway is a service of its own, reached at `BROKER_MCP_URL`, and that is what lets it do the three things above: it stands on the path every order takes, it is the only thing that can refuse one, and it keeps the record of what it refused. Its rules are declarations rather than code, each carrying how much of itself it tells the agent, and an operator changes one while the agent is running. `docs/architecture.md` shows where it sits and what it answers.

## How it is built

**One binary, four roles.** Go, and one program that runs as the harness holding
the clock, the read side serving the page, the session's own tools, or the service
that answers what a caller may do. One image, four things, and a deployment picks
which by naming the roles.

**One chain per account.** Harness, gateway endpoint, Alpaca MCP server, database,
page and credential all carry the same agent's name, so a row in the record, a
refusal and a container in `docker ps` are read as the same agent without a table.
Adding an account is adding a chain rather than editing a setting.

**Everything the agent trades on is declared.** The sessions and their windows, the
playbooks it may load, every number with the mark saying where it came from, how the
execution ladder walks. A declaration edited while the market is open reaches the
next session with no restart and no deploy.

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
