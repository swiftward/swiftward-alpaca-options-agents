# An options agent that trades inside limits it can read

An autonomous agent trades defined-risk option structures on an Alpaca paper account. A schedule decides *when* it runs and tells it *why*; the agent decides *what* to trade. Everything it did and everything it meant to do is written down as it happens, and the demo page shows both.

## Three contributions, and where each is proven

1. **Limits the agent reads rather than is told.** No risk number the agent sizes
   with appears in its prompt. It asks a service at runtime, is refused by that
   same service, and sees a tightened limit on the next turn without a restart.
   Proof: `golang/internal/envelope`, `tools_test.go` and `percent_test.go`;
   `GET /api/limits` on the page shows what it is told, live.
2. **Every price acted on is a price that could have been got.** Candidates are
   charged the cost of getting in before they are compared with each other, and the
   ladder re-prices a structure before every concession and refuses a give that
   breaks the rule the entry was made on. Proof:
   `golang/internal/screener/candidate.go`, where the crossing is subtracted into
   the number the edge is measured on, and `make claims`, which recomputes the
   measurement saying that charging it or not differs by more than the strategy
   earns.
3. **An instrument that attacks the agent rather than confirming it.** A stand
   beside it takes the real option book and displaces one number along a curve,
   repricing every contract by its own live implied volatility; at zero
   displacement it equals the live market to the cent. Thirteen trials have run
   through it, and what they caught - including a guard of ours that counted legs
   and was wrong from the first backspread - is written down. Proof:
   `testbed/README.md`, `testbed/proxy/overlay.go`.

## The AI logic

The agent is a model session with tools, not a script with a model in it.

It runs four layers, and its declaration says which of them it may open at all: a **premium harvest** that sells vertical credit spreads out of the money; a **convexity** layer that buys backspreads so the day the market moves hard is not a day this account only loses; an **earnings crush** that sells the premium a report has inflated, and only when the measurement says the market is paying more for the move than the company has historically made; and an **event convexity** bet that buys the gap a scheduled macro number opens, outright, on the expiration that dies the same day. The account submitted for judging runs all four (`agent/alpaca-agent-tikhon.yaml`, `skills:`); `docs/algorithm.md` takes each apart.

Its schedule is a declaration, one file per agent in `agent/`:

```yaml
  - name: flatten
    cause: "close everything before the trading day ends"
    at: "15:40"
    within: 10m
    days: [mon, tue, wed, thu, fri]
    cannot_wait: true
    task: |
      First read the intents (read_state). ...
```

`at` and `within` mean *fire at 15:40, and still count as due for 10 minutes* - so a restart at 15:45 does not lose the window, and a restart at 18:00 does not close a book nobody asked to have closed. `every` with `between` fires repeatedly inside one: the defence looks every 30 minutes from 09:40 to 15:55, and the news watch every 30 from 09:35. The submitted declaration opens three entry windows a day rather than a repeating one - 10:20, 12:30 and 14:20, each with its own room to start late. `model` names a cheaper model for a session that only reads the news.

One session at a time holds the agent, because two sessions on one account close each other's positions: a session that comes due while a turn is running waits and tries again a minute later. `cannot_wait: true` is the exception, and the window that empties the book before the bell carries it - waiting past that window is the same as not running at all, so the task is said into the turn already running instead.

Three things wake a session: the schedule, a person writing in the chat, and a wake-up the session set for itself (`wake_me_at`, `wake_me_on_price`). It keeps one conversation across all of them, so the session that closes a position remembers opening it - and the conversation survives a restart, because the thread is kept on disk.

The session carries tools the broker's server does not have:

| Tool | What it answers |
|---|---|
| `record_intent` | states the thesis, the structure and the accepted loss - called *before* any order |
| `read_state` | what earlier sessions did, meant to do, and what came back |
| `read_schedule` | when it will be woken and why, read from the declaration rather than guessed |
| `read_volatility_history` | where today's implied volatility sits in its own recorded history, ranked 0 to 100 |
| `read_candidates` | what the screener's last pass found, with the edge it measured on each structure |
| `score_placements` | where to put the legs of a structure whose worst case sits in the middle: every placement the limits allow, priced at the sides of the book and replayed against that underlying's own history in weather like today's |
| `wake_me_at`, `wake_me_on_price`, `list_wakeups`, `cancel_wakeup` | its own standing requests |

