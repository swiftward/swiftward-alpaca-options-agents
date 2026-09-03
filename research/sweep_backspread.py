"""Where the legs of a call backspread go: every placement, scored on SPY history.

The declaration says NOTHING about strikes today - neither how far out to sell nor
how wide a gap to the bought legs. The agent picks by eye, and on 28 August it put
the valley of maximum loss at 0.91 sigma, the most crowded point of the distribution.

This walks every placement. Each structure is priced by Black-Scholes at the
volatility standing NOW - otherwise the payoffs being compared would rest on
invented credits. The payoff is replayed over historical windows of the same
length, taken only from volatility regimes like today's.
"""

import numpy as np
import pandas as pd

import bs

SPOT = 771.93
DAYS = 3                # trading days to expiry
IV = 0.105              # realised annual volatility now; the options are priced on it
RATE, DIV = 0.04, 0.012
EQUITY = 100_000.0
CAP = 0.02              # ceiling per structure: 2% of equity at its worst case

daily = pd.read_parquet("data/daily.parquet")
spy = daily[daily.symbol == "SPY"].sort_values("date").reset_index(drop=True)
c = spy.c.to_numpy()
ret = np.diff(np.log(c))
vol20 = pd.Series(ret).rolling(20).std().to_numpy() * np.sqrt(252)
moves = c[DAYS:] / c[:-DAYS]
# The volatility that selects a window has to be KNOWN when the window opens, and
# aligning it by hand is where this went wrong once: `vol20[j + DAYS - 1]` covers
# the returns of the very three days being measured, so "a regime like today's"
# was chosen with the answer already in hand and every number below was flattered.
# `vol20[i]` is the standard deviation of `ret[i-19..i]`, and `ret[i]` is the move
# from day i to day i+1 - so the last value a trader holds at the close of day j is
# `vol20[j-1]`. The first window has no history behind it and drops out.
v = np.concatenate(([np.nan], vol20[: len(moves) - 1]))
band = ~np.isnan(v) & (v > IV * 0.75) & (v < IV * 1.25)
moves = moves[band]

years = DAYS / 252.0
sigma = IV * np.sqrt(years)          # the move expected over the term, as a fraction
print(f"windows in regime: {len(moves)}   sigma over {DAYS} days: {sigma:.2%} "
      f"(={SPOT*sigma:.2f} points)")
print()

rows = []
for short_sig in np.arange(0.25, 2.51, 0.25):          # where we sell, in sigmas
    for gap_sig in np.arange(0.25, 2.51, 0.25):        # the gap to the bought legs
        k_short = SPOT * (1 + short_sig * sigma)
        k_long = SPOT * (1 + (short_sig + gap_sig) * sigma)
        width = k_long - k_short
        # The structure: sell one nearer leg, buy two further out.
        cost = 2 * bs.price("call", SPOT, k_long, IV, years, RATE, DIV) \
             - bs.price("call", SPOT, k_short, IV, years, RATE, DIV)
        credit = -cost                       # positive means money came in
        worst = (width - credit) * 100       # worst case per set, in dollars
        if worst <= 0:
            continue
        sets = int(EQUITY * CAP // worst)    # how many sets fit under the ceiling
        if sets < 1:
            continue

        end = moves * SPOT
        per_set = (-np.maximum(end - k_short, 0) + 2 * np.maximum(end - k_long, 0) + credit) * 100
        out = per_set * sets
        top = np.sort(out)[-max(1, len(out) // 100):]     # the best one percent
        rows.append({
            "sell_sigma": short_sig, "gap_sigma": gap_sig, "sets": sets,
            "credit": credit, "valley_sigma": short_sig + gap_sig,
            "mean": out.mean(), "median": np.median(out),
            "in_the_red_pct": 100 * (out < 0).mean(),
            "worst": out.min(),
            "from_top_pct": 100 * top.sum() / len(out) / out.mean() if out.mean() > 0 else np.nan,
        })

t = pd.DataFrame(rows).sort_values("mean", ascending=False)
pd.set_option("display.width", 200)
print("BEST PLACEMENTS BY MEAN")
print(t.head(12).to_string(index=False, float_format=lambda x: f"{x:,.2f}"))
print()
print("WORST")
print(t.tail(5).to_string(index=False, float_format=lambda x: f"{x:,.2f}"))
print()
actual = t[(t["sell_sigma"] <= 0.75) & (t["valley_sigma"] <= 1.5)]
print("NEAREST TO WHAT THE AGENT DID ON 28 AUGUST (sold ~0.57 sigma, valley ~1.25):")
print(actual.head(4).to_string(index=False, float_format=lambda x: f"{x:,.2f}"))
