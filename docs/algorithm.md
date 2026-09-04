# How a trade is decided

One page, end to end: what wakes the agent, how a structure is found, what makes
one worth selling, how it is sized, how it is filled, and how it is left. Every
number here is either recomputed by `make claims` or names the file that produces
it. What the pieces are is `capabilities.md`; how they are wired is
`architecture.md`; this is what they do.

## The shape of it: what a model is for, and what it is not for

A trading system built out of a model can go wrong in two opposite ways. Give the
model nothing and it is a chat window with a broker key: it hunts for underlyings
one at a time, prices them by eye, and its judgement is spent on arithmetic. Give
it everything and it is a script with an expensive random number generator in the
middle.

The line here is drawn on one question: **can the answer be wrong in an interesting
way?**

- Walking a limit price a cent at a time until the book takes it cannot. It is
  arithmetic on a clock, it has to happen in seconds, and a turn of the agent costs
  a minute and a half. It is code (`golang/internal/execution`).
- Pricing six hundred structures and ranking them by one measure cannot. It is the
  same formula applied six hundred times, and a session that does it by hand sees
  six of them (`golang/internal/screener`).
- Deciding whether today's news makes a rich structure a trap, whether two open
  positions are really the same bet, whether to sit out an hour - these can. They
  are judgement, and they belong to the model.

So the session is handed a priced, ranked list and a set of limits it reads at
runtime, and it decides. Nothing in our code decides what to trade; nothing in the
model computes what a spread is worth. The autonomy requirement rests on the first
half and the reliability on the second.

## What wakes it

The schedule is a declaration, one file per agent (`agent/alpaca-agent-2.yaml`),
and it is the only place a wake-up time exists. Each session carries a cause in
words - why it was woken - and a task, never an answer.

| Session | When | What it is for |
|---|---|---|
| `open-check` | 09:35, within 20m | what happened overnight, what is held, what expires today |
| `entry` / `entry-call` | every 10m, 09:45-15:15, Mon-Thu | the put side and the call side, separately |
| `defend` | every 15m, 09:40-15:55 | where the price stands against each pair of strikes |
| `earnings-crush` | 15:20 Wed, `earnings-crush-exit` 09:35 Thu | sell the volatility an announcement inflates, buy it back after |
| `flatten-before-the-deadline` | 10:15 Fri, `cannot_wait` | the book is emptied before the result is read |
| `flatten` | 15:35 daily, `cannot_wait` | what risks assignment is closed; what will expire worthless is not |

`cannot_wait` is the exception to one-session-at-a-time: a window that empties the
book has nowhere to queue, so its task is said into the turn already running. A
session can also set its own wake-up - "wake me at 15:45", "wake me when SPY trades
under 760" - and those survive a restart, because a promise nobody kept is worse
than one never made.

## Finding the structures

Every few minutes the screener walks the permitted universe in parallel and prices
every permitted pairing of a sold and a bought leg **at the sides of the book a
real order would cross** - the bid it would be paid and the ask it would pay, never
the midpoint. A caller told "nothing qualifies" is told out of how many: on a
normal pass that is several hundred pairings, and the reasons for refusing each are
tallied by name.

