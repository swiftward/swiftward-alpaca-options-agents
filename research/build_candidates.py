"""Builds every structure the rule could have seen, for every day of history.

It selects nothing by delta or by edge - the grid does that. Here there is only
what does not depend on a threshold: the price of each leg, the credit, the risk,
the implied delta and the outcome at expiry.

Dropped here, and counted:
  - a leg with no trade inside the fifteen-minute entry window: there is no price;
  - a leg with fewer trades in the window than the floor: one or two trades are
    not a price;
  - a credit that is not positive, or a risk that is not positive: not a credit
    spread;
  - a sold strike whose distance from the price falls outside 0.3 to 3.0 per cent:
    outside what the product's screener looks at (SCREENER_NEAREST,
    SCREENER_FURTHEST);
  - credit against risk outside 8 to 100 per cent (SCREENER_LEAST_PAID,
    SCREENER_MOST_PAID).
"""

import datetime as dt
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path
from zoneinfo import ZoneInfo

import numpy as np
import pandas as pd
from scipy.stats import norm

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
NY = ZoneInfo("America/New_York")

RATE = 0.045
# Annual dividend yield. Over terms of nought to five days it changes almost
# nothing - it shows only when an ex-date falls inside the window - but one name's
# figure cannot stand in for another's: SPY pays 1.15% and TSLA nothing. An
# unknown name is priced with no dividend, and that is said out loud rather than
# silently.
#
# GLD and SLV stand at ZERO by measurement rather than by reference: their
# adjusted and raw daily series agree to the last digit across 685 days, which
# does not happen to a fund that distributes. The other names differ by 1.6 to
# 10.7 per cent, so the check means something.
#
# Three of these remain ASSIGNED. Measuring them through put-call parity was tried
# on 29 August and honestly did not work; the result and the reason are kept.
DIV = {"SPY": 0.0115, "QQQ": 0.0045, "IWM": 0.0120, "GLD": 0.0, "SLV": 0.0,
       "AAPL": 0.004, "MSFT": 0.007, "NVDA": 0.0002, "META": 0.003,
       "MU": 0.004, "TSLA": 0.0, "AMZN": 0.0, "AMD": 0.0}
WINDOW = dt.time(14, 0)
AHEAD = 5
WIDEST = 5           # the width in strikes, as `widest` is in the product
NEAREST, FURTHEST = 0.3, 3.0
LEAST_PAID, MOST_PAID = 8.0, 100.0
MIN_TRADES = 5       # trades inside the window, on each leg


def load(symbol):
    day = json.loads((DATA / f"stock_{symbol}_day.json").read_text())
    m15 = json.loads((DATA / f"stock_{symbol}_15m.json").read_text())
    contracts = {c["symbol"]: c for c in json.loads((DATA / f"contracts_{symbol}.json").read_text())}
    closes, highs, lows = {}, {}, {}
    for b in day:
        d = dt.date.fromisoformat(b["t"][:10])
        closes[d], highs[d], lows[d] = b["c"], b["h"], b["l"]
    spots = {}
    for b in m15:
        at = dt.datetime.fromisoformat(b["t"].replace("Z", "+00:00")).astimezone(NY)
        if at.time() == WINDOW:
            spots[at.date()] = b["c"]
    obars = {}
    for line in (DATA / f"obars_{symbol}.jsonl").open():
        row = json.loads(line)
        obars[dt.date.fromisoformat(row["day"])] = {b["s"]: b for b in row["bars"]}
    return closes, highs, lows, spots, contracts, obars


