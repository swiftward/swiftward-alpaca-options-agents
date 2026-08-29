"""Does the hour of the day move the edge, and are the three windows where it is?

The declaration lets the agent open inside three windows - 10:20, 12:30 and 14:20
New York - about two hours of a six-and-a-half hour session. This asks whether that
is where the good entries are.

HOW THE EDGE IS REBUILT AT AN HOUR THAT WAS NEVER MEASURED

`edge_points` needs two things: the credit after the crossing cost, and the chance
the sold strike survives. The credit comes straight from the minute bars. The
chance does not - it comes from delta, and delta is a broker's number, absent from
any historical bar.

So delta is recovered the way `exit_rules.py` recovers a closing price: the
volatility implied by each leg's own traded price at that minute, then Black-Scholes
from it. At 14:20 this reconstruction can be checked against the delta the broker
actually gave, and the check is printed - a reconstruction that disagrees there has
no business being trusted at 10:00.

WHAT IS AND IS NOT COMPARED

The strike universe is the one that day's candidates covered. Re-selecting from the
whole chain at every hour would need the whole chain at every hour, which is a
different and much larger collection. So this measures: given the strikes that were
worth looking at that day, which HOUR offered the best structure among them.

The outcome of each pick is known from `settle` - the same settlement the rest of
these measurements use. Cost is charged as in grid.py: the full book plus fees.
"""

import json
import math
from collections import defaultdict
from pathlib import Path

import numpy as np
import pandas as pd

import bs
import grid

HERE = Path(__file__).resolve().parent
DELTA_CAP = 0.30
EDGE_MIN = 3.0
# Half past every hour and on the hour, from the open to an hour before the close.
# Later than that and a same-day structure has no life left to sell.
HOURS = [f"{h:02d}:{m:02d}" for h in range(10, 15) for m in (0, 30)]


def occ(underlying, expiry, kind, strike):
    day = pd.Timestamp(expiry).strftime("%y%m%d")
    return f"{underlying}{day}{'C' if kind == 'call' else 'P'}{int(round(strike * 1000)):08d}"


def at_minute(bars, minute):
    """The last trade at or before `minute`, New York. None where none traded."""
    seen = None
    for bar in bars:
        stamp = pd.Timestamp(bar["t"]).tz_convert("America/New_York").strftime("%H:%M")
        if stamp <= minute:
            seen = bar["c"]
        else:
            break
    return seen


