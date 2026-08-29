# Where the numbers come from

The agent trades on numbers. Every one of them is stated in the declarations under `agent/`,
and every one carries a marker saying where it came from: **measured**, **from the
rules**, or **assigned**. A test refuses to build if a number carries none.

This directory is what stands behind the measured ones. It is not the product and
nothing here trades: these scripts read Alpaca, replay history, and print tables.

The honest summary first, because it is the part that is easy to leave out:

| | how many |
|---|---|
| measured | 7 |
| from the platform's rules | 1 |
| **assigned, not yet measured** | **8** |

The eight assigned numbers are marked as such in the declaration, in the open. We
would rather ship a number we can name as a guess than one that looks measured and
is not.

## What was measured, and what came out

### The delta ceiling on the sold leg: 0.30

`grid.py` — 646 trading days of SPY and QQQ, a grid over the delta ceiling and the
edge threshold.

The ceiling had been 0.45. Across the grid 0.30 was better everywhere, not at one
point: fewer trades, but enough more expectation per trade to win on the total. At
0.40 the expectation turns negative — selling that close pays less than it costs.

### The edge threshold: at least +3

`grid.py`, the same run. `edge_points` is what a structure pays above what it has
to survive, both halves at once: a delta ceiling alone keeps what is far and throws
away what pays; a credit threshold alone keeps what pays and ignores how often it
loses.

Two candidates were close, +2 and +3. +3 was taken: it peaks on the total and sits
further from the sign change, so a small error in the cost model does not flip it.

### The hours the agent enters at: unchanged, and now for a measured reason

`collect_intraday.py` then `entry_windows.py` — the rule was replayed at every
half-hour slot of the trading day, not only at the three the schedule wakes on.
Delta is not stored in historical bars, so it is recovered from the option's own
traded price by Black-Scholes; against the broker's live delta the recovery is off
by a median of +0.001, and 99% of it lands within 0.05.

| | picks | mean per trade |
|---|---|---|
| inside our three windows | 78 | $11.7 |
| every other half hour | 79 | $12.7 |

The same. **Waking more often would buy more trades, not better ones** — a size
lever, not an edge lever. One slot does stand out: 13:00 is worse (33% of trades in
the red against 23% elsewhere), which is what a thin lunchtime book looks like, and
it is not a window we use.

### Whether more names should be traded: no, on the evidence we have

`fetch_options.py`, `build_candidates.py`, `by_name.py` — the same rule, run over
single names instead of the index funds.

Dollars do not compare across names: a spread on a $200 stock is simply worth more
than one on a $60 one, so everything here is the return on the dollar put at risk.

Of 284 names the screener can return, only four ever clear both filters at all —
SPY on 31% of days, QQQ 25%, IWM 9.5%, TLT 2%. All sixteen single names tried clear
them on 0%.

NVDA was the deciding test, because the reason a single name might fail is
liquidity, and NVDA's book is nearly as tight as SPY's (0.090 against 0.060 in the
same measurement). If it fails, liquidity is not the cause. 19,019 of its
structures were collected over two and a half years, and it does have a cell that
looks excellent — a 0.45 delta ceiling at +6 returns 56.9% of the dollar risked
across 26 trades, and it holds up on the second half of the history.

It does not survive being looked at:

| | |
|---|---|
| trades before the 10-for-1 split of June 2024 | 23 |
| trades in the 26 months after it | 3 |
| of them sold puts, in a name that tripled | 22 of 26 |

**One usable trade a year is not a strategy**, and what is left is a bet on the
direction the stock already went. At our own thresholds NVDA offers four trades in
two and a half years.

There is a second reason not to fit per name. Given 48 cells to choose from, one
wins by luck alone, so each name was fitted on the first half of its history and
then spent on the second. SPY's fitted cell fell from 34.5% to 6.7%; QQQ's held
near its fitted value. **The two names disagree on the ceiling** — SPY picks 0.20,
QQQ 0.40 — which is the signature of fitting noise, not of two different kinds of
market. And a per-name rule would need the agent to know what kind of thing it is
holding; the broker returns `class=us_equity` for every one of them, so it cannot
be told from the data the agent has.

### One thing the totals were hiding: the threshold could be higher

`shared_threshold.py`, `threshold_robust.py` — the edge threshold above was chosen
by the total it earns, and by that measure +3 is right. Read the drawdown beside
it — the deepest the running total ever falls below its own high-water mark — and
the picture changes. On the half of the history the threshold was not fitted on:

| threshold | trades | per trade | total | drawdown |
|---|---|---|---|---|
| +3 | 311 | $3.0 | $920 | −$819 |
| **+8** | **132** | **$6.9** | **$917** | **−$338** |

