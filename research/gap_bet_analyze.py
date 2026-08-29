"""Steps 3-5: how the bet on the gap turned out, and what share of equity it is worth.

Reads data/gap_raw/*.json, collected by gap_bet_collect.py. Downloads nothing.

The rules baked in here, which have to be read alongside the numbers:

ENTRY. The last minute of the window before Thursday's close whose bar carries at
least MIN_TRADES trades and at least MIN_VOL contracts. A bar with one or two
trades is not a price; such observations are thrown out whole.

ENTRY PRICE. There is NO historical option order book available to us (checked -
see DATA.md), so the buyer's price has to be estimated. We take the minute's close
plus max(2 cents, 2% of price) per leg - a crude stand-in for half the spread. The
same concession downward on the way out. The variant without the markup is computed
alongside.

EXIT. Three rules: the close of the 9:35 minute, the close of the 10:00 minute, and
the best sum of the legs across 9:30-10:30. The third is unreachable in life - it is
a ceiling.
"""

import json
import statistics
from datetime import date, datetime
from pathlib import Path
from zoneinfo import ZoneInfo

HERE = Path(__file__).resolve().parent
RAW = HERE / "data" / "gap_raw"
NY = ZoneInfo("America/New_York")

MIN_TRADES = 5      # trades in the entry minute bar
MIN_VOL = 20        # contracts in the entry minute bar
FILTER = {"n": MIN_TRADES, "v": MIN_VOL}
SHARE_STRADDLE = 2 / 3
SHARE_STRANGLE = 1 / 3

STRADDLE = ("straddle_call", "straddle_put")
STRANGLE = ("strangle_call", "strangle_put")


def ny(stamp: str) -> datetime:
    return datetime.fromisoformat(stamp.replace("Z", "+00:00")).astimezone(NY)


def fee(price: float) -> float:
    """The half-spread we assigned ourselves. Named at the top of this file."""
    return max(0.02, 0.02 * price)


def entry_leg(bars: list[dict]) -> dict | None:
    good = [b for b in bars if b.get("n", 0) >= FILTER["n"] and b.get("v", 0) >= FILTER["v"]]
    return good[-1] if good else None


def series(bars: list[dict]) -> dict[str, dict]:
    return {ny(b["t"]).strftime("%H:%M"): b for b in bars}


def intrinsic(symbol: str, spot: float) -> float:
    strike = int(symbol[-8:]) / 1000
    return max(0.0, spot - strike) if symbol[-9] == "C" else max(0.0, strike - spot)


def leg_exit(symbol: str, bars: list[dict], want: str, spy_at: dict[str, float]) -> tuple[float, str]:
    """The price of a leg in the minute wanted. With no trades, the last one before it;
    with none at all, intrinsic value against SPY (zero when out of the money)."""
    have = series(bars)
    if want in have:
        return have[want]["c"], "bar"
    earlier = [m for m in sorted(have) if m <= want]
    if earlier:
        return have[earlier[-1]]["c"], "an earlier minute"
    spot = spy_at.get(want) or (list(spy_at.values())[0] if spy_at else 0.0)
    return intrinsic(symbol, spot), "intrinsic value"


