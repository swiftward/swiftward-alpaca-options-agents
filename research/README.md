# Where the numbers come from

The agent trades on numbers. Every one of them is stated in the declarations under `agent/`,
and every one carries a mark saying where it came from: `MEASURED`, `FROM THE RULES`
or `PROVISIONAL`. `golang/internal/declaration/provenance_test.go` fails on a number
that carries none.

This directory is what stands behind the measured ones. It is not the product and
nothing here trades: these scripts read Alpaca, replay history, and print tables.

**You do not have to believe any of it.** From the repository root:

```
make claims
```

It recomputes every number below that can be recomputed and prints each one PASS
or FAIL against what we publish. It needs no Alpaca key and reaches no network:
the structures it reads - `research/data/candidates_bt.parquet`, the 327,634
spreads the rule could have seen over 646 trading days - are committed here for
exactly that reason. A claim that fails there is a claim we have to correct.

The honest summary first, because it is the part that is easy to leave out. Count it
yourself:

```
grep -c MEASURED agent/*.yaml
grep -c PROVISIONAL agent/*.yaml
```

A number nobody has measured is marked `PROVISIONAL` in the declaration, in the
open. We would rather ship a number we can name as a guess than one that looks
measured and is not.

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
standing now, replayed over 464 SPY windows from comparable volatility regimes.

A backspread's worst case sits in the MIDDLE, at the bought strike, at expiration —
the valley. **The gap between the sold leg and the bought ones is what decides, and
the number to read is the median rather than the mean.** A gap of a quarter sigma
ends in the red in 75 to 98 per cent of windows — a median of −$621 with the valley
at 0.5 sigma, −$147 at 1.25 — while half or more of its average comes from the best
one per cent of windows. Widen the gap to half a sigma and the median turns
positive. On 28 August the agent, choosing by eye, sold at 0.57 sigma with the
valley at 1.25, which is in the first group. The declaration had said nothing about
strikes; now it does.

**This number was wrong until 4 September and the correction is worth stating.** The
sweep selected "windows in a regime like today's" with a rolling volatility whose
own window covered the same three days it was measuring — it chose the sample
knowing the answer. Aligned to what a trader holds when the window opens, the
finding changes shape: those placements do not have a negative expectation, they
have a positive mean that is almost entirely tail. The rule the number argues for
did not change.

### The convexity layer's share of equity: 2%

`sweep_backspread.py`, the same sweep. It was 3%, which let the layer hold one and
a half structures at once. Against a worst case near −$1,800 on the placements the
rule allows, there is nothing to pay a second simultaneous bet with: it doubles the
tail without doubling what the tail is being paid for.

### The share of a credit at which a winning spread is bought back: 35%

`collect_paths.py` then `threshold.py` — the minute-by-minute path of all 597
trades the rule picked across 646 days. This is the one number that could not be
measured from outcomes alone: the outcome at expiry was on hand, the path to it was
not, and the whole rule lives in the path.

| | total | trades in the red | worst |
|---|---|---|---|
| hold to expiry | $2,461 | 26% | −$189 |
| **close at 35%** | **$6,722** | **9%** | **−$143** |

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

`exit_rules.py` — 672 trades from January 2024 to August 2026, every exit rule run over
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

What `make claims` recomputes is committed: the structures the rule could have
seen, the daily and 15-minute bars behind them, the entry-window leg prices the
exit rules are walked along, and the buy-back lows the take-profit share is
measured on. What can and cannot be checked from it is the three lists below.

The collectors that produce it are here too: `fetch_options.py` pulls the contracts
that existed and their price in the entry window, `build_candidates.py` turns those
into every structure the rule could have seen, and `collect_exits.py` and
`collect_paths.py` walk the outcomes. They read the market-data key from the `.env`
this repository already uses.

**A rebuild does not land on the committed dataset, and that is why it is
committed.** Run on 4 September against the bars we hold, `build_candidates.py`
produced 323,255 structures over 641 trading days where the committed file has
327,634 over 646 - about one per cent short, and enough to move the per-expiry
means by up to a dollar a trade. An option-bar pull is bounded by dates and by what
Alpaca still lists, and neither is frozen. So the artefact the numbers are computed
from is in the repository, the code that made it is beside it, and `make claims`
checks the numbers against the artefact rather than against a pull nobody can
repeat.

The rest of `data/` is not committed — the minute-by-minute paths file alone is
109 MB. Its collectors rebuild it; `alpaca.py` reads the market-data key from the
`.env` this repository already uses.

## What `make claims` covers, and what it does not

Three lists, so that nobody has to work out which kind of number they are reading.

**Checked by `make claims`, from data committed here.** The twenty-five it prints:
the history's length; the expiry gradient and the figure for each day to expiry;
the per-name figures for SPY, QQQ, IWM and GLD; the delta ceiling of 0.30 against
0.45; that the crossing costs more than the strategy earns without it; the defence
measurement over 672 trades and its three exit rules; and the take-profit
measurement over 597 trades with its totals and its share in the red.

**Reproducible, but MODELLED rather than traded.** Every early-exit result, and
every backspread placement in `sweep_backspread.py`, which prices each structure by
Black-Scholes at the volatility standing now and replays it over historical moves. Option
prices through time exist in our data only in the entry window, so a spread closed
before expiry is repriced by Black-Scholes at the volatility it was entered on, and
the crossing is charged at an assigned 0.10 with a 0.05 fee. Holding to expiry is
the one arm that settles on real closes. The numbers concerned: the 2.32 of closing
on the touch, the 3.46 of closing a width past the strike, and the paths behind the
take-profit share. `make claims` proves the arithmetic reproduces; it does not turn
a model into a fill.

**Neither, and named so that they are not mistaken for either.**

| Number | Where it comes from |
|---|---|
| The 3 September incident in `docs/write-up.md`: 119 sets against a $9,298 ceiling at a true worst case of $20,349 | our own record of the judged account, which is not published |
| "+2 takes 1.00 trades a day for 2,087, +3 takes 0.85 for 2,146" | a grid priced with each name's own book; the cost file behind it is not committed |
| The four observations about this account tier - greeks only on `get_option_snapshot`, no greeks on SPXW, `feed=indicative`, one order for a vertical | measured against the broker on our own account |
| `entry_windows.py` | needs `data/intraday.jsonl`, which is not committed; the entry-window finding stands on the run recorded above rather than on a rerun |
| `gap_bet_analyze.py`, and the event-bet sizing and exit-time results above | needs `data/gap_raw/`, which is not committed |
| The ten-minute non-fill in `docs/architecture.md`, and "every judged order went to Alpaca's own MCP server" in `docs/write-up.md` | our own record and the broker's order list |
| The 34 fills of 26 August and the fee measured from them (`grid.py`) | our own account, watched while cash moved |

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
