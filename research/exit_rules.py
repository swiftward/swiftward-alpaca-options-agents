"""What a defence rule is worth: every exit rule run over the same trades.

Trade selection uses the CURRENT settings: delta ceiling 0.30, edge threshold +2,
width one to five strikes (the whole range collected at all). The selection comes
from `grid.pick`, so that exactly the trades the product produces are compared.

The exit rules:
  1. hold to expiry (the starting point, matching `grid.prepare`);
  2. close when the short strike is touched (the defence in force);
  3. close once the price has gone past the short strike by a share of the width:
     0.25 / 0.50 / 1.00;
  4. close an hour before the bell on expiry day, wherever the price is.

WHAT THE DATA DOES NOT HOLD, and what is therefore assigned.

  There are no minute bars of the underlying to that depth: `fetch_stocks.py`
  pulled daily and FIFTEEN-MINUTE ones. The price path runs on 15-minute bars. So a
  touch inside a quarter hour is seen through the bar's high and low, but the exact
  moment of the touch is not - and the exit fills not where it touched but at the
  bar's price (see below).

  Option prices through time are absent entirely: `obars_*.jsonl` holds one bar per
  leg per day, in the 14:00 entry window. So the cost of CLOSING is modelled: the
  implied volatility is recovered from each leg's price in the entry window, held
  constant, and at the moment of exit the spread is repriced by Black-Scholes from
  the underlying's price then and the time remaining. By construction the model
  returns exactly the market credit at entry. After that it is a model, not history:
  there is no smile here and no movement of one, and no volatility jump when a
  strike is struck. So a stressed variant is computed as well: volatility at exit
  raised by a quarter.

  The outcome of holding to expiry comes from history, not from the model: the
  underlying's closing price on expiry day, as in the previous run.

Cost. The same as in `grid.py`: a book of 0.07 dollars per spread (both legs) and
fees of 0.05 dollars per spread. Entry is always paid. The exit is paid where the
position is CLOSED: holding to expiry pays only if the spread was breached in the
end (as the previous run counted it), an early exit always pays.

Exit price. Conservatively: the worse of the trigger level and the closing price of
the bar it fired on. For a call the higher, for a put the lower. A softer variant -
simply the bar's close - is shown on its own line.
"""

import datetime as dt
import json
from collections import defaultdict
from pathlib import Path
from zoneinfo import ZoneInfo

import numpy as np
import pandas as pd
from scipy.stats import norm

import grid

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
NY = ZoneInfo("America/New_York")

RATE = 0.045
DIV = {"SPY": 0.0115, "QQQ": 0.0045, "IWM": 0.0120}
ENTRY_TIME = dt.time(14, 0)
CLOSE_TIME = dt.time(16, 0)
TIME_EXIT = dt.time(15, 0)      # an hour before the bell on expiry day
OPEN_TIME = dt.time(9, 30)

DELTA_CAP, EDGE_MIN = 0.30, 2.0
EXIT_COST = grid.SLIP * 100 + grid.FEE   # dollars per spread: book plus fees
IV_STRESS = 1.25


# --------------------------------------------------------- the price of a spread

def bs(kind, spot, strike, vol, years, div):
    """The price of one option. At zero time left, intrinsic value."""
    if years <= 0 or vol <= 0:
        return max(0.0, spot - strike) if kind == "call" else max(0.0, strike - spot)
    sq = vol * np.sqrt(years)
    one = (np.log(spot / strike) + (RATE - div + 0.5 * vol * vol) * years) / sq
    two = one - sq
    ds = spot * np.exp(-div * years)
    dk = strike * np.exp(-RATE * years)
    if kind == "call":
        return float(ds * norm.cdf(one) - dk * norm.cdf(two))
    return float(dk * norm.cdf(-two) - ds * norm.cdf(-one))


def implied(kind, market, spot, strike, years, div):
    """The inverse problem by bisection. NaN where there is no answer."""
    if years <= 0 or market <= 0:
        return float("nan")
    low, high = 0.005, 5.0
    for _ in range(80):
        mid = 0.5 * (low + high)
        if bs(kind, spot, strike, mid, years, div) < market:
            low = mid
        else:
            high = mid
    vol = 0.5 * (low + high)
    return float("nan") if vol <= 0.0055 or vol >= 4.99 else vol


