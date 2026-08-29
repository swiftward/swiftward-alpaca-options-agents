"""Running the rule over the collected structures, and a grid of thresholds.

Cost. There is no historical order book, so the markup is ASSIGNED and named:

  SLIP = 0.10 dollars per spread (both legs together), in per-share price, which
  is 10 dollars a contract.

Where that number comes from. The live book on SPY, QQQ and IWM was taken after
the close on 27 August 2026 across 587 strike pairs in the 0.3-3.0 percent band on
expirations 0-8 days out (`measure_cost.py`): the median of the two spreads summed
was 0.070, three quarters below 0.120. The project's own 34 fills on 26 August gave
up 0.0229 on average against the first price asked. We take 0.10 - above the median
of the live book and four times what we gave up ourselves. This is the FULL crossing
of both legs, where the product assumes half; and it is charged on top of the TRADE
price rather than the midpoint, so it is loaded twice over. That is deliberate: let
the conclusion survive the worst.

Fees: 0.025 dollars per contract per leg, measured on the account twice on
25 August. A spread is two legs, so 0.05 dollars to get in and the same to get out.

Exit. The rule holds to expiry unless the price passes the short strike. So there
is a close only where the strike was passed - and that is where the second crossing
is charged.
"""

import datetime as dt
import json
import sys
from pathlib import Path

import numpy as np
import pandas as pd

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"

# The book width per spread: the two bid-ask spreads summed. Measured by `measure_cost.py`.
BOOK = 0.07
# The edge is COMPUTED the way the product computes it: HALF the book comes off the
# credit (credit_after_cost). Otherwise a threshold found here could not be put into
# SCREENER_LEAST_EDGE - it would be measuring a different quantity.
EDGE_SLIP = BOOK / 2
# But the FULL book is paid: a fill at the far side of both legs. That is the
# conservative markup. Entry is taken at the TRADE price rather than the midpoint,
# so it is loaded twice over.
SLIP = BOOK
FEE = 0.05         # dollars per spread, both contracts
# The product ceiling SCREENER_DEAREST=35 is NOT applied here: it is measured
# against a real order book, and history holds none. Feeding our markup into it
# would turn an assumption about cost into a filter on credit as well, and the grid
# would then be measuring two changes at once.
MOST_COST_SHARE = 0.0


def prepare(frame, slip=SLIP, fee=FEE, cost_share=MOST_COST_SHARE, edge_slip=EDGE_SLIP):
    f = frame.copy()
    f["net_credit"] = f["credit"] - edge_slip
    f["net_risk"] = f["width"] - f["net_credit"]
    f = f[(f["net_credit"] > 0) & (f["net_risk"] > 0) & (f["credit"] > slip)].copy()
    if cost_share:
        f = f[edge_slip * 2 / f["credit"] * 100 <= cost_share].copy()
    f["survives"] = 1 - f["delta"].abs()
    f["breakeven"] = f["net_risk"] / (f["net_credit"] + f["net_risk"])
    f["edge"] = (f["survives"] - f["breakeven"]) * 100
    breached = f["loss"] > 0
    f["pnl"] = (f["credit"] - f["loss"]) * 100 - (slip * 100 + fee) - np.where(breached, slip * 100 + fee, 0.0)
    f["risk_dollars"] = f["risk"] * 100
    f["pnl_over_risk"] = f["pnl"] / f["risk_dollars"]
    return f


def pick(f, delta_cap, edge_min):
    kept = f[(f["delta"].abs() <= delta_cap) & (f["edge"] >= edge_min)]
    if kept.empty:
        return kept
    kept = kept.sort_values(["edge", "credit"], ascending=False)
    return kept.groupby(["day", "underlying"], as_index=False).head(1)


def drawdown(pnl):
    cum = np.cumsum(pnl)
    peak = np.maximum.accumulate(np.concatenate([[0.0], cum]))[1:]
    return float((cum - peak).min()) if len(cum) else 0.0


def stats(trades, all_days):
    if trades.empty:
        return dict(trades=0)
    order = trades.sort_values(["day", "underlying"])
    return dict(
        trades=len(order),
        mean_dollars=float(order["pnl"].mean()),
        mean_over_risk=float(order["pnl_over_risk"].mean()),
        win_share=float((order["pnl"] > 0).mean()),
        drawdown_dollars=drawdown(order["pnl"].values),
        total_dollars=float(order["pnl"].sum()),
        days_without_trades=int(all_days - order["day"].nunique()),
        median_risk_dollars=float(order["risk_dollars"].median()),
        touched_share=float(order["touched"].mean()),
        touched_but_survived=float((order["touched"] & (order["loss"] <= 0)).mean()),
    )