def implied_many(kinds, market, spot, strike, years, rate, div):
    """Volatility backed out for every row at once, by bisection.

    Vectorised, because the rows number in the hundreds of thousands. Newton is
    no good here: on far strikes the derivative with respect to volatility is
    almost nought.
    """
    low = np.full(len(market), 0.005)
    high = np.full(len(market), 5.0)
    call = kinds == "call"

    def price(vol):
        sq = vol * np.sqrt(years)
        one = (np.log(spot / strike) + (rate - div + 0.5 * vol * vol) * years) / sq
        two = one - sq
        ds = spot * np.exp(-div * years)
        dk = strike * np.exp(-rate * years)
        return np.where(call, ds * norm.cdf(one) - dk * norm.cdf(two),
                        dk * norm.cdf(-two) - ds * norm.cdf(-one))

    for _ in range(60):
        mid = 0.5 * (low + high)
        below = price(mid) < market
        low = np.where(below, mid, low)
        high = np.where(below, high, mid)
    vol = 0.5 * (low + high)
    # Where there is no solution - a price below intrinsic or above the bound -
    # the row is marked NaN.
    bad = (vol <= 0.0055) | (vol >= 4.99)
    vol = np.where(bad, np.nan, vol)
    return vol


def delta_many(kinds, spot, strike, vol, years, rate, div):
    sq = vol * np.sqrt(years)
    one = (np.log(spot / strike) + (rate - div + 0.5 * vol * vol) * years) / sq
    return np.where(kinds == "call", np.exp(-div * years) * norm.cdf(one),
                    -np.exp(-div * years) * norm.cdf(-one))


def build(symbol, days_all):
    closes, highs, lows, spots, contracts, obars = load(symbol)
    div = DIV.get(symbol)
    if div is None:
        print(f"    {symbol}: dividend unknown, priced without one")
        div = 0.0

    # The strike grid of each series, worked out once rather than inside the loop
    # over days.
    grids = defaultdict(set)
    for c in contracts.values():
        grids[(c["expiration"], c["type"])].add(c["strike"])
    # The step is the MOST FREQUENT gap, not the smallest. Some SPY and QQQ series
    # carry strikes half a dollar apart and even 0.22 apart; the smallest gap would
    # take one of those for the grid and narrow the permitted width fivefold
    # against the real one.
    steps = {}
    for key, strikes in grids.items():
        ordered = sorted(strikes)
        gaps = Counter(round(b - a, 2) for a, b in zip(ordered, ordered[1:]) if b > a)
        if gaps:
            steps[key] = gaps.most_common(1)[0][0]
    rows = []
    dropped = defaultdict(int)

    for i, day in enumerate(days_all):
        book = obars.get(day)
        if not book or day not in spots:
            continue
        spot = spots[day]
        entry = dt.datetime.combine(day, WINDOW, tzinfo=NY)

        # The legs that have a price: a trade inside the window, and no fewer
        # trades than the floor.
        priced = {}
        for name, bar in book.items():
            if bar["n"] < MIN_TRADES:
                dropped["leg: too few trades in the window"] += 1
                continue
            if bar["c"] <= 0:
                dropped["leg: price of nought"] += 1
                continue
            priced[name] = bar

        path_hi, path_lo = -1e18, 1e18
        for e in range(0, AHEAD + 1):
            if i + e >= len(days_all):
                break
            expiry = days_all[i + e]
            if expiry not in closes:
                continue
            # The daily high and low over the whole path from entry to expiry.
            # They are needed to count how often the short strike was TOUCHED -
            # that is, how often a stop would have fired, which the outcome does
            # not include.
            path_hi = max(path_hi, highs[expiry])
            path_lo = min(path_lo, lows[expiry])
            settle = closes[expiry]
            expires = dt.datetime.combine(expiry, dt.time(16, 0), tzinfo=NY)
            years = (expires - entry).total_seconds() / (365.0 * 24 * 3600)
            if years <= 0:
                continue

            for kind in ("call", "put"):
                # The strike step of this series comes from the WHOLE contract
                # listing rather than from what traded: a width in strikes has to
                # be counted on the exchange's grid, or a strike that did not
                # trade turns a five-strike spread into an eight-strike one.
                step = steps.get((expiry.isoformat(), kind))
                if step is None:
                    continue

                chain = []
                for name, bar in priced.items():
                    c = contracts.get(name)
                    if c and c["expiration"] == expiry.isoformat() and c["type"] == kind:
                        chain.append((c["strike"], name, bar))
                if len(chain) < 2:
                    continue
                chain.sort()

                for a in range(len(chain)):
                    for b in range(a + 1, min(a + 1 + WIDEST, len(chain))):
                        if chain[b][0] - chain[a][0] > WIDEST * step + 1e-9:
                            break
                        if kind == "call":
                            si, li = a, b          # call: sell the nearer, buy the further
                        else:
                            si, li = b, a          # put: sell the upper, buy the lower
                        ks, ns, short_bar = chain[si]
                        kl, nl, long_bar = chain[li]
                        width = abs(ks - kl)
                        if width <= 0:
                            continue
                        credit = short_bar["c"] - long_bar["c"]
                        risk = width - credit
                        if credit <= 0 or risk <= 0:
                            dropped["pair: no credit or no risk"] += 1
                            continue
                        out = (ks - spot) / spot * 100 if kind == "call" else (spot - ks) / spot * 100
                        if not (NEAREST <= out <= FURTHEST):
                            dropped["pair: distance outside 0.3-3.0%"] += 1
                            continue
                        to_risk = credit / risk * 100
                        if to_risk < LEAST_PAID or to_risk > MOST_PAID:
                            dropped["pair: credit to risk outside 8-100%"] += 1
                            continue

                        if kind == "call":
                            loss = max(0.0, min(settle, kl) - ks)
                        else:
                            loss = max(0.0, ks - max(settle, kl))
                        touched = path_hi >= ks if kind == "call" else path_lo <= ks
                        rows.append((day, symbol, kind, expiry, e, ks, kl, width,
                                     credit, risk, spot, short_bar["c"], short_bar["n"], long_bar["n"],
                                     years, settle, loss, bool(touched)))

    frame = pd.DataFrame(rows, columns=[
        "day", "underlying", "kind", "expiry", "dte", "short_strike", "long_strike",
        "width", "credit", "risk", "spot", "short_price", "n_short", "n_long",
        "years", "settle", "loss", "touched"])
    if frame.empty:
        return frame, dropped

    vol = implied_many(frame["kind"].values, frame["short_price"].values,
                       frame["spot"].values, frame["short_strike"].values,
                       frame["years"].values, RATE, div)
    frame["iv"] = vol
    frame["delta"] = delta_many(frame["kind"].values, frame["spot"].values,
                                frame["short_strike"].values, vol,
                                frame["years"].values, RATE, div)
    lost = frame["iv"].isna().sum()
    dropped["pair: volatility did not come back"] += int(lost)
    frame = frame[~frame["iv"].isna()].copy()
    return frame, dropped