def spread_value(trade, spot, at, iv_short, iv_long, stress=1.0):
    """What closing the spread costs at `at`, with the underlying at `spot`."""
    div = DIV[trade["underlying"]]
    expires = dt.datetime.combine(trade["expiry"], CLOSE_TIME, tzinfo=NY)
    years = max(0.0, (expires - at).total_seconds() / (365.0 * 24 * 3600))
    short = bs(trade["kind"], spot, trade["short_strike"], iv_short * stress, years, div)
    long_ = bs(trade["kind"], spot, trade["long_strike"], iv_long * stress, years, div)
    return float(np.clip(short - long_, 0.0, trade["width"]))


# ------------------------------------------------------------------- loading

def load_paths(symbol):
    """15-minute bars in trading hours, by day, in New York time."""
    rows = json.loads((DATA / f"stock_{symbol}_15m.json").read_text())
    days = defaultdict(list)
    for b in rows:
        at = dt.datetime.fromisoformat(b["t"].replace("Z", "+00:00")).astimezone(NY)
        if OPEN_TIME <= at.time() < CLOSE_TIME:
            days[at.date()].append((at, b["o"], b["h"], b["l"], b["c"]))
    for d in days:
        days[d].sort()
    return dict(days)


def load_leg_prices(symbol):
    """Each leg's price in the entry window: (day, expiry, type, strike) -> price."""
    by_symbol = {}
    for c in json.loads((DATA / f"contracts_{symbol}.json").read_text()):
        by_symbol[c["symbol"]] = (c["expiration"], c["type"], c["strike"])
    out = {}
    for line in (DATA / f"obars_{symbol}.jsonl").open():
        row = json.loads(line)
        day = dt.date.fromisoformat(row["day"])
        for b in row["bars"]:
            meta = by_symbol.get(b["s"])
            if meta:
                out[(day, meta[0], meta[1], meta[2])] = b["c"]
    return out


# --------------------------------------------------------------- walking the path

def walk(trade, paths, level):
    """The first bar on which the price passed the level. None if it never did."""
    day, expiry = trade["day"], trade["expiry"]
    call = trade["kind"] == "call"
    d = day
    while d <= expiry:
        for at, o, h, l, c in paths.get(d, ()):
            if d == day and at.time() < ENTRY_TIME:
                continue
            if d == day and at.time() == ENTRY_TIME:
                continue          # the entry bar: its close is where we entered
            hit = h >= level if call else l <= level
            if hit:
                fill = max(level, c) if call else min(level, c)
                return at, fill
        d += dt.timedelta(days=1)
        while d <= expiry and d not in paths:
            d += dt.timedelta(days=1)
    return None


def time_exit_spot(trade, paths):
    """The underlying at 15:00 on expiry day (the open of the 15:00 bar)."""
    bars = paths.get(trade["expiry"], ())
    for at, o, h, l, c in bars:
        if at.time() == TIME_EXIT:
            return dt.datetime.combine(trade["expiry"], TIME_EXIT, tzinfo=NY), o
    for at, o, h, l, c in reversed(bars):        # the fallback
        if at.time() <= TIME_EXIT:
            return at, c
    return None


# ------------------------------------------------------------------ counting

def hold_pnl(trade):
    """Holding to expiry - exactly the formula in `grid.prepare`."""
    breached = trade["loss"] > 0
    return (trade["credit"] - trade["loss"]) * 100 - EXIT_COST - (EXIT_COST if breached else 0.0)


def run_rule(trades, paths, ivs, kind, fraction=0.0, stress=1.0, soft=False):
    """kind: 'hold' | 'touch' | 'time'. Returns the outcome of each trade."""
    out = []
    for t in trades:
        iv_s, iv_l = ivs[t["key"]]
        if kind == "hold":
            out.append(dict(pnl=hold_pnl(t), closed=False, at=None))
            continue
        if kind == "time":
            got = time_exit_spot(t, paths[t["underlying"]])
            if got is None:
                out.append(dict(pnl=hold_pnl(t), closed=False, at=None))
                continue
            at, spot = got
            value = spread_value(t, spot, at, iv_s, iv_l, stress)
            out.append(dict(pnl=(t["credit"] - value) * 100 - 2 * EXIT_COST, closed=True, at=at))
            continue
        # the short strike touched, shifted by a share of the width
        step = fraction * t["width"]
        level = t["short_strike"] + step if t["kind"] == "call" else t["short_strike"] - step
        got = walk_soft(t, paths[t["underlying"]], level) if soft else walk(t, paths[t["underlying"]], level)
        if got is None:
            out.append(dict(pnl=hold_pnl(t), closed=False, at=None))
            continue
        at, spot = got
        value = spread_value(t, spot, at, iv_s, iv_l, stress)
        out.append(dict(pnl=(t["credit"] - value) * 100 - 2 * EXIT_COST, closed=True, at=at))
    return out


