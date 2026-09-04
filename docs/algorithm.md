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

## Four layers, and which of them the judged account runs

The credit spread below is one layer of four, and a declaration says which of them
an agent may open at all - `skills:` names the playbooks, and a playbook whose
numbers the declaration does not supply refuses to start rather than inventing one.

| Layer | What it is | What it is for |
|---|---|---|
| **Premium harvest** | a vertical credit spread, sold out of the money, 1-5 days to expiry | the engine of the book: many small credits, each with a known worst case |
| **Convexity** | a backspread - sell one nearer option, buy two further out | so that the day the market moves hard is not a day this account only loses |
| **Earnings crush** | sell the premium a company's report has inflated, on the expiration that survives the report | the one place selling into an event is allowed, and only when the measurement says the market is paying more for the move than the company has historically made |
| **Event convexity** | buy the gap a scheduled macro number opens, outright, on the expiration that dies the same day; entered the afternoon before and sold into the open | the uncapped right tail, and the reason the rest of the book is kept modest. It burns more often than it pays, and that is the shape chosen rather than a defect |

The account submitted for judging runs all four (`agent/alpaca-agent-tikhon.yaml`,
`skills:`). The other two declarations in this repository run narrower sets - one
adds convexity to the harvest, the other harvests and sells the crush - because two
accounts on one playbook, differing in one number, is how a number gets settled by
the market rather than by an argument.

Everything from here to "Leaving" describes the harvest layer, which is where the
measurements are. The other three carry their own rules in `agent/skills/`, each
with the numbers it requires declared in its header.

## What wakes it

The schedule is a declaration, one file per agent, and it is the only place a
wake-up time exists. Each session carries a cause in words - why it was woken - and
a task, never an answer. This is the submitted account's, `agent/alpaca-agent-tikhon.yaml`,
and that file is the source of truth for every time and number below.

| Session | When | What it is for |
|---|---|---|
| `open-check` | 10:00, within 2h | what happened overnight, what is held, what expires today |
| `entry-morning` / `entry-midday` / `entry` | 10:20, 12:30 and 14:20, Mon-Thu | three entry windows, each with its own room to start late |
| `convexity` | every 2h, 09:50-15:00, Mon-Thu | the layer that pays when the market moves hard |
| `defend` | every 30m, 09:40-15:55 | where the price stands against each pair of strikes, and what that is worth closing |
| `news-watch` | every 30m, 09:35-15:45 | what has come out, on a cheaper model |
| `earnings-crush` | 15:20 Wed, `earnings-crush-exit` 09:35 Thu | sell the volatility a report has inflated, buy it back after |
| `event-convexity-enter` | 15:30 Thu, `event-convexity-exit` 09:35 Fri | buy the gap a scheduled number opens, sell it into the open |
| `deadline-flatten` | 10:35 Fri | the book is settled before the result is read |
| `flatten` | 15:40 daily, `cannot_wait` | what risks assignment is closed; what will expire worthless is not |

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

- **It costs what it costs.** `credit_after_cost` is the credit less HALF the
  crossing, so a structure that pays well and is expensive to enter shows a low
  edge by itself, with no separate threshold on the cost. Half rather than the
  whole because that is what the book actually took: measured over this project's
  own 34 fills on 26 August, 0.0229 was conceded on average against the price first
  asked, and 20 of the 34 filled at that price exactly. The research charges the
  FULL crossing instead, which is the conservative direction and is why the
  measured expectancy is a floor rather than a forecast.
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
| Where a defence may close | past the BOUGHT strike, never on a touch of the sold one | the same script, `research/exit_rules.py`, measured both: closing on a touch of the sold strike is worse than doing nothing, and closing once the price is through the bought leg - where the loss is already capped - is better than either. The declaration carries the figures its own run produced |
| Daily fuse | -2% from yesterday's close on the submitted account | above the noise of an ordinary day and below what one position may lose. The number is the account's own, and the declaration says what it was computed from |

The line worth reading twice is the fifth. The obvious defence - close the spread
when the price reaches the strike you sold - is measurably worse than doing
nothing, and it is measurably worse in a way that explains itself: the account pays
the crossing on every false alarm and collects on none of them. We publish the
measurement, and the agent does the counter-intuitive thing it points at.

The exit that beats holding is the one the agent uses: closing a full width PAST the
sold strike, where the loss is already capped, at 3.46 a trade. Its number is
modelled rather than traded - option prices through time exist in our data only in
the entry window, so a deep in-the-money spread is repriced by Black-Scholes at a
volatility held constant from entry - and it is labelled as modelled everywhere it
appears. Holding to expiry is the one arm that settles on real closes.

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
  and the defence comes round every half hour; a number crossing a line is
  arithmetic on a clock.
- **A same-day spread is not closed early otherwise.** It lives on time decay, and
  closing it early pays the crossing twice while collecting half of what it was
  opened for.
- **At the close, what risks assignment is closed** - a position whose underlying
  has come nearer to the sold strike than the width of the structure - and the rest
  is left to expire, because buying back something that will expire worthless pays
  the crossing for nothing.
- **Before the result is read, the book is SETTLED rather than emptied.** Every
  working order is cancelled first, because an order that fills a minute before the
  cut changes the result at random and nobody decided that. Then each position is
  weighed against two numbers, both named out loud: what the broker marks it at, and
  what the worst side of the book would actually give for it. No worse than the mark
  - close it, that is free. Noticeably worse - leave it, and say by how much. Closing
  turns a mark into cash and pays the crossing for the privilege; measured on a live
  pair on 27 August, the broker marked it at -300 and closing it right then cost
  -500. A clean account is bought with the money we are judged on.

