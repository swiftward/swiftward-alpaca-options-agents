"""The take-profit share: at what fraction of the credit is closing better.

Measured on the minute-by-minute path of every trade the rule actually picked.
Until this ran the number could only have been assigned: the outcome at expiry was
on hand, the path to it was not, and the whole rule lives in the path.

Closing is charged the same way grid.py charges it: the full book plus fees. That
matters - without it an early exit would look free.
"""
import json
from collections import defaultdict

import numpy as np
import pandas as pd

from grid import BOOK, FEE, prepare, pick

DEEP = 100  # prices are kept as dollars, as they come

picked = pd.read_parquet("data/picked.parquet")
print(f"trades: {len(picked)}")

# The path: for each day of entry, the minute bars of every leg.
paths = {}
with open("data/paths.jsonl") as h:
    for line in h:
        d = json.loads(line)
        paths[d["day"]] = d["bars"]
print(f"days with paths: {len(paths)}")

def series(bars, symbol):
    rows = bars.get(symbol) or []
    return {r["t"]: r["c"] for r in rows if r.get("c") is not None}

rows = []
missing = 0
for r in picked.itertuples():
    bars = paths.get(str(r.day))
    if not bars:
        missing += 1
        continue
    short = series(bars, r.short_symbol)
    long_ = series(bars, r.long_symbol)
    common = sorted(set(short) & set(long_))
    if len(common) < 5:
        missing += 1
        continue
    # What buying the spread back cost in each minute. These are trade prices, not
    # quotes, so the book is charged separately - in full, as in grid.
    cost = np.array([short[t] - long_[t] for t in common])
    rows.append(dict(day=str(r.day), credit=float(r.credit), risk=float(r.risk),
                     pnl_hold=float(r.pnl), path=cost))

print(f"without a path: {missing}, with one: {len(rows)}")
print()

def run(share):
    """Close once the buy-back costs no more than `share` of the credit taken."""
    out = []
    for x in rows:
        target = x["credit"] * share
        hit = np.nonzero(x["path"] <= target)[0]
        if len(hit) == 0:
            out.append(x["pnl_hold"])          # never reached it: held to expiry
            continue
        at = float(x["path"][hit[0]])
        # Entry is already charged inside pnl_hold; this charges the SECOND
        # crossing - the close.
        out.append((x["credit"] - at) * 100 - (BOOK * 100 + FEE) * 2)
    return np.array(out)

print("SHARE: close once the buy-back is cheaper than this fraction of the credit")
print(f"{'share':>6} {'closed early':>14} {'total, $':>12} {'mean, $':>12} {'worst, $':>12} {'in the red':>12}")
hold = np.array([x["pnl_hold"] for x in rows])
print(f"{'hold':>6} {'-':>14} {hold.sum():>11,.0f} {hold.mean():>11,.0f} {hold.min():>11,.0f} {(hold<0).mean():>11.0%}")
for share in [0.10, 0.20, 0.25, 0.30, 0.40, 0.50, 0.60, 0.75]:
    p = run(share)
    early = sum(1 for x in rows if (x["path"] <= x["credit"]*share).any())
    print(f"{share:>6.0%} {early/len(rows):>13.0%} {p.sum():>11,.0f} {p.mean():>11,.0f} {p.min():>11,.0f} {(p<0).mean():>9.0%}")

print()
print("A FINER GRID AROUND THE PEAK")
best=None
for share in np.arange(0.20,0.56,0.025):
    p=run(share)
    print(f"  {share:>5.1%}  total {p.sum():>8,.0f} $  mean {p.mean():>6,.1f} $  in the red {(p<0).mean():>5.0%}")
    if best is None or p.sum()>best[1]: best=(share,p.sum())
print(f"  peak: {best[0]:.1%}")

print()
print("STABILITY: the same grid on each half of the history")
half=len(rows)//2
order=sorted(range(len(rows)), key=lambda i: rows[i]["day"])
for name,idx in [("first half",order[:half]),("second half",order[half:])]:
    sub=[rows[i] for i in idx]
    saved=rows[:]
    globals()['rows']=sub
    line=[]
    for share in [0.25,0.30,0.35,0.40,0.45,0.50]:
        line.append(f"{share:.0%}:{run(share).sum():>7,.0f}")
    globals()['rows']=saved
    print(f"  {name}: "+"  ".join(line))
