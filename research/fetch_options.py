"""Pulls what a trade is built from: the contracts that existed, and their price
at the moment of entry.

The moment of entry is the same every day - the fifteen-minute bar that opens at
14:00 in New York. It is that bar rather than the close because the envelope
allows an expiration ZERO trading days out, and at the close a zero-day option has
no time left at all: Black-Scholes degenerates on it. At 14:00 it has two hours of
life, and that is the trade the rule actually opens.

A leg's price is the close of that fifteen-minute bar - the last trade inside the
window. Checked on SPY for 13 March 2025: the daily close matched the close of the
last fifteen-minute bar on all 95 contracts that traded inside the window. Where
nothing traded inside the window there is no price, and the contract is left out
rather than carried at yesterday's.
"""

import datetime as dt
import json
import sys
import random
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
from zoneinfo import ZoneInfo

import alpaca

# A limiter and a retry of its own. The broker allows 200 requests a minute, and
# the first attempt at two threads of five workers ran into 429: the queue inside
# `alpaca.get` gives up after four tries, and a day's pull then loses the whole
# day.
_LAST = [0.0]
_LOCK = threading.Lock()
PER_MINUTE = 60


def paced(url, params, tries=8):
    for attempt in range(tries):
        with _LOCK:
            wait = _LAST[0] + 60.0 / PER_MINUTE - time.monotonic()
            if wait > 0:
                time.sleep(wait)
            _LAST[0] = time.monotonic()
        try:
            return alpaca.get(url, params, tries=1)
        except Exception:
            # The back-off doubles: Alpaca throttles in bursts, and an even step
            # meets the same ceiling eight times running.
            time.sleep(min(60.0, 3.0 * 2 ** attempt) + random.random())
    raise RuntimeError(f"{url}: no answer in {tries} tries")


HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
NY = ZoneInfo("America/New_York")
UNDERLYINGS = ["SPY", "QQQ", "IWM"]
FIRST = dt.date(2024, 1, 1)
LAST = dt.date(2026, 8, 26)
# The entry window, New York time.
WINDOW = dt.time(14, 0)
# How many trading days ahead to look for expirations.
AHEAD = 5
# The band of strikes around the price, as a fraction. A delta of 0.45 stands
# almost at the money, while a delta of 0.02 in high volatility walks out some ten
# per cent - so the band is generous, and the extra strikes cost only disk.
BAND = 0.10
# The band in which BARS are actually needed. Narrower than the band of the
# listing: the screener looks at a sold strike 0.3 to 3.0 per cent from the price,
# and the long leg walks another five strikes out - on IWM that is a further two
# and a half per cent. Six and a half covers both sides with room to spare and
# halves the pull.
BAND_BARS = 0.055


def stock(symbol, name):
    return json.loads((DATA / f"stock_{symbol}_{name}.json").read_text())


def trading_days(symbol="SPY"):
    days = [dt.date.fromisoformat(b["t"][:10]) for b in stock(symbol, "day")]
    return [d for d in days if FIRST <= d <= LAST]


def window_bars(symbol):
    """The fifteen-minute bar opening at 14:00 New York, for every day."""
    out = {}
    for bar in stock(symbol, "15m"):
        at = dt.datetime.fromisoformat(bar["t"].replace("Z", "+00:00")).astimezone(NY)
        if at.time() == WINDOW:
            out[at.date()] = bar
    return out


def contracts_for(symbol, first, last, low, high):
    out, token = [], None
    while True:
        params = {"underlying_symbols": symbol, "status": "inactive",
                  "expiration_date_gte": first.isoformat(),
                  "expiration_date_lte": last.isoformat(),
                  "strike_price_gte": f"{low:.2f}", "strike_price_lte": f"{high:.2f}",
                  "limit": 10000}
        if token:
            params["page_token"] = token
        answer = paced(f"{alpaca.TRADE}/v2/options/contracts", params)
        out.extend(answer.get("option_contracts") or [])
        token = answer.get("next_page_token")
        if not token:
            break
    return out