def walk_soft(trade, paths, level):
    """The same, but filled at the bar's close, without worsening to the level."""
    got = walk(trade, paths, level)
    if got is None:
        return None
    at, _ = got
    for a, o, h, l, c in paths.get(at.date(), ()):
        if a == at:
            return at, c
    return got


def drawdown(pnl):
    cum = np.cumsum(pnl)
    peak = np.maximum.accumulate(np.concatenate([[0.0], cum]))[1:]
    return float((cum - peak).min()) if len(cum) else 0.0


def summarize(name, outs, trades, base=None):
    pnl = np.array([o["pnl"] for o in outs])
    risk = np.array([t["risk_dollars"] for t in trades])
    row = dict(
        rule=name,
        trades=len(pnl),
        closed_early=sum(1 for o in outs if o["closed"]),
        mean_dollars=pnl.mean(),
        over_risk=(pnl / risk).mean(),
        win_share=(pnl > 0).mean(),
        total_dollars=pnl.sum(),
        drawdown_dollars=drawdown(pnl),
        worst_trade=pnl.min(),
    )
    if base is not None:
        b = np.array([o["pnl"] for o in base])
        row["win_turned_loss"] = int(((b > 0) & (pnl <= 0)).sum())
        row["loss_turned_win"] = int(((b <= 0) & (pnl > 0)).sum())
        row["versus_holding_dollars"] = pnl.mean() - b.mean()
    else:
        row["win_turned_loss"] = 0
        row["loss_turned_win"] = 0
        row["versus_holding_dollars"] = 0.0
    return row