The volatility history is ours: the broker answers what an option costs now, and two of the three entry rules compare today with its own past. A recorder reads the option closest to the money on every watched underlying every few minutes and writes it to `volatility_samples`.

## Risk gates

- **Defined risk only.** Every position's largest possible loss is known before it is opened: a credit spread is bounded by its width less the credit, and an option bought outright by the premium paid. No naked short option is ever opened, and `execution.Unbounded` cancels a resting order whose loss has no floor, on the ladder's next pass. Three shapes exist in `golang/internal/structures` - a credit vertical, a backspread (sell one, buy two: net long, worst case at the bought strike) and a ratio spread - and `shapes_test.go` walks all three, so a fourth cannot be added until every guard says what it does with it. The guard that decides what the profit watch may close asks for two legs with equal quantities sold and bought, which a backspread fails by construction; counting legs alone is what it did until 31 August, and what a backspread cost it.
- **Size comes from the risk engine, not from the prompt.** The agent asks what one position may lose (`position_max_loss`, 10% of equity today), what everything staked on one side of the market may lose together (`same_direction_max_loss`, 35%), and what the whole book may lose (`portfolio_max_loss`, 80%). None of the three is in the declaration, and the declaration says so where a number would otherwise go: they come from the risk engine, and the page reads them live at `GET /api/limits` (`golang/internal/api/limits_test.go`). The working numbers that ARE the agent's own - the delta ceiling, the edge threshold, the day's fuse - are `parameters:` in `agent/alpaca-agent-tikhon.yaml`, each carrying where it came from, and `golang/internal/declaration/provenance_test.go` fails on one that does not. It sizes to nine tenths of the position ceiling, because the ceiling is a share of equity and equity moves while the order rests in the book.
- **Two of the three ceilings are re-checked after the session; the third is not.** The ladder is given `max-loss-per-position` and `max-loss-across-portfolio` and cancels a resting order that breaches either. `same_direction_max_loss` is disclosed to the session and sized to by the session, and nothing after it checks the sum again. On 3 September that mattered, on the same development account: the turn's own intent recorded $18,192 of call-side risk already open and added $9,163 more by its reckoning, while the true worst case of what it added was $20,349 - $38,541 against a side ceiling near $36,600.
- **And the ceiling holds an order, not a fill - which is the case for putting it in the engine.** `execution.WorstCase` prices an order's payoff at every strike parsed from its contracts, and the ladder cancels one that may lose more than a position is allowed. It can only cancel what is still resting. On 3 September, on one of the two DEVELOPMENT accounts rather than the account submitted for judging, a session read a two-wide spread as one wide, sized 119 sets against a $9,298 ceiling at a true worst case of $20,349, and the order filled at once: there was nothing left to cancel. Both halves of our own arithmetic were right and neither was in the path. A limit that lives with the caller is advice however carefully it is computed; the refusal has to belong to the thing the order passes through.
- **Intent before order.** The agent calls `record_intent` with the thesis, the structure and the maximum loss before it orders, and the record carries both, so what was declared can be checked against what was done. It is a rule the agent follows and the record exposes, not a lock on the broker: the order goes to a different server, and an order that skipped the intent would show as a fill with nothing behind it.
- **Where a defence may close is a measurement, not a preference.** The obvious rule - close the spread the moment the price touches the strike you sold - is worse than doing nothing, and `research/exit_rules.py` says why: the price passed the sold strike in 42.7% of trades while only 26.6% ended breached, so that rule pays for 108 crossings that bought nothing, -$1.13 a trade in fees and spread against +$0.51 of genuinely better outcomes. Over 672 trades it returns $2.32 a trade against $2.94 for holding. What IS better than holding is closing once the price is a full width PAST the sold strike, through the bought leg, where the loss is already capped: $3.46 a trade. So the submitted agent's defence looks every thirty minutes, names where the price stands against each pair of strikes, and closes only past the bought strike - never on a touch of the sold one (`agent/alpaca-agent-tikhon.yaml`, session `defend`). A rule that fires on some crossings and misses others is worse than either policy, because the account pays for both and collects neither.
- **And the number behind that exit is modelled, which is said rather than hidden.** The $3.46 comes from repricing a deep in-the-money spread by Black-Scholes at a volatility held constant from entry, because option prices through time exist in our data only in the entry window. Holding to expiry is the one arm that settles on real closes. The rule is used because it is the better of the measured exits and it stays ahead under a 25% volatility stress; the figure behind it is labelled for what it is.
- **Winners are bought back by a watch, not by a turn.** Every thirty seconds a process checks each open structure against the book and closes it once the buy-back costs no more than 0.35 of the credit it was opened for. An agent's turn costs a minute and a half and defence comes round every half hour; a number crossing a line is arithmetic on a clock. Measured on the minute-by-minute path of 597 trades over 646 days: holding to expiry returns $2,461 with 26% losing trades, closing at 0.35 returns $6,722 with 9%. `make claims` recomputes both from `data/buyback_lows.json` - the minutes where the buy-back cost fell to a new low, which is the only thing the rule asks of a path and reproduces every share exactly in 186 KB rather than the 114 MB the full path takes.
- **Nothing else is closed early.** A same-day spread lives on time decay, and closing it early pays the spread twice while collecting half of what it was opened for.
- **A daily halt.** Down 2% from yesterday's close on the submitted account, the entry windows open nothing for the rest of the day. The number sits above the noise of an ordinary day and below what one position may lose, so it measures "the day is bad" and not "one trade is going wrong".
- **Flat by the close, and settled before the result.** The daily window closes what risks assignment - a position whose underlying has come nearer to the sold strike than half the gap between its strikes, with fifty cents as the fallback where that gap is unknown - and lets the rest expire, because buying back something that will expire worthless pays the crossing for nothing. It does not wait for a running turn: a window that empties the book has nowhere to queue. The window before the result is judged is different and deliberately so: it cancels every working order, then closes a position only where the book will give no worse than the broker's mark, and says by how much for each one it leaves. Closing everything would pay the crossing out of the very number being measured.
- **A worst price is held to the rule the entry was made on.** An order names the worst price it accepts, and the ladder walks toward it. Before conceding, the ladder computes what the structure would pay at that price from the quotes of that pass - credit over width less the short leg's delta, the same measure the screener ranks by - and refuses the concession if it falls below the entry threshold. The order keeps the price it was placed at, which cleared the rule; only the concession is refused. An exit is never judged this way: a rule that can cost a fill judges nothing it is unsure of.
- **A position can always be left.** Recording an intent requires the limits to have been read in the same turn, and the agent's own rule forbids an order without an intent - so a risk engine that cannot answer would hold a position open. A closing intent is excused a risk engine that could not ANSWER, but never one that was never CALLED, and the record marks which of the two it was.
- **Every call written down.** `tool_calls` carries the server, the tool, the arguments and the outcome of every call the session made. A call still in flight when a process dies is recorded as `unknown`, never as done: an order in that state may or may not have reached the broker, and the record does not choose.

