"""Does +8 beat +3 on drawdown by more than luck?

shared_threshold.py found the two take nearly the same money on the unseen half -
920 dollars against 917 - while +8 does it with a drawdown of 338 against 819. That
is one split of one period, and a drawdown is the noisiest thing a small sample
produces: it is a single worst stretch, so one bad week decides it.

Three checks, and the claim only survives if all three agree.

  1. Every quarter separately. If +8 wins in one quarter and loses in the others,
     the split above found a quarter, not a rule.
  2. A thousand reshuffles of the trade order. Drawdown depends on the ORDER the
     losses arrive in; the same trades dealt differently give a different worst
     stretch. If the gap survives reshuffling, it is in the trades and not in the
     calendar.
  3. Each name alone, so neither carries the other.
"""

from pathlib import Path

import numpy as np
import pandas as pd

import grid

HERE = Path(__file__).resolve().parent
BOOK = {"SPY": 0.06, "QQQ": 0.08}
CAP = 0.30


def taken(frame, edge):
    return grid.pick(frame, CAP, edge)


def main() -> None:
    raw = pd.read_parquet(HERE / "data" / "candidates_bt.parquet")
    frames = {n: grid.prepare(raw[raw.underlying == n], slip=BOOK[n], edge_slip=BOOK[n] / 2)
              for n in BOOK}
    whole = pd.concat(frames.values())

    print("BY QUARTER, both names together")
    print(f"  {'quarter':<10}{'+3 total':>10}{'+3 drawdown':>14}{'+8 total':>10}{'+8 drawdown':>14}")
    stamp = pd.to_datetime(whole["day"])
    wins = 0
    quarters = sorted(stamp.dt.to_period("Q").unique())
    for q in quarters:
        part = whole[stamp.dt.to_period("Q").values == q]
        a, b = taken(part, 3.0), taken(part, 8.0)
        if len(a) < 10 or len(b) < 5:
            continue
        da, db = grid.drawdown(a["pnl"].values), grid.drawdown(b["pnl"].values)
        print(f"  {str(q):<10}{a['pnl'].sum():>10,.0f}{da:>14,.0f}"
              f"{b['pnl'].sum():>10,.0f}{db:>14,.0f}")
        if db > da:
            wins += 1
    print(f"  quarters where +8 had the shallower drawdown: {wins}")

    print("\nRESHUFFLED A THOUSAND TIMES, both names together")
    source = np.random.default_rng(11)
    for edge in (3.0, 8.0):
        out = taken(whole, edge)["pnl"].values
        worst = []
        for _ in range(1000):
            worst.append(grid.drawdown(source.permutation(out)))
        worst = np.array(worst)
        print(f"  {edge:+.0f}: total {out.sum():>8,.0f} $, drawdown median {np.median(worst):>8,.0f} $, "
              f"worst tenth {np.percentile(worst, 10):>8,.0f} $")

    print("\nEACH NAME ALONE, whole history")
    print(f"  {'name':<6}{'+3 trades':>11}{'+3 total':>10}{'+3 dd':>9}"
          f"{'+8 trades':>11}{'+8 total':>10}{'+8 dd':>9}")
    for name, frame in frames.items():
        a, b = taken(frame, 3.0), taken(frame, 8.0)
        print(f"  {name:<6}{len(a):>11}{a['pnl'].sum():>10,.0f}{grid.drawdown(a['pnl'].values):>9,.0f}"
              f"{len(b):>11}{b['pnl'].sum():>10,.0f}{grid.drawdown(b['pnl'].values):>9,.0f}")


if __name__ == "__main__":
    main()
