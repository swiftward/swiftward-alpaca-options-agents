"""Is +3 too low? Both names, fitted apart, chose +8.

by_name.py fits a delta ceiling and an edge threshold to each name on the first
half of its history and spends them on the second. The ceilings disagreed - SPY
asked for 0.20, QQQ for 0.40 - and that disagreement is what a fitted number looks
like when it is fitting noise. The edge threshold did not disagree: both asked for
+8, against the +3 in force.

So the question worth answering is not whether each name deserves its own numbers.
It is whether the number both names agree on beats the one we ship. That is a
SHARED threshold, tested the same way: chosen on the first half, spent on the
second, on each name separately so neither can carry the other.
"""

from pathlib import Path

import numpy as np
import pandas as pd

import grid

HERE = Path(__file__).resolve().parent
BOOK = {"SPY": 0.06, "QQQ": 0.08}
EDGES = [1, 2, 3, 4, 5, 6, 8, 10, 12]
CAP = 0.30


def main() -> None:
    raw = pd.read_parquet(HERE / "data" / "candidates_bt.parquet")
    print(f"{'edge':>6}{'':>3}", end="")
    for name in ("SPY", "QQQ"):
        print(f"{name + ' 1st':>11}{name + ' 2nd':>11}{name + ' n':>9}", end="")
    print(f"{'both, 2nd half':>17}")

    rows = []
    for edge in EDGES:
        line, spent_all = f"{edge:>+6}{'':>3}", []
        for name in ("SPY", "QQQ"):
            frame = grid.prepare(raw[raw.underlying == name],
                                 slip=BOOK[name], edge_slip=BOOK[name] / 2)
            days = sorted(frame.day.unique())
            cut = days[len(days) // 2]
            early = grid.pick(frame[frame.day < cut], CAP, edge)
            late = grid.pick(frame[frame.day >= cut], CAP, edge)
            line += (f"{(early['pnl'].mean() if len(early) else float('nan')):>11.1f}"
                     f"{(late['pnl'].mean() if len(late) else float('nan')):>11.1f}"
                     f"{len(late):>9}")
            if len(late):
                spent_all.append(late["pnl"])
        both = pd.concat(spent_all) if spent_all else pd.Series(dtype=float)
        line += f"{(both.mean() if len(both) else float('nan')):>17.1f}"
        print(line)
        rows.append((edge, both.mean() if len(both) else float("nan"), len(both),
                     both.sum() if len(both) else float("nan"),
                     grid.drawdown(both.values) if len(both) else float("nan"),
                     (both < 0).mean() if len(both) else float("nan")))

    print()
    print("The mean is not the answer - the TOTAL is. A higher bar pays more per")
    print("trade and takes fewer of them, and those two move against each other.")
    print(f"\n{'edge':>6}{'trades':>9}{'mean $':>9}{'TOTAL $':>10}{'drawdown $':>12}{'in the red':>12}")
    for edge, mean, n, total, dd, red in rows:
        if n < 40:
            continue
        print(f"{edge:>+6}{n:>9}{mean:>9.1f}{total:>10,.0f}{dd:>12,.0f}{red:>11.0%}")

    good = [r for r in rows if r[2] >= 40]
    by_total = max(good, key=lambda r: r[3], default=None)
    ours = next((r for r in rows if r[0] == 3), None)
    if by_total and ours:
        print(f"\nbest by total: {by_total[0]:+.0f} at {by_total[3]:+,.0f} $ "
              f"over {by_total[2]} trades")
        print(f"in force now:  +3 at {ours[3]:+,.0f} $ over {ours[2]} trades")


if __name__ == "__main__":
    main()