## Deciding and executing are different jobs

The agent states the structure, the size and the price it wants. A separate module walks that order toward the price the book is showing, a tick at a time, never past it, and cancels what the book refuses. That split is deliberate: a model choosing what to trade is judgement, a limit price moving by a cent is arithmetic on a clock, and each is done by the thing that is good at it. The order is still sent by the session, through Alpaca's own MCP server.

## Alpaca's infrastructure

Orders and market data go through Alpaca's own MCP server - the released `alpaca-mcp-server` package, pinned, unmodified - and it holds the only copy of the account keys. Nothing on the trading path reimplements it or calls Alpaca's REST in its place.

One thing does call REST, and it is not the agent: `research/alpaca.py` pulls historical bars and option snapshots for the measurements under `research/`. It places no order, holds no position and is never imported by the Go binary - the market data API is what serves years of history, and the measurements are what the thresholds rest on.

The risk engine stands in FRONT of that server, not instead of it: the agent calls the gateway, the gateway decides and records, and the call it forwards is an MCP call to Alpaca's own server. One process per account, because that server reads its keys from its own environment and therefore serves exactly one.

What we measured on the account rather than read in a document:

- greeks and implied volatility come from `get_option_snapshot`, and only for contracts with a two-sided quote; the option chain does not carry them;
- index options (SPXW) carry neither on this account tier, while ETF options (SPY, QQQ, IWM) carry both - which is why the agent trades ETF options;
- `get_option_chain` needs `feed=indicative` on this tier;
- a vertical spread is **one** order: `place_option_order` with `order_class=mleg` and a negative limit price for a credit. The agent never sends two orders and never risks half a structure.

