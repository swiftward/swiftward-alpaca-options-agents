"""Do the thresholds transfer from index ETFs to single stocks?

Every measured number in the declaration was found on SPY and QQQ. The screener
prices 284 underlyings. On the recorded sweeps only SPY, QQQ and IWM ever clear
both filters at once - delta at most 0.30 and an edge of at least +3 - and every
single stock clears them zero times out of a hundred and more tries each.

Two readings fit that, and they call for opposite actions. Either single stocks are
genuinely worse and the universe should shrink to what is traded; or the threshold,
tuned on quiet indices, does not fit names whose tails are fatter, and a threshold
of their own would open them.

Each name is charged ITS OWN crossing cost, measured from the live book
(cost_by_name.py). Charging SPY's 0.06 to a name whose book is 1.81 would answer a
question about a market that does not exist.
"""

import json
import sys
from pathlib import Path

import numpy as np
import pandas as pd

import grid

HERE = Path(__file__).resolve().parent
DELTAS = [0.20, 0.25, 0.30, 0.35, 0.40, 0.45]
EDGES = [-2, 0, 1, 2, 3, 4, 6, 8]


def prepared(frame: pd.DataFrame, book: float) -> pd.DataFrame:
    """grid.prepare with this name's own book width rather than the shared one."""
    return grid.prepare(frame, slip=book, edge_slip=book / 2)


def main() -> None:
    costs = json.loads((HERE / "data" / "cost_by_name.json").read_text())
    source = HERE / "data" / (sys.argv[1] if len(sys.argv) > 1 else "candidates_names.parquet")
    raw = pd.read_parquet(source)
    print(f"structures {len(raw):,}, underlyings {raw.underlying.nunique()}, "
          f"{raw.day.min()} .. {raw.day.max()}\n")

    # Dollars do not compare across names: NVDA's strikes are wider than SPY's, so
    # one of its spreads is simply worth more. Return on the dollar risked does
    # compare, and it is what sizing turns into money.
    print(f"{'name':7}{'book':>7}{'at 0.30/+3':>12}{'per $ risked':>14}"
          f"{'best cell':>12}{'its per $':>11}{'its trades':>12}")
    summary = []
    for name in sorted(raw.underlying.unique()):
        book = costs.get(name)
        if book is None:
            print(f"{name:7} no measured book, skipped")
            continue
        frame = prepared(raw[raw.underlying == name], book)
        days = frame.day.nunique()

        ours = grid.pick(frame, 0.30, 3.0)
        ours_mean = ours["pnl_over_risk"].mean() if len(ours) else float("nan")

        best = None
        for cap in DELTAS:
            for edge in EDGES:
                took = grid.pick(frame, cap, edge)
                if len(took) < 20:          # fewer than twenty is not a conclusion
                    continue
                mean = took["pnl_over_risk"].mean()
                if best is None or mean > best[0]:
                    best = (mean, cap, edge, len(took))

        cell = f"{best[1]:.2f}/{best[2]:+.0f}" if best else "-"
        print(f"{name:7}{book:>7.2f}{len(ours):>12}{ours_mean:>13.1%}"
              f"{cell:>12}{(best[0] if best else float('nan')):>10.1%}"
              f"{(best[3] if best else 0):>12}")
        summary.append((name, book, len(ours), ours_mean, best))

    print("\nDOES A NAME'S OWN BEST CELL SURVIVE THE SECOND HALF?")
    print("  Forty-eight cells are tried per name, so one of them wins by luck alone.")
    print("  The test: fit on the first half of the history, then spend the second.")
    print(f"  {'name':7}{'fitted cell':>13}{'fitted half':>14}{'spent half':>15}"
          f"{'ours 0.30/+3 there':>20}")
    for name in sorted(raw.underlying.unique()):
        book = costs.get(name)
        if book is None:
            continue
        frame = prepared(raw[raw.underlying == name], book)
        days = sorted(frame.day.unique())
        cut = days[len(days) // 2]
        early, late = frame[frame.day < cut], frame[frame.day >= cut]

        fitted = None
        for cap in DELTAS:
            for edge in EDGES:
                took = grid.pick(early, cap, edge)
                if len(took) < 15:
                    continue
                mean = took["pnl_over_risk"].mean()
                if fitted is None or mean > fitted[0]:
                    fitted = (mean, cap, edge)
        if fitted is None:
            continue
        spent = grid.pick(late, fitted[1], fitted[2])
        ours = grid.pick(late, 0.30, 3.0)
        print(f"  {name:7}{f'{fitted[1]:.2f}/{fitted[2]:+.0f}':>13}{fitted[0]:>14.1%}"
              f"{(spent['pnl_over_risk'].mean() if len(spent) else float('nan')):>15.1%}"
              f"{(ours['pnl_over_risk'].mean() if len(ours) else float('nan')):>20.1%}")

    print("\nWHAT THIS ANSWERS")
    opened = [s for s in summary if s[4] and s[4][0] > 0 and s[2] == 0]
    if opened:
        print("  names our thresholds shut out that another cell opens profitably:")
        for name, book, _, _, best in opened:
            print(f"    {name:6} book {book:.2f}: {best[1]:.2f}/{best[2]:+.0f} "
                  f"gives {best[0]:+.1f} $ over {best[3]} trades")
    else:
        print("  no name our thresholds shut out is profitable under any cell tried")

    worse = [s for s in summary if s[2] >= 20 and s[3] < 0]
    if worse:
        print("  names we DO take that lose money:")
        for name, book, n, mean, _ in worse:
            print(f"    {name:6} {n} trades at {mean:+.1f} $")


if __name__ == "__main__":
    main()