def main() -> None:
    frame = grid.prepare(pd.read_parquet(HERE / "data" / "candidates_bt.parquet"))
    days = {}
    with (HERE / "data" / "intraday.jsonl").open() as handle:
        for line in handle:
            row = json.loads(line)
            days[row["day"]] = row["bars"]
    print(f"days with intraday bars: {len(days)}")

    checks, picked = [], defaultdict(list)
    for day, bars in days.items():
        rows = frame[frame.day.astype(str) == day]
        if rows.empty:
            continue
        for minute in HOURS:
            best = None
            for r in rows.itertuples():
                short_bars = bars.get(occ(r.underlying, r.expiry, r.kind, r.short_strike)) or []
                long_bars = bars.get(occ(r.underlying, r.expiry, r.kind, r.long_strike)) or []
                short_price = at_minute(short_bars, minute)
                long_price = at_minute(long_bars, minute)
                if short_price is None or long_price is None:
                    continue
                credit = short_price - long_price
                net = credit - grid.EDGE_SLIP
                net_risk = r.width - net
                if net <= 0 or net_risk <= 0:
                    continue

                years = r.years
                vol = bs.implied(r.kind, short_price, r.spot, r.short_strike, years, 0.04, 0.012)
                if vol is None or not math.isfinite(vol):
                    continue
                delta = abs(bs.delta(r.kind, r.spot, r.short_strike, vol, years, 0.04, 0.012))
                edge = ((1 - delta) - net_risk / (net + net_risk)) * 100
                if minute == "14:00":
                    checks.append(delta - abs(r.delta))
                if delta > DELTA_CAP or edge < EDGE_MIN:
                    continue
                if best is None or edge > best[0]:
                    best = (edge, r, credit)

            if best is None:
                picked[minute].append(0.0)
                continue
            _, r, credit = best
            breached = r.loss > 0
            pnl = (credit - r.loss) * 100 - (grid.SLIP * 100 + grid.FEE) \
                - (grid.SLIP * 100 + grid.FEE if breached else 0.0)
            picked[minute].append(pnl)

    if checks:
        checks = np.array(checks)
        print(f"delta reconstruction against the broker's own at 14:00: "
              f"median error {np.median(checks):+.3f}, |error| under 0.05 in "
              f"{(np.abs(checks) < 0.05).mean():.0%} of cases\n")

    print(f"{'hour':>6} {'days with a pick':>18} {'total, $':>10} {'mean, $':>9} {'in the red':>12}")
    for minute in HOURS:
        out = np.array(picked[minute])
        taken = out[out != 0.0]
        if len(taken) == 0:
            print(f"{minute:>6} {0:>18} {'-':>10} {'-':>9} {'-':>12}")
            continue
        print(f"{minute:>6} {len(taken):>18} {taken.sum():>10,.0f} {taken.mean():>9,.1f} "
              f"{(taken < 0).mean():>11.0%}")

    print("\nper half-hour slot, so the count of slots does not decide the answer")
    inside_slots = ("10:30", "12:30", "14:00", "14:30")
    ins = [np.array(picked[m])[np.array(picked[m]) != 0] for m in inside_slots if len(picked[m])]
    outs = [np.array(picked[m])[np.array(picked[m]) != 0] for m in HOURS
            if m not in inside_slots and len(picked[m])]
    if ins and outs:
        print(f"  inside  {len(ins)} slots, {np.mean([len(x) for x in ins]):.0f} picks a slot, "
              f"mean {np.mean([x.mean() for x in ins]):+.1f} $")
        print(f"  outside {len(outs)} slots, {np.mean([len(x) for x in outs]):.0f} picks a slot, "
              f"mean {np.mean([x.mean() for x in outs]):+.1f} $")

    print("\nthe 13:00 slot, which stands apart")
    odd = np.array(picked["13:00"]); odd = odd[odd != 0]
    near = np.concatenate([np.array(picked[m])[np.array(picked[m]) != 0]
                           for m in ("12:30", "13:30")])
    if len(odd) and len(near):
        print(f"  13:00        {len(odd):>4} picks, mean {odd.mean():+7.1f} $, "
              f"worst {odd.min():+8.0f} $, in the red {(odd < 0).mean():.0%}")
        print(f"  12:30+13:30  {len(near):>4} picks, mean {near.mean():+7.1f} $, "
              f"worst {near.min():+8.0f} $, in the red {(near < 0).mean():.0%}")
        print(f"  the gap is {'one bad trade' if odd.min() < -2 * abs(odd.sum()) else 'spread across the sample'}: "
              f"drop the single worst and 13:00 becomes "
              f"{np.sort(odd)[1:].mean():+.1f} $")

    print("\nthe three windows the declaration allows, against the rest of the day")
    inside = [m for m in HOURS if m in ("10:30", "12:30", "14:00", "14:30")]
    a = np.concatenate([np.array(picked[m])[np.array(picked[m]) != 0] for m in inside]) \
        if any(len(picked[m]) for m in inside) else np.array([])
    outside_hours = [m for m in HOURS if m not in inside]
    b = np.concatenate([np.array(picked[m])[np.array(picked[m]) != 0] for m in outside_hours]) \
        if any(len(picked[m]) for m in outside_hours) else np.array([])
    if len(a) and len(b):
        print(f"  inside  {len(a):>4} picks, mean {a.mean():+7.1f} $, total {a.sum():+9,.0f} $")
        print(f"  outside {len(b):>4} picks, mean {b.mean():+7.1f} $, total {b.sum():+9,.0f} $")


if __name__ == "__main__":
    main()