## Where the research and the account deliberately differ

The measurements above take the edge threshold that maximises the TOTAL over 646
days, which is `+3`. The judged accounts run `+2`, and the declaration says why
where the number stands (`agent/alpaca-agent-tikhon.yaml`, `min_edge_points`): over 646
days the total is what a threshold is for, and there the peak is +3; over the three
days that are actually scored, the spread of outcomes decides instead, and refusing
to trade settles the result at zero. On the grid priced with each name's own book,
+2 takes 1.00 trades a day for a total of 2,087 and +3 takes 0.85 for 2,146 - three
per cent of the money for seventeen per cent more trades.

So the published expectancy is the expectancy of the threshold we measured, not of
the threshold the account is running this week. Both numbers are in the repository
and the difference is a decision, not an oversight.

## How it is tested

Every rule IN THIS REPOSITORY that can refuse a trade carries a test that FAILS when the rule is removed - the ladder's cancellations, the worst-price re-check, the profit watch's guard on structure shapes. The gateway's own refusals are tested where the gateway lives, which is not here. That is the property worth having: a suite that stays green when a gate is deleted has measured nothing. Each gate above was checked that way - the rule disabled, the test watched to go red, the rule restored.

Beside the code stands an instrument built to attack the agent rather than confirm it, and it is not a backtest: it replays no history and predicts no price.

It works two ways. A staged market takes prices, clock and option book from a file, for the questions a real market will not produce on request - the price arriving at a sold strike, one leg of a spread assigned, a tool that stops answering mid-session. An overlay does the harder thing: every read goes to the REAL broker and one number is displaced along a curve, with each contract repriced from that move by its own live implied volatility. At zero displacement the overlay equals the live market to the cent, which is the property the whole stand rests on. So the agent can be shown a fall that did not happen today, at prices that are otherwise real, and watched.

Either way a fault can be laid on top: any tool the stand serves can be made to answer with a stated message for a stated stretch, because a market that misbehaves is only half of what breaks an agent - the other half is a tool that goes quiet while the market keeps moving, and that half leaves no trace, since nothing crashes. A scenario that would mislead is refused at load rather than run: steps out of order, a price of nothing, a fault naming no tools.

Thirteen trials have gone through it and each says what separates a right answer from a wrong one. What they caught is in `testbed/README.md`, including a guard of ours that decided what to close by counting legs - right while only verticals were held, wrong from the first backspread.

Two defects the suite had passed green were found this week by running this repository's binary against a stand outside it: an execution cadence measuring 45.002 and 89.999 seconds where 45 was declared, and an order that lived nineteen minutes against a patience of eight. Both are now held by tests that go red without the fix - `TestSchedulerJitterDoesNotDecideWhetherAnOrderSteps` and `TestAMissedPassCostsADelayAndNotTheCancellation` in `golang/internal/execution`.

No judged order has ever passed through it. Every order on the submitted account went to Alpaca's own MCP server.

## The result

The account submitted for judging is `PA3BXFR0ZVYC`, and it is measured twice on
two clocks, which is worth stating plainly rather than picking the flattering one.

Alpaca reads total equity at the close of Thursday 3 September. That reading is
settled: **$102,588.74, up 2.59%** from the $100,000 the account opened with.

Against the market it trades: SPY opened the first session of that window at
767.315 and closed the last at 773.115, **+0.76%**, from Alpaca's own market data.
The account returned 2.59% over the same four trading days, and every structure behind
that had a worst case computed before it was opened.

lablab reads it again at the moment submissions close, Friday 15:00 UTC, with the
market open and positions still marking. That number is not ours to state before it
is taken, and the page carries it as blank rather than as a guess in a result's
clothing.

