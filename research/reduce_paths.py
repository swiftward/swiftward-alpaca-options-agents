"""The take-profit rule, reduced to what it actually reads.

`threshold.py` walks the minute-by-minute buy-back cost of every trade, and that
file is 114 MB - past what a repository should carry, so the number it produces
could not be checked without an Alpaca key.

The rule asks one thing of the path: WHEN did the buy-back first cost no more than
a share of the credit, and what did it cost at that minute. Both are answered by
the sequence of new lows alone - the first minute below any level is by definition
the minute of a new low, and the cost there is that low. Every other minute can be
dropped without changing a single answer for any share.

Result: `data/buyback_lows.json`, a few hundred kilobytes, committed, and
`claims.py` recomputes the published take-profit numbers from it with no
credential.
"""

import json
from pathlib import Path

import pandas as pd

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
OUT = DATA / "buyback_lows.json"


def lows(costs):
    """The running minimum, keeping only the minutes where it fell."""
    out, best = [], None
    for c in costs:
        if best is None or c < best:
            best = c
            out.append(float(c))
    return out


def main() -> None:
    picked = pd.read_parquet(DATA / "picked.parquet")
    paths = {}
    with (DATA / "paths.jsonl").open() as h:
        for line in h:
            row = json.loads(line)
            paths[row["day"]] = row["bars"]

    def series(bars, symbol):
        return {r["t"]: r["c"] for r in (bars.get(symbol) or []) if r.get("c") is not None}

    kept, missing = [], 0
    for r in picked.itertuples():
        bars = paths.get(str(r.day))
        if not bars:
            missing += 1
            continue
        short, long_ = series(bars, r.short_symbol), series(bars, r.long_symbol)
        common = sorted(set(short) & set(long_))
        if len(common) < 5:
            missing += 1
            continue
        kept.append(dict(day=str(r.day), credit=float(r.credit), risk=float(r.risk),
                         pnl_hold=float(r.pnl),
                         lows=lows([short[t] - long_[t] for t in common])))

    OUT.write_text(json.dumps(kept))
    print(f"trades {len(kept)}, without a path {missing}, "
          f"lows a trade {sum(len(k['lows']) for k in kept) / max(1, len(kept)):.1f}, "
          f"{OUT.stat().st_size / 1024:.0f} KB")


if __name__ == "__main__":
    main()