## What each control can stop, and what it cannot

This is the part most systems leave out, and it is the part worth reading.

| Control | Stops | Does not stop |
|---|---|---|
| Defined risk by construction | an unbounded loss. Every structure's worst case is known before it is opened and the broker holds the collateral for it | the worst case arriving |
| The gateway | a tool call it is not willing to forward, before the broker sees it, naming the rule that refused it | a limit that was right and a world that moved. What it enforces is its own; this repository shows what it DISCLOSES |
| The ladder's per-position ceiling | a resting order that has grown too large for the account it sits on | an order that fills instantly - it can only cancel what is there to cancel |
| The ladder's portfolio ceiling | the same, for everything held together | the same |
| The side ceiling, `same_direction_max_loss` | nothing in this code. It is disclosed to the session and the session sizes to it | itself: unlike the other two it is **not** wired into the ladder, so nothing here re-checks it after the session has done its arithmetic. That is the gap 3 September ran into, and it is named here rather than left to be found |
| `execution.Unbounded` | an order whose loss has no floor | nothing else: it is a shape check |
| The daily fuse | new risk after the day has gone badly | the risk already in the book |
| The profit watch | giving a winner back | a loser getting worse; that is the defence's job, and the defence is measured to report rather than close |

Two of those rows cost money on 3 September - on one of the two development
accounts in this repository, not on the account submitted for judging - and they
are written up rather than buried, because the finding is worth more than the
account it happened on.

A session read a two-wide spread as one wide, sized 119 sets against a $9,298
ceiling at a true worst case of $20,349, and the order filled at once. Both halves
of our own arithmetic were right - the sizing used the wrong width, and
`execution.WorstCase`, which parses the strikes out of the contracts rather than
trusting anyone's number, had it right - and neither was in the path of the order.

The same turn's own intent shows the second: it recorded $18,192 of call-side risk
already open and added $9,163 more by its own reckoning, which it judged to be
inside the side ceiling. At the true worst case the sum is $38,541 against a side
ceiling near $36,600, so it was outside. Nothing after the session re-checked that
sum, because the side ceiling is the one limit the ladder is not given.

The conclusion is the same for both, and it is a design conclusion rather than a
bug report: **a limit that lives with the caller is advice, however carefully it is
computed. The refusal has to belong to the thing the order passes through** - which
means all three ceilings enforced synchronously by the gateway, before
`place_option_order` is forwarded, rather than re-checked afterwards by the thing
that walks the price.

## The practices this is built on, and why each one is there

None of these is clever. Each is what a desk does, and each is here because the
alternative has a name and a cost.

**Size from the loss, never from the premium.** A position is sized from what it
can lose at expiry - `(width - the worst credit) x 100` a set - rather than from
what it collects. Sizing from the credit is how an account discovers that twenty
small winners were funded by one loser it could not carry.

**One order for a structure, never two.** A vertical goes as a single
`place_option_order` with `order_class=mleg` and a negative limit price for a
credit. Two orders means a window in which half a structure exists, and half a
credit spread is a naked short.

**The crossing is charged, not assumed away.** A candidate's credit is taken at the
midpoints, and then the crossing is subtracted before anything is ranked - half of
it, because an order goes out at the midpoint and is walked toward the book, and
half is what it concedes in expectation. That half is measured rather than guessed:
over this project's own 34 fills on 26 August, 0.0229 was conceded on average
against the price first asked. The research behind the thresholds charges the FULL
crossing instead, which is the conservative direction, so the measured expectancy is
a floor rather than a forecast. A screen that ranks on raw midpoints is ranking
trades nobody could have done.

**The worst case before the order, not after the fill.** `execution.WorstCase`
prices an order's payoff at every strike parsed out of its own contracts, so the
number is the contracts' and not the caller's arithmetic. `execution.Unbounded`
refuses a shape whose loss has no floor at all.

**A ceiling on the position, on the side, and on the book.** Three limits rather
than one, because twenty positions each inside its own ceiling still put the whole
account at risk, and because everything betting the same way loses together - which
is exactly what happened here on 3 September.

**A fuse on the day, not only on the trade.** Below a declared share of yesterday's
close, entries stop for the rest of the day. It measures "the day is bad" rather
than "this trade is going wrong", and it does not touch the defence or the profit
watch: the book is still managed, only new risk stops.

**Intent before order, in the record.** The thesis, the structure and the accepted
loss are written down before anything is sent, so what was declared can be checked
against what was done. A fill with nothing behind it is visible rather than
deniable.

**A winner is closed by a clock, not by a turn.** A model turn costs a minute and a
half and the defence comes round every half hour; a buy-back crossing a line
is arithmetic, so a process checks it every thirty seconds.

**Settled, not flattened, before the result is read.** The organiser measures the
account at a moment, and a position open at that moment enters at its mark. So the
declared window cancels every working order - an accidental fill at the cut is a
result nobody chose - and then closes only what the book will take at no worse than
the mark. Closing everything for the look of a clean account pays the crossing out of
the number being judged.

**Every call written down, including the ones still in flight.** A call whose
answer never arrived is recorded as `unknown` rather than as done, and `make
reconcile` asks the broker afterwards which it was. An order in that state may or
may not have reached the broker, and the record does not choose.

**One credential per agent, and the page's cannot trade.** The record names which
agent made each call; one agent can be stopped without touching the other; and the
surface a stranger can open holds a credential that can read and cannot order,
because a page that could trade is a page whose leak is a trade.

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
