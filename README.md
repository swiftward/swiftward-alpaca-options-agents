# swiftward-alpaca-options-agents

**The model decides what to trade. It never decides what it may lose.**

The risk limits are not in its prompt. It asks a policy engine for them while it works, is refused by that same engine when it tries to exceed them, and sees a tightened limit on its next turn without a restart.

Built for the Alpaca AI Trading Agents Hackathon, 28 August - 4 September 2026. Everything runs against Alpaca's paper trading environment: simulated funds, real market data, no real money.

**The submitted account is `PA3BXFR0ZVYC`, and it is measured twice, on two clocks.**
Alpaca reads total equity at the close of Thursday 3 September: that reading is
settled, and it is **$102,061.24 - up 2.06%** from the $100,000 the account opened
with. lablab reads it again when submissions close on Friday at 15:00 UTC, with the
market open, and that number is not ours to state before it is taken.

Neither is a backtest and neither is a projection: open the page linked below, or
open the account, and it is the same book. `make account-claims` checks the trading
on it against what these documents say, and needs no credential of ours.

Over the same four sessions the market it trades on moved a fraction of that: SPY
opened Monday 31 August at 767.315 and closed Thursday at 773.115, **+0.76%**
(Alpaca's own market data, IEX feed - the same source the agent reads). The account
returned 2.06% against it, on structures whose worst case was bounded before each
one was opened.

| | |
|---|---|
| the measured window | 4 trading days: 31 August, 1, 2 and 3 September |
| what SPY did in it | +0.76%, open of the first session to close of the last |
| Go behind it | 15,467 lines, against 19,639 lines of tests |
| tests | 587 test functions in 90 files, and every rule that can refuse a trade has one that goes red when the rule is removed |
| published numbers that recompute | 25 of 25, no credentials, no network |
| trials that attack the agent | 13, against a stand that displaces the real option book rather than simulating one |

Twenty-five of the numbers this project publishes recompute from data committed here, with no credentials and no network. Which twenty-five, and which numbers are modelled or come from our own account record instead, is listed in `research/README.md`:

```
$ make claims
PASS  the history covers 646 trading days                        646     646
PASS  1 days to expiry pays 10.72 a trade                      10.72   10.72
PASS  closing at 0.35 of the credit returns 6722                 6722    6722
...
25 claims, 0 failed
```

**Watch it work: [alpaca.swiftward.dev](https://alpaca.swiftward.dev)** - the page
the agent writes to, reading the broker through the same process the agent does.
What it did, what it meant to do, and where it was stopped; `/live` is the one that
moves. The five-minute video and the slides are linked from the submission on
lablab.ai. Point a second command at that page and it checks the trading against
what these documents say, with no credential of ours:

```
make account-claims PAGE=<the page's address>
```

Three places to go from here:

- **[`docs/algorithm.md`](docs/algorithm.md)** - how a trade is decided, end to end, and what each control can and cannot stop. Start here.
- **[`docs/capabilities.md`](docs/capabilities.md)** - every capability, where it lives, and what shows it works.
- **[`testbed/`](testbed/README.md)** - the stand that plays the agent conditions the market will not produce on request, the thirteen trials it has run, and what they caught.

## What is unusual about it

Four things, and each one can be checked rather than taken on trust.

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

**We found an exit that beats holding, and we do not trade it.** Closing a full
width past the sold strike pays 3.46 a trade against 2.94 for holding, and it holds
up under a 25% volatility stress. It is not in the agent because that exit price is
modelled rather than historical - option prices through time exist in our data only
in the entry window - so the finding is published and not traded on. Knowing which
of your own results you are not allowed to use is the difference between a backtest
and a system.

**Every price it acts on is a price it could actually have got.** The screener
walks the permitted universe in parallel and prices every permitted pairing at the
sides of the book an order would have to cross - the bid it would be paid, the ask
it would pay - never the midpoint. That is not fastidiousness: over 646 trading
days, the same rule priced at the midpoint and priced at the book differ by more
than the whole strategy earns. What survives is ranked by one measure that weighs
what a structure pays against how often it survives, and it keeps working on the
day of expiry - where the broker computes no delta and most of the money is - by
taking the same quantity from the price of volatility instead.

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

The agent trades defined-risk options structures on Alpaca. Every order the SESSION places goes through a policy gateway that does three things a prompt cannot:

- **states the limit in the tool description** the agent reads, so the agent plans an order it will be allowed to place;
- **refuses in a form a program can act on** - the refusal names the boundary that stopped it, so the agent adjusts instead of retrying;
- **accepts a limit change on a running session** - an operator tightens a ceiling and the live agent sees the new one without a restart.

One order does not take that path, and it is named rather than left to be found: the profit watch closes a structure itself, without a model, and `marketdata.CloseStructure` says why at the point where it crosses the boundary. A close can only make the book smaller, so there is nothing for the gateway to refuse - and rather than rest on that reasoning, every leg carries `position_intent` of `buy_to_close` or `sell_to_close`, so the BROKER rejects the order outright if it would open anything.

The gateway itself is a network service, addressed by `BROKER_MCP_URL`, and it is not in this repository. `docs/architecture.md` says what it does and where the boundary between it and this code runs.

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