It is checkable in two ways that need nothing from us. The page -
[alpaca.swiftward.dev](https://alpaca.swiftward.dev), and `/live` is the one that
moves - reads the broker through the same process the agent does, so the number on
the screen and the number on the account are one answer. And `make account-claims PAGE=<the page>` turns four
sentences from these documents into checks against that account: every order a
structure rather than a naked leg, every leg declaring whether it opens or closes,
one server behind every order, and no intent recorded knowing its limits had not
been read.

**Four trading days, and that is the contest rather than the strategy.** The
organiser fixed the window: "Monday, August 31 at 9:30 a.m. ET to Friday, September
4 at 9:30 a.m. ET ... evaluation based on portfolio's total equity as of EOD
Thursday Sep 3rd." Four sessions is the most any entry could have, and every entry
has the same four - so the number compares entries fairly and tells nobody whether a
strategy works. On twenty trades an agent with a real edge finishes ahead of a coin
flip most of the time but not reliably, and no amount of writing changes that
arithmetic.

What four days CAN show is whether the machine does what its documents say: whether
the limits were read, whether every order was a structure, whether every intent was
recorded before its order. That is what `make account-claims` checks against the
account, needing nothing of ours.

The strategy's own evidence is not the week. It is 646 trading days of option prices
committed to this repository, priced with the crossing charged at every entry, which
`make claims` recomputes on your machine in a minute. That is the horizon this was
built for; four days is the horizon it was asked to show up in.

The two development accounts in this repository are not submitted and are not the
subject of that figure. The 3 September finding above happened on one of them, and
it is published because a defect in where a limit is enforced is worth more to a
reader than the account it was found on.

## The questions a reader asks, answered before they are asked

**"Four days proves nothing."** Correct, and the window is the organiser's, quoted
above. Every entry has the same four. What the four days settle is whether the
machine behaves as documented, which `make account-claims` checks; what settles the
strategy is 646 days, which `make claims` recomputes.

**"Someone else returned more."** Over four sessions, almost certainly - and over
four sessions a return ranks draws rather than systems. A handful of trades is not
enough to separate an edge from a good week: an agent with a genuine 60% edge
finishes ahead of a coin flip only about seven times in ten over twenty trades, and
nobody here had twenty. That arithmetic applies to the largest number in this field
as much as to ours, and the entry that posted it says so about its own result.

What four sessions CAN rank is what is not luck: whether the worst case of every
position was computed before the order went out, whether the limits came from
somewhere the model could not edit, whether the published numbers recompute on a
stranger's machine, whether the exits were chosen by measurement, and whether the
system says which of its own numbers are modelled. Each of those is checkable here
in minutes, and none of them moves with a good week.

**"How do I know the limits are really enforced?"** By what the engine did rather
than by anyone's word for it. It stands on the path every order takes, and every
call it answered is in `tool_calls` with its arguments and its answer - including
the ones it refused, each naming the rule that refused it, shown on the page beside
the call it stopped. The limits it hands the agent are readable live at
`GET /api/limits`. And the agent has nothing to route around it with: it holds that
engine's address and a token, and no broker key of its own.

**"It is a paper account."** The organiser requires one: "it is required to create a
new paper account with a starting balance of $100,000." Real market data, simulated
funds, and the same for every entry.

**"The model could do anything."** It could decide anything - and what it decides is
bounded by things it does not control: the ceilings come from a service it cannot
edit, the order goes through that service, the ladder walks and cancels without it,
and the profit watch closes without it. `docs/algorithm.md` sets out what is
declared, what the engine guarantees, and what the session is trusted with, in three
columns.

**"Numbers in a README are free."** These are not: twenty-five of them recompute
from data committed here with no credentials and no network, and the ones that
cannot are named as measured, modelled, or from the account, in
`research/README.md`.

## Where to look

- The page, live: [alpaca.swiftward.dev](https://alpaca.swiftward.dev), and `/live` is the one that moves.
- The video, three minutes: [youtu.be/AWgiXKl8ysI](https://youtu.be/AWgiXKl8ysI).
- The deck, thirteen pages: `docs/slides.pdf`.
- The account, `PA3BXFR0ZVYC`, at Alpaca - the id is in the entry.

## How to check any of this

```
$ make claims
...
25 claims, 0 failed
```

`make claims` recomputes twenty-five of the numbers above from data committed to the repository, with no credentials and no network. It does not recompute the 3 September incident, the grid priced with each name's own book, or anything measured against the account - `research/README.md` lists what falls in each of the three groups, so that no number here has to be guessed at. `docs/capabilities.md` names every capability, where it lives, and what shows it works.

The page shows the account, the equity line, open positions, every order with its legs, every tool call with its arguments and outcome, the intents and the turns. It is a read side: it decides nothing and can only read.

Everything it shows comes from Postgres and from the broker, and both are the same sources the agent used.