def main():
    spy = json.loads((DATA / "stock_SPY_day.json").read_text())
    days_all = [dt.date.fromisoformat(b["t"][:10]) for b in spy]
    days_all = [d for d in days_all if dt.date(2024, 1, 1) <= d <= dt.date(2026, 8, 26)]

    names = sys.argv[1:] or ["SPY", "QQQ", "IWM"]
    frames, counted = [], defaultdict(int)
    for symbol in names:
        if not (DATA / f"obars_{symbol}.jsonl").exists():
            print(f"{symbol}: no bars, skipping")
            continue
        frame, dropped = build(symbol, days_all)
        print(f"{symbol}: {len(frame):>8} structures over {frame['day'].nunique() if len(frame) else 0} days")
        for k, v in dropped.items():
            print(f"    dropped, {k}: {v}")
            counted[k] += v
        frames.append(frame)

    out = pd.concat(frames, ignore_index=True)
    target = "candidates_bt.parquet" if names == ["SPY", "QQQ", "IWM"] else "candidates_names.parquet"
    out.to_parquet(DATA / target)
    print(f"written to {target}")
    print(f"\n{len(out)} structures in all")
    (DATA / "dropped.json").write_text(json.dumps(dict(counted), indent=1))


if __name__ == "__main__":
    main()