That the crossing is priced rather than assumed is not a detail. Measured over 646
trading days, the same rule priced at the midpoint and priced at the book differ by
more than the whole strategy earns (`make claims`: "the crossing costs more than
the strategy earns without it").

## The one measure: `edge_points`

A credit spread is two bets at once - how much it pays, and how often it survives -
and every simple screen picks one and ignores the other. A delta ceiling keeps what
is far away and throws away what pays. A credit-to-risk floor keeps what pays and
ignores how often it loses.

`edge_points` weighs both halves against each other, in percentage points:

```
edge = P(the sold strike survives)  -  (what it must survive to break even)

     = (1 - |delta|)  -  risk / (credit_after_cost + risk)
```

Positive means the market is paying more for this risk than the same market says
the risk is worth. Both halves come from the same book at the same instant, so it
is a comparison and not a forecast.

Two properties are worth naming:

- **It costs what it costs.** `credit_after_cost` is the credit less the crossing,
  so a structure that pays well and is expensive to enter shows a low edge by
  itself, with no separate threshold on the cost.
- **It works on the day of expiry, where most of the money is.** The broker
  computes no delta on the day a contract expires, so the same quantity is taken
  from the price of volatility instead (`screener.Survival`), and the candidate says
  which of the two it used (`edge_from`). Nothing is left unmeasured because one
  input went missing; and nothing pretends the two are identical.

It is a screen, not a promise, and the code says so where it is defined: delta is a
risk-neutral probability rather than a real one, and a spread does not lose its
whole width when the strike is touched.

## The thresholds, and where each came from

Every number a declaration trades on carries a mark - `MEASURED`, `FROM THE RULES`
or `PROVISIONAL` - and `provenance_test.go` fails on one that carries none. The
measured ones come from 646 trading days of option prices committed to this
repository, priced with the crossing charged at every entry.

| Number | Value | What decided it |
|---|---|---|
| Delta ceiling on the sold leg | 0.30 | a grid over ceiling and edge: 0.30 beat 0.45 everywhere, and at 0.40 the expectation turns negative |
| Edge threshold | +2 | the grid's peak over 646 days is +3; the horizon that is scored here is days, where refusing to trade settles the result at zero, so +2 takes seventeen per cent more trades for three per cent less total |
| Days to expiry | 1-5 | one day to expiry pays 10.72 a trade against 2.29 at five, and the gradient is monotone in between |
| Take-profit share | 0.35 of the credit | on the minute-by-minute path of 597 trades: holding to expiry returns 2,461 with 26% losing; closing at 0.35 returns 6,722 with 9% |
| Defence on the touch | not used | 672 trades: closing when the price touches the sold strike pays 2.32 against 2.94 for holding. The price passed the strike in 42.7% of trades and only 26.6% ended breached, so the defence pays 108 crossings that bought nothing |
| Daily fuse | -5% from yesterday's close | above the noise of an ordinary day, below what one position may lose |

The line worth reading twice is the fifth. The obvious defence - close the spread
when the price reaches the strike you sold - is measurably worse than doing
nothing, and it is measurably worse in a way that explains itself: the account pays
the crossing on every false alarm and collects on none of them. We publish the
measurement, and the agent does the counter-intuitive thing it points at.

There is one exit that beats holding - closing a full width PAST the sold strike,
where the loss is already capped, at 3.46 a trade - and it is **not** in the agent,
because that exit price is modelled rather than historical. The finding is
published and not traded on. That distinction is the whole difference between a
backtest and a system.

## Sizing

The size does not come from the model and does not come from our code. The session
asks the gateway what one position may lose (`position_max_loss`), what everything
betting the same way may lose together (`same_direction_max_loss`) and what the
whole book may lose (`portfolio_max_loss`), and sizes from the answer. No number it
sizes with is written anywhere in its prompt: writing it there would mean the limit
is only as strong as the model's attention.

A set's loss is `(width - the worst credit) x 100`, and the count comes from that.
It sizes to nine tenths of the ceiling, because the ceiling is a share of equity
and equity moves while an order rests in the book.

## Filling it

The session names the structure, the size and the worst price it will accept. From
there the ladder takes over: it walks the limit toward the book, stops at the price
the session named, and cancels what the book will not take before patience runs
out. Two strides are declared - a cent a step, or the remaining distance divided by
the steps left before patience ends.

Before every concession the ladder re-computes what the structure would pay at the
new price, from the quotes of that pass, using the same measure the screener ranked
it by - and refuses the concession if it falls below the entry threshold. The order
keeps the price it was placed at, which cleared the rule; only the concession is
refused. An exit is never judged this way: a rule that can cost a fill judges
nothing it is unsure of.

## Leaving

- **A winner is bought back by a watch, not by a turn.** Every thirty seconds a
  process checks each open structure against the book and closes it once the
  buy-back costs no more than 0.35 of the credit. A turn costs a minute and a half
  and the defence comes round every fifteen minutes; a number crossing a line is
  arithmetic on a clock.
- **A same-day spread is not closed early otherwise.** It lives on time decay, and
  closing it early pays the crossing twice while collecting half of what it was
  opened for.
- **At 15:35 what risks assignment is closed** - a position whose underlying has
  come nearer to the sold strike than the width of the structure - and the rest is
  left to expire, because buying back something that will expire worthless pays the
  crossing for nothing.

## What each control can stop, and what it cannot

This is the part most systems leave out, and it is the part worth reading.

| Control | Stops | Does not stop |
|---|---|---|
| Defined risk by construction | an unbounded loss. Every structure's worst case is known before it is opened and the broker holds the collateral for it | the worst case arriving |
| The gateway's ceilings | an order that would breach a limit, before the broker sees it | a limit that was right and a world that moved |
| The ladder's per-position ceiling | an oversized order **while it is still resting** | an order that fills instantly - it can only cancel what is there to cancel |
| `execution.Unbounded` | an order whose loss has no floor | nothing else: it is a shape check |
| The daily fuse | new risk after the day has gone badly | the risk already in the book |
| The profit watch | giving a winner back | a loser getting worse; that is the defence's job, and the defence is measured to report rather than close |

The third row is the one that cost real money on 3 September, and it is written up
rather than buried: a session read a two-wide spread as one wide, sized 119 sets
against a $9,298 ceiling at a true worst case of $20,349, and the order filled at
once. Both halves of our own arithmetic were right and neither was in the path. A
limit that lives with the caller is advice however carefully it is computed; the
refusal has to belong to the thing the order passes through. That is the change,
and it belongs in the engine rather than in a prompt.

## Why it is built to be checked rather than believed

Every rule above that can refuse a trade carries a test that FAILS when the rule is
removed - the rule disabled, the test watched to go red, the rule restored. A suite
that stays green when a gate is deleted has measured nothing.

Beside it stands a test stand that plays the agent conditions the market will not
produce on request: a staged book for what cannot be arranged, and an overlay that
takes the REAL book and moves one number along a curve, repricing each contract by
its own live implied volatility. At zero displacement the overlay equals the live
market to the cent. Thirteen trials have been run through it, and `testbed/README.md`
lists what they caught - including a guard that decided what to close by counting
legs, which is right while only verticals are held and wrong from the first
backspread.

And the numbers: `make claims` recomputes twenty-five of them from data committed
here, with no credentials and no network. `make account-claims` reads the live page
and checks the trading against what this document says.