def main():
    frame = pd.read_parquet(DATA / "candidates_bt.parquet")
    all_days = frame["day"].nunique()
    print(f"structures in history: {len(frame)}, trading days with at least one: {all_days}")
    print(f"period: {frame['day'].min()} .. {frame['day'].max()}")
    print(f"underlyings: {sorted(frame['underlying'].unique())}\n")

    levels = ((0.0, "no markup, fees only"),
              (0.035, "0.035 - half the book, as the product assumes"),
              (SLIP, "0.07 - the FULL median of the live book (baseline)"),
              (0.12, "0.12 - stress, three quarters of the live book"))
    for slip, name in levels:
        f = prepare(frame, slip=slip)
        base = pick(f, 0.45, -1.0)
        s = stats(base, all_days)
        print(f"{name:48}: " + "  ".join(f"{k} {v:.4g}" if isinstance(v, float) else f"{k} {v}" for k, v in s.items()))

    print("\n=== values in force: delta ceiling 0.45, edge threshold -1.0 ===")
    f = prepare(frame)
    base = pick(f, 0.45, -1.0)
    print(json.dumps(stats(base, all_days), ensure_ascii=False, indent=1))
    if not base.empty:
        print("\nby underlying:")
        for name, part in base.groupby("underlying"):
            print(f"  {name}: {len(part):5} trades, mean {part['pnl'].mean():+8.2f} usd, "
                  f"wins {(part['pnl'] > 0).mean():.3f}, drawdown {drawdown(part.sort_values('day')['pnl'].values):.0f}")
        print("\nby days to expiry (trading days):")
        for name, part in base.groupby("dte"):
            print(f"  {name}: {len(part):5} trades, mean {part['pnl'].mean():+8.2f} usd, "
                  f"wins {(part['pnl'] > 0).mean():.3f}")
        print("\nby side:")
        for name, part in base.groupby("kind"):
            print(f"  {name}: {len(part):5} trades, mean {part['pnl'].mean():+8.2f} usd, "
                  f"wins {(part['pnl'] > 0).mean():.3f}")
        print("\nby width in strikes:")
        for name, part in base.groupby("width"):
            print(f"  {name}: {len(part):5} trades, mean {part['pnl'].mean():+8.2f} usd, "
                  f"wins {(part['pnl'] > 0).mean():.3f}")

    print("\n=== the grid: rows are the delta ceiling, columns the edge threshold ===")
    caps = [0.10, 0.15, 0.20, 0.25, 0.30, 0.35, 0.45]
    thresholds = list(range(-10, 6))
    for what, key, fmt in (("trades", "trades", "{:>6d}"),
                           ("mean, usd", "mean_dollars", "{:>6.1f}"),
                           ("mean, share of risk", "mean_over_risk", "{:>6.3f}"),
                           ("win share", "win_share", "{:>6.3f}"),
                           ("worst drawdown, usd", "drawdown_dollars", "{:>6.0f}"),
                           ("days without trades", "days_without_trades", "{:>6d}")):
        print(f"\n-- {what} --")
        head = "delta|edge"
        print(f"{head:>12}" + "".join(f"{t:>6}" for t in thresholds))
        for cap in caps:
            line = f"{cap:>12.2f}"
            for t in thresholds:
                s = stats(pick(f, cap, t), all_days)
                v = s.get(key)
                line += ("     -" if v is None else fmt.format(v))
            print(line)

    print("\n(cells with fewer than 30 trades are not a conclusion)")

    print("\n=== stability: the best cells and their neighbours ===")
    table = []
    for cap in caps:
        for t in thresholds:
            s_ = stats(pick(f, cap, t), all_days)
            if s_["trades"] >= 60:
                table.append((s_["mean_dollars"], cap, t, s_))
    table.sort(reverse=True)
    print(f"{'d':>5} {'edge':>6} {'trades':>7} {'mean,$':>11} {'over risk':>11} {'wins':>10} {'drawdown,$':>11} {'idle days':>11}")
    for _, cap, t, s_ in table[:12]:
        print(f"{cap:5.2f} {t:6} {s_['trades']:7} {s_['mean_dollars']:11.2f} "
              f"{s_['mean_over_risk']:11.3f} {s_['win_share']:10.3f} "
              f"{s_['drawdown_dollars']:11.0f} {s_['days_without_trades']:11}")

    if table:
        _, best_cap, best_t, _ = table[0]
        print(f"\nmoving the edge one step from the best cell (delta={best_cap}, edge={best_t}):")
        for t in (best_t - 2, best_t - 1, best_t, best_t + 1, best_t + 2):
            if t < -10 or t > 5:
                continue
            s_ = stats(pick(f, best_cap, t), all_days)
            if s_["trades"] == 0:
                print(f"  edge {t:+3}: 0 trades")
                continue
            print(f"  edge {t:+3}: {s_['trades']:5} trades, mean {s_['mean_dollars']:+8.2f}, "
                  f"over risk {s_['mean_over_risk']:+.3f}, drawdown {s_['drawdown_dollars']:.0f}")
        print(f"\nmoving the delta ceiling at edge {best_t}:")
        for cap in caps:
            s_ = stats(pick(f, cap, best_t), all_days)
            if s_["trades"] == 0:
                print(f"  delta {cap:.2f}: 0 trades")
                continue
            print(f"  delta {cap:.2f}: {s_['trades']:5} trades, mean {s_['mean_dollars']:+8.2f}, "
                  f"over risk {s_['mean_over_risk']:+.3f}, drawdown {s_['drawdown_dollars']:.0f}")

    print("\n=== the same grid of means WITHOUT the crossing markup (fees only) ===")
    clean = prepare(frame, slip=0.0)
    head = "delta|edge"
    print(f"{head:>12}" + "".join(f"{t:>6}" for t in thresholds))
    for cap in caps:
        line = f"{cap:>12.2f}"
        for t in thresholds:
            s_ = stats(pick(clean, cap, t), all_days)
            line += "     -" if s_["trades"] == 0 else f"{s_['mean_dollars']:>6.1f}"
        print(line)
    print(f"{'trades':>12}" + "".join(f"{stats(pick(clean, 0.45, t), all_days)['trades']:>6}" for t in thresholds) + "   (row delta=0.45)")


if __name__ == "__main__":
    main()