def analyse(case: dict) -> dict | None:
    legs = case["legs"]
    if any(legs.get(k) is None for k in STRADDLE + STRANGLE):
        return {"skip": "the four contracts were not all found"}

    pre, post = case["opt_pre"], case["opt_post"]
    entry, missing = {}, []
    for key in STRADDLE + STRANGLE:
        bar = entry_leg(pre.get(legs[key]) or [])
        if bar is None:
            missing.append(key)
        else:
            entry[key] = bar
    if missing:
        return {"skip": f"illiquid at entry, or no bars: {', '.join(missing)}"}
    if not any(post.get(legs[k]) for k in STRADDLE):
        return {"skip": "no morning option bars (a hole in Alpaca's data)"}

    spy_at = {ny(b["t"]).strftime("%H:%M"): b["c"] for b in case["spy_post"]}

    def package(keys, minute, fees):
        total, how = 0.0, []
        for k in keys:
            price, source = leg_exit(legs[k], post.get(legs[k]) or [], minute, spy_at)
            total += max(0.0, price - (fee(price) if fees else 0.0))
            how.append(source)
        return total, how

    def cost(keys, fees):
        return sum(entry[k]["c"] + (fee(entry[k]["c"]) if fees else 0.0) for k in keys)

    minutes = sorted(spy_at)
    hour = [m for m in minutes if "09:30" <= m <= "10:30"]

    out = {"day": case["report_day"], "entry_day": case["entry_day"],
           "spot": case["spot_entry"], "strikes": case["strikes"],
           "window": case.get("entry_window_ny"),
           "entry_n": {k: entry[k]["n"] for k in entry},
           "gap_pct": (spy_at[hour[0]] / case["spot_entry"] - 1) * 100 if hour else None}

    for fees in (True, False):
        tag = "spread" if fees else "clean"
        cs, cg = cost(STRADDLE, fees), cost(STRANGLE, fees)
        out[f"cost_{tag}"] = {"straddle": cs, "strangle": cg}
        res, sources = {}, []
        for name, minute in (("0935", "09:35"), ("1000", "10:00")):
            vs, h1 = package(STRADDLE, minute, fees)
            vg, h2 = package(STRANGLE, minute, fees)
            sources += h1 + h2
            res[name] = SHARE_STRADDLE * (vs / cs - 1) + SHARE_STRANGLE * (vg / cg - 1)
        best = max(
            SHARE_STRADDLE * (package(STRADDLE, m, fees)[0] / cs - 1)
            + SHARE_STRANGLE * (package(STRANGLE, m, fees)[0] / cg - 1)
            for m in hour
        )
        res["best"] = best
        out[f"ret_{tag}"] = res
        out[f"sources_{tag}"] = sources
    return out


def table(name: str, values: list[float]) -> None:
    values = sorted(values)
    n = len(values)
    burnt = sum(1 for v in values if v <= -0.90)
    half = sum(1 for v in values if v <= -0.50)
    win = sum(1 for v in values if v > 0)
    print(f"  {name:>22}: n={n}  worst {values[0]:+.1%}  median {statistics.median(values):+.1%}  "
          f"best {values[-1]:+.1%}  mean {statistics.fmean(values):+.1%}")
    print(f"  {'':>22}  in profit {win}/{n} ({win / n:.0%})   lost >=50%: {half}/{n}   "
          f"lost >=90%: {burnt}/{n}")