def gather_contracts(symbol):
    path = DATA / f"contracts_{symbol}.json"
    if path.exists():
        return json.loads(path.read_text())

    days = trading_days()
    closes = {dt.date.fromisoformat(b["t"][:10]): b["c"] for b in stock(symbol, "day")}
    found = {}
    weeks = []
    step = 7
    start = FIRST
    while start <= LAST:
        stop = start + dt.timedelta(days=step - 1)
        inside = [d for d in days if start <= d <= stop + dt.timedelta(days=12)]
        prices = [closes[d] for d in inside if d in closes]
        if prices:
            weeks.append((start, stop, min(prices) * (1 - BAND), max(prices) * (1 + BAND)))
        start = stop + dt.timedelta(days=1)

    def one(job):
        first, last, low, high = job
        return contracts_for(symbol, first, last, low, high)

    with ThreadPoolExecutor(max_workers=4) as pool:
        for chunk in pool.map(one, weeks):
            for c in chunk:
                found[c["symbol"]] = {
                    "symbol": c["symbol"], "type": c["type"],
                    "strike": float(c["strike_price"]),
                    "expiration": c["expiration_date"],
                }
    path.write_text(json.dumps(list(found.values())))
    print(f"{symbol}: {len(found)} contracts", flush=True)
    return list(found.values())


def gather_bars(symbol):
    path = DATA / f"obars_{symbol}.jsonl"
    done = set()
    if path.exists():
        for line in path.open():
            done.add(json.loads(line)["day"])

    contracts = gather_contracts(symbol)
    by_expiry = {}
    for c in contracts:
        by_expiry.setdefault(c["expiration"], []).append(c)

    days = trading_days()
    windows = window_bars(symbol)
    jobs = []
    for i, day in enumerate(days):
        if day.isoformat() in done or day not in windows:
            continue
        spot = windows[day]["c"]
        ahead = [d for d in days[i:i + AHEAD + 1]]
        want = []
        for e in ahead:
            for c in by_expiry.get(e.isoformat(), []):
                if spot * (1 - BAND_BARS) <= c["strike"] <= spot * (1 + BAND_BARS):
                    want.append(c["symbol"])
        if want:
            jobs.append((day, want))

    print(f"{symbol}: {len(jobs)} days to pull", flush=True)

    def one(job):
        day, want = job
        opens = dt.datetime.combine(day, WINDOW, tzinfo=NY).astimezone(dt.timezone.utc)
        closes = opens + dt.timedelta(minutes=15)
        rows = []
        for i in range(0, len(want), 900):
            part = want[i:i + 900]
            token = None
            while True:
                params = {"symbols": ",".join(part), "timeframe": "15Min",
                          "start": opens.isoformat().replace("+00:00", "Z"),
                          "end": (closes - dt.timedelta(seconds=1)).isoformat().replace("+00:00", "Z"),
                          "limit": 10000}
                if token:
                    params["page_token"] = token
                answer = paced(f"{alpaca.DATA}/v1beta1/options/bars", params)
                for name, bars in (answer.get("bars") or {}).items():
                    for b in bars:
                        rows.append({"s": name, "c": b["c"], "o": b["o"], "h": b["h"],
                                     "l": b["l"], "n": b["n"], "v": b["v"]})
                token = answer.get("next_page_token")
                if not token:
                    break
        return {"day": day.isoformat(), "bars": rows}

    with path.open("a") as out, ThreadPoolExecutor(max_workers=8) as pool:
        for n, got in enumerate(pool.map(one, jobs), 1):
            out.write(json.dumps(got) + "\n")
            if n % 50 == 0:
                print(f"  {symbol}: {n}/{len(jobs)}", flush=True)
            out.flush()


if __name__ == "__main__":
    for name in (sys.argv[1:] or UNDERLYINGS):
        gather_bars(name)