The same money for less than half the fall. Three checks agree: +8 has the
shallower drawdown in nine of ten quarters; across a thousand reshuffles of the
trade order the totals stay level (2,146 against 2,137) while the median drawdown
separates (−816 against −457); and on SPY alone +8 wins on both counts at once.
Both names, fitted apart, chose +8 — the thing that disagreed between them was the
ceiling that was being fitted, and the thing that agreed was this.

The cost is real and is not hidden: +8 takes fewer than half as many trades, and
fewer trades means the result leans harder on which ones they happen to be.

### Where the legs of a backspread go: sold no nearer than 1.5 sigma, the valley no nearer than 2.5

`sweep_backspread.py` — every placement, priced by Black-Scholes at the volatility
standing now, replayed over 466 SPY windows from comparable volatility regimes.

A backspread's worst case sits in the MIDDLE, at the bought strike, at expiration —
the valley. **Every placement with the valley inside two sigma has a negative
expectation.** On 28 August the agent, choosing by eye, sold at 0.57 sigma with the
valley at 1.25 and was down about seventy dollars a trade the moment the order was
sent. The declaration had said nothing about strikes; now it does.

### The convexity layer's share of equity: 2%

`sweep_backspread.py`, the same sweep. It was 3%, which let the layer hold one and
a half structures at once. At an expectation of +14 dollars a trade and a worst
case of −1,825 there is nothing to pay a second simultaneous bet with: it doubles
the tail without doubling the expectation.

### The share of a credit at which a winning spread is bought back: 35%

`collect_paths.py` then `threshold.py` — the minute-by-minute path of all 553
trades the rule picked across 646 days. This is the one number that could not be
measured from outcomes alone: the outcome at expiry was on hand, the path to it was
not, and the whole rule lives in the path.

| | total | trades in the red | worst |
|---|---|---|---|
| hold to expiry | $2,287 | 26% | −$189 |
| **close at 35%** | **$6,292** | **9%** | **−$143** |

Better on both counts, which is rare — usually more return is paid for with more
risk. The peak is at 35%, the plateau runs 35–40, and it falls away after 50. Each
half of the history agrees. Closing is charged the full book plus fees, twice over,
so an early exit is not made to look free.

### The share of equity on an event bet: 5%

`gap_bet_analyze.py` — 25 employment reports since February 2024, the straddle and
strangle bought the afternoon before and sold in the first hour after.

At 20% of equity the worst historical run costs about 29 thousand dollars. At 5% it
costs a quarter of that and the bet still pays when it pays.

### When to sell an event bet: not before 10:00 and not after 10:30 New York

`gap_bet_analyze.py`, the same run. Selling at 9:35 gives a mean of −7.4%; waiting
to 10:00 turns it positive. The best price of the hour is a ceiling nobody reaches
in life, and it is printed only to show how much is left on the table.

### And one that measured a rule out of existence

`exit_rules.py` — 638 trades over two and a half years, every exit rule run over
the same trades.

Closing when the short strike is touched — the defence as it stood — **loses to
doing nothing.** Most of the loss is not the outcome but the extra crossing of the
book: the defence pays to get out of positions that would have expired worthless
anyway. The number that came out of this is in the declaration; the rule that went
in did not survive.

## Running any of it

```
uv run python grid.py
```

The data lives in `data/` and is not committed — the paths file alone is 109 MB.
The collectors rebuild it from Alpaca; `alpaca.py` reads the keys from the `.env`
this repository already uses.

## What the data does not hold

Each script says this at its top, and it is worth repeating here because it bounds
every number above.

There is **no historical option order book**. What crossing it costs is measured
from the live book and then charged conservatively — the full crossing of both
legs, on top of the trade price rather than the midpoint, which is twice what the
product itself assumes.

There are **no minute bars of the underlying** to full depth, so `exit_rules.py`
walks 15-minute bars: it sees that a strike was touched inside the quarter hour,
but not when.

**Option prices through time** exist only for the trades that were picked, and only
because they were pulled on purpose. Everywhere else the cost of closing is
modelled from the implied volatility at entry, held constant — and a stressed
variant is computed beside it, because a model that never has a volatility jump is
a model that flatters an exit.

Every number above except the two on entry hours rests on **SPY and QQQ**. They are
the only names that clear the filters often enough to leave a history to measure,
which is itself one of the findings — but it means the thresholds are tuned to two
index funds, and nothing here shows they carry to a third.

Windows overlap by a day, so four hundred windows are not four hundred independent
observations. Where that matters — the share of an expectation coming from the best
one percent of history — the scripts say so.