def main():
    frame = pd.read_parquet(DATA / "candidates_bt.parquet")
    prepared = grid.prepare(frame)
    picked = grid.pick(prepared, DELTA_CAP, EDGE_MIN).sort_values(["day", "underlying"])
    # Pricing an exit needs the underlying's dividend yield, and DIV holds only the
    # names it was measured for. Dropping the rest is stated rather than silent: a
    # selection quietly shrunk reads afterwards as the whole population.
    outside = picked[~picked["underlying"].isin(DIV)]
    if len(outside):
        print(f"dropped {len(outside)} trades on {sorted(outside['underlying'].unique())}: "
              f"no dividend yield measured for them")
        picked = picked[picked["underlying"].isin(DIV)]
    print(f"selection: delta ceiling {DELTA_CAP}, edge threshold {EDGE_MIN:+.0f}, "
          f"width 1-5 strikes")
    print(f"trades {len(picked)}, trading days with at least one {picked['day'].nunique()}, "
          f"period {picked['day'].min()} .. {picked['day'].max()}")
    print(f"underlyings: {dict(picked.groupby('underlying').size())}, "
          f"width in dollars: {sorted(picked['width'].unique())}")
    print(f"cost: book {grid.SLIP} + fees {grid.FEE} = {EXIT_COST:.0f} usd "
          f"for one crossing of the book per spread\n")

    paths = {s: load_paths(s) for s in picked["underlying"].unique()}
    legs = {s: load_leg_prices(s) for s in picked["underlying"].unique()}

    trades, ivs, no_long, no_iv = [], {}, 0, 0
    for i, r in picked.iterrows():
        t = dict(key=i, day=r["day"], expiry=r["expiry"], underlying=r["underlying"],
                 kind=r["kind"], short_strike=r["short_strike"], long_strike=r["long_strike"],
                 width=r["width"], credit=r["credit"], loss=r["loss"],
                 risk_dollars=r["risk_dollars"], touched=bool(r["touched"]))
        div = DIV[t["underlying"]]
        long_price = legs[t["underlying"]].get(
            (t["day"], t["expiry"].isoformat(), t["kind"], t["long_strike"]))
        iv_s = float(r["iv"])
        if long_price is None:
            no_long += 1
            iv_l = iv_s
        else:
            iv_l = implied(t["kind"], long_price, r["spot"], t["long_strike"], r["years"], div)
            if not np.isfinite(iv_l):
                no_iv += 1
                iv_l = iv_s
        trades.append(t)
        ivs[i] = (iv_s, iv_l)
    print(f"volatility of the long leg: no price found for {no_long}, "
          f"not recovered for {no_iv} - there the short leg's volatility is used")

    # a check on the model: at entry the repricing must return the market credit
    err = []
    for t in trades:
        at = dt.datetime.combine(t["day"], ENTRY_TIME, tzinfo=NY)
        spot = float(picked.loc[t["key"], "spot"])
        err.append(spread_value(t, spot, at, *ivs[t["key"]]) - t["credit"])
    err = np.array(err)
    print(f"model agreement at entry: median |error| {np.median(np.abs(err)):.4f} usd, "
          f"worst {np.abs(err).max():.4f} usd\n")

    print(f"spread width in dollars: median {picked['width'].median():.2f}, "
          f"three quarters below {picked['width'].quantile(0.75):.2f} - a share of the width "
          f"is a SMALL shift of the level, so shares above one are taken too\n")

    FRACTIONS = (0.25, 0.5, 1.0, 1.5, 2.0)
    base = run_rule(trades, paths, ivs, "hold")
    rows = [summarize("1. hold to expiry", base, trades)]
    rows.append(summarize("2. the short strike touched",
                          run_rule(trades, paths, ivs, "touch", 0.0), trades, base))
    for f in FRACTIONS:
        rows.append(summarize(f"3. past the strike by {f:.2f} of the width",
                              run_rule(trades, paths, ivs, "touch", f), trades, base))
    rows.append(summarize("4. an hour before the bell",
                          run_rule(trades, paths, ivs, "time"), trades, base))

    cols = ["rule", "trades", "closed_early", "mean_dollars", "over_risk",
            "win_share", "drawdown_dollars", "worst_trade", "total_dollars",
            "win_turned_loss", "loss_turned_win", "versus_holding_dollars"]
    table = pd.DataFrame(rows)[cols]
    print("=== the exit rules, baseline ===")
    print(table.to_string(index=False, float_format=lambda v: f"{v:.3f}"))

    print("\n=== the same under stress: volatility at exit +25% ===")
    rows_s = [summarize("1. hold to expiry", base, trades)]
    rows_s.append(summarize("2. the short strike touched",
                            run_rule(trades, paths, ivs, "touch", 0.0, IV_STRESS), trades, base))
    for f in FRACTIONS:
        rows_s.append(summarize(f"3. past the strike by {f:.2f} of the width",
                                run_rule(trades, paths, ivs, "touch", f, IV_STRESS), trades, base))
    rows_s.append(summarize("4. an hour before the bell",
                            run_rule(trades, paths, ivs, "time", stress=IV_STRESS), trades, base))
    print(pd.DataFrame(rows_s)[cols].to_string(index=False, float_format=lambda v: f"{v:.3f}"))

    print("\n=== a softer fill: at the bar's close, without worsening to the level ===")
    rows_soft = [summarize("1. hold to expiry", base, trades)]
    rows_soft.append(summarize("2. the short strike touched",
                               run_rule(trades, paths, ivs, "touch", 0.0, soft=True), trades, base))
    for f in FRACTIONS:
        rows_soft.append(summarize(f"3. past the strike by {f:.2f} of the width",
                                   run_rule(trades, paths, ivs, "touch", f, soft=True), trades, base))
    print(pd.DataFrame(rows_soft)[cols].to_string(index=False, float_format=lambda v: f"{v:.3f}"))

    # --------------------------------------------- what going undefended costs
    print("\n=== what NOT closing costs: losing the whole width ===")
    full = [t for t in trades if t["loss"] >= t["width"] - 1e-9]
    print(f"trades in all {len(trades)}")
    print(f"reached a full loss of the width when held to expiry: {len(full)} "
          f"({len(full)/len(trades)*100:.1f}%)")
    if full:
        losses = np.array([hold_pnl(t) for t in full])
        print(f"  their total {losses.sum():+.0f} usd, worst trade {losses.min():+.0f} usd, "
              f"median {np.median(losses):+.0f} usd")
        print(f"  median risk of those trades {np.median([t['risk_dollars'] for t in full]):.0f} usd")
        by = defaultdict(int)
        for t in full:
            by[(t["underlying"], t["kind"])] += 1
        print(f"  breakdown: {dict(by)}")
    touched = [t for t in trades if t["touched"]]
    survived = [t for t in touched if t["loss"] <= 0]
    print(f"touched the short strike by daily highs and lows: {len(touched)} "
          f"({len(touched)/len(trades)*100:.1f}%), of them finished out of the money {len(survived)} "
          f"({len(survived)/len(trades)*100:.1f}% of all trades)")

    # the same on the 15-minute path rather than daily extremes
    hit = run_rule(trades, paths, ivs, "touch", 0.0)
    n_hit = sum(1 for o in hit if o["closed"])
    hit_full = sum(1 for t, o in zip(trades, hit) if o["closed"] and t["loss"] >= t["width"] - 1e-9)
    hit_ok = sum(1 for t, o in zip(trades, hit) if o["closed"] and t["loss"] <= 0)
    print(f"on the 15-minute path the short strike was passed in {n_hit} trades "
          f"({n_hit/len(trades)*100:.1f}%); of them, when held to expiry, "
          f"{hit_ok} finished out of the money ({hit_ok/max(n_hit,1)*100:.1f}%), "
          f"{hit_full} reached the full width ({hit_full/max(n_hit,1)*100:.1f}%)")

    # ------------------------------------------------ why the defence does not save
    print("\n=== where the defence fires and what it lands on ===")
    when = defaultdict(int)
    deep = 0
    for t, o in zip(trades, hit):
        if not o["closed"]:
            continue
        at = o["at"]
        when["at the open (first bar of the day)" if at.time() == OPEN_TIME else "intraday"] += 1
        if at.date() != t["day"] and at.time() == OPEN_TIME:
            deep += 1
    print(f"of {n_hit} firings: {dict(when)}")
    print(f"of them at the open on a day after entry (an overnight gap): {deep} "
          f"({deep/max(n_hit,1)*100:.1f}%) - there the price is already past the strike, "
          f"and the defence closes after the fact, not ahead of it")
    worst = sorted(zip(trades, hit), key=lambda p: p[1]["pnl"])[:5]
    print("the five worst trades under the defence in force:")
    for t, o in worst:
        print(f"  {t['day']} {t['underlying']} {t['kind']} strikes "
              f"{t['short_strike']:.0f}/{t['long_strike']:.0f} risk {t['risk_dollars']:.0f} usd: "
              f"defended {o['pnl']:+8.1f}, held {hold_pnl(t):+8.1f}, "
              f"exit {'none' if o['at'] is None else o['at'].strftime('%Y-%m-%d %H:%M')}")

    # ------------------------------- what the defence's loss is actually made of
    print("\n=== the breakdown: why defending loses to holding ===")
    breached = [t for t in trades if t["loss"] > 0]
    print(f"held to expiry, the spread ends breached in {len(breached)} trades "
          f"({len(breached)/len(trades)*100:.1f}%) - only there does holding pay to get out")
    extra_cost = (n_hit - len(breached)) * EXIT_COST
    diff = sum(o["pnl"] for o in hit) - sum(o["pnl"] for o in base)
    print(f"extra crossings the defence pays: {n_hit} - {len(breached)} = {n_hit-len(breached)}, "
          f"that is {-extra_cost:+.0f} usd, or {-extra_cost/len(trades):+.2f} usd a trade")
    print(f"the whole loss of defending: {diff:+.0f} usd, or {diff/len(trades):+.2f} usd a trade")
    print(f"the rest is the difference in the outcome itself: {diff+extra_cost:+.0f} usd, "
          f"or {(diff+extra_cost)/len(trades):+.2f} usd a trade")
    vals = [(t["credit"] - (o["pnl"] + 2 * EXIT_COST) / 100) / t["width"]
            for t, o in zip(trades, hit) if o["closed"]]
    vals = np.array(vals)
    print(f"what the spread was worth when the defence fired, as a share of the width: median {np.median(vals):.2f}, "
          f"quartiles {np.percentile(vals,25):.2f}/{np.percentile(vals,75):.2f}, "
          f"share of firings where it was already worth more than 0.9 of the width: {(vals>0.9).mean():.2f}")

    print("\n=== the worst day and the worst run under each rule ===")
    named = [("1. hold", base),
             ("2. touched", hit),
             ("3. 0.25 of the width", run_rule(trades, paths, ivs, "touch", 0.25)),
             ("3. 0.50 of the width", run_rule(trades, paths, ivs, "touch", 0.5)),
             ("3. 1.00 of the width", run_rule(trades, paths, ivs, "touch", 1.0)),
             ("3. 2.00 of the width", run_rule(trades, paths, ivs, "touch", 2.0)),
             ("4. an hour before", run_rule(trades, paths, ivs, "time"))]
    for name, outs in named:
        pnl = np.array([o["pnl"] for o in outs])
        days = defaultdict(float)
        for t, p in zip(trades, pnl):
            days[t["day"]] += p
        worst_day = min(days.items(), key=lambda kv: kv[1])
        print(f"{name:16} worst trade {pnl.min():8.0f}  worst day {worst_day[1]:8.0f} "
              f"({worst_day[0]})  drawdown {drawdown(pnl):8.0f}  total {pnl.sum():9.0f}")


if __name__ == "__main__":
    main()