def main() -> None:
    files = sorted(RAW.glob("*.json"))
    if FILTER["n"] > 1:
        print("=" * 72)
        print("FIRST a check: what the three dates dropped by the liquidity filter give")
        FILTER["n"], FILTER["v"] = 1, 1
        loose = {}
        for f in files:
            got = analyse(json.loads(f.read_text()))
            if got and "skip" not in got:
                loose[got["day"]] = got["ret_spread"]
        FILTER["n"], FILTER["v"] = MIN_TRADES, MIN_VOL
        for day in ("2024-12-06", "2025-09-05", "2026-06-05"):
            if day in loose:
                r = loose[day]
                print(f"  {day}: 9:35 {r['0935']:+.0%}, 10:00 {r['1000']:+.0%} - "
                      f"from a bar with 2-3 trades - not trustworthy, kept out of the total")
        vals = [v["0935"] for v in loose.values()]
        print(f"  across all {len(vals)} dates unfiltered: median {statistics.median(vals):+.1%}, "
              f"worst {min(vals):+.1%}, mean {statistics.fmean(vals):+.1%}")
        print("=" * 72 + "\n")
    cases, skipped = [], []
    for f in files:
        got = analyse(json.loads(f.read_text()))
        if got is None or "skip" in got:
            skipped.append((f.stem, got["skip"] if got else "no data"))
        else:
            cases.append(got)

    print(f"dates with raw data: {len(files)};  used {len(cases)};  dropped {len(skipped)}")
    for day, why in skipped:
        print(f"  dropped {day}: {why}")

    fallbacks = sum(1 for c in cases for s in c["sources_spread"] if s != "bar")
    total = sum(len(c["sources_spread"]) for c in cases)
    print(f"\nexit quotes in all {total}, of them taken from another minute: {fallbacks}")

    print("\n=== the bet by date (with the spread markup) ===")
    print("entry price is per one contract of each structure, in dollars")
    print(f"{'date':<12}{'gap':>8}{'straddle':>9}{'strangle':>9}{'9:35':>9}{'10:00':>9}{'best':>9}")
    for c in sorted(cases, key=lambda x: x["day"]):
        r = c["ret_spread"]
        print(f"{c['day']:<12}{c['gap_pct']:>+7.2f}%"
              f"{c['cost_spread']['straddle'] * 100:>9.0f}{c['cost_spread']['strangle'] * 100:>9.0f}"
              f"{r['0935']:>+8.0%}{r['1000']:>+8.0%}{r['best']:>+8.0%}")

    print("\n=== the spread of outcomes, as a share of what was paid ===")
    for tag, title in (("spread", "with the spread markup"), ("clean", "without the markup")):
        print(f"\n{title}:")
        for rule, label in (("0935", "sell at 9:35"), ("1000", "sell at 10:00"),
                            ("best", "the best price of the hour (a ceiling)")):
            table(label, [c[f"ret_{tag}"][rule] for c in cases])

    print("\n=== Fridays only ===")
    fri = [c for c in cases if date.fromisoformat(c["day"]).weekday() == 4]
    for rule, label in (("0935", "sell at 9:35"), ("1000", "sell at 10:00")):
        table(label, [c["ret_spread"][rule] for c in fri])

    print("\n=== how much to stake: an account of $100,000 ===")
    for rule in ("0935", "1000"):
        worst = min(c["ret_spread"][rule] for c in cases)
        print(f"\nexit rule {rule}: the worst outcome in history {worst:+.1%}")
        for f in (0.02, 0.05, 0.10, 0.20, 0.33):
            one = 100_000 * (1 + f * worst)
            print(f"  share {f:>4.0%}: one worst bet -> ${one:,.0f} "
                  f"({f * worst:+.1%} of the account);  three in a row -> ${100_000 * (1 + f * worst) ** 3:,.0f};  "
                  f"five in a row -> ${100_000 * (1 + f * worst) ** 5:,.0f}")

    print("\n  a total loss (-100%, the stake burned to nothing):")
    for f in (0.05, 0.20):
        row = [100_000 * (1 - f) ** k for k in range(6)]
        print(f"  share {f:>4.0%}: " + "  ".join(f"{k}:${v:,.0f}" for k, v in enumerate(row)))

    print("\n=== the tail: if the exit failed and the position lived to expiry ===")
    closes = {r["t"][:10]: r["c"] for r in
              json.loads((HERE / "data" / "spy_daily.json").read_text())}
    tail = []
    for c in sorted(cases, key=lambda x: x["day"]):
        close = closes.get(c["day"])
        if close is None:
            continue
        k = c["strikes"]
        pay_s = max(0.0, close - k["atm"]) + max(0.0, k["atm"] - close)
        pay_g = max(0.0, close - k["call_out"]) + max(0.0, k["put_out"] - close)
        r = (SHARE_STRADDLE * (pay_s / c["cost_spread"]["straddle"] - 1)
             + SHARE_STRANGLE * (pay_g / c["cost_spread"]["strangle"] - 1))
        tail.append(r)
    table("to expiry", tail)
    print("       (settlement is SPY's close on the report day; exchange fees for "
          "automatic exercise are not counted)")

    print("\n=== losing runs, as they actually came ===")
    for rule in ("0935", "1000"):
        seq = [c["ret_spread"][rule] for c in sorted(cases, key=lambda x: x["day"])]
        run = best_run = 0
        for r in seq:
            run = run + 1 if r < 0 else 0
            best_run = max(best_run, run)
        deep = [r for r in seq if r <= -0.30]
        print(f"  {rule}: the longest run of losing bets in a row - {best_run}; "
              f"bets losing 30% or worse - {len(deep)} of {len(seq)}")
        for f in (0.05, 0.20):
            money = 100_000.0
            low = money
            for r in seq:
                money *= 1 + f * r
                low = min(low, money)
            print(f"      share {f:.0%}: walking the whole history in order, the account ends at {money:,.0f} "
                  f"(down to {low:,.0f} on the way)")
        worst = min(seq)
        print(f"      worst bet {worst:+.1%}; a run of {best_run} worst in a row at 5% -> "
              f"{100_000 * (1 + 0.05 * worst) ** best_run:,.0f}, at 20% -> "
              f"{100_000 * (1 + 0.20 * worst) ** best_run:,.0f}")

    json.dump(cases, (HERE / "data" / "gap_bet_results.json").open("w"), indent=1)


if __name__ == "__main__":
    main()
