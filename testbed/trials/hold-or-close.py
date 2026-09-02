#!/usr/bin/env python3
"""Hold the spread to expiry, or close it before the day the result is counted?

The question came from the team on 2 September with a deadline attached: both
books hold short call spreads on SPY expiring 8-9 September, and the result is
counted at Thursday's close on the 4th. What lands in the number is therefore
not the credit received but the MARK - what the position is worth with three or
four days of time value still inside it.

Two things are being compared, and only two:

    close it now      you pay today's debit. Certain, and it is known.
    hold it           you owe Thursday's mark. Unknown, and it has a shape.

So the whole measurement is the shape of Thursday's mark. It comes from three
real numbers and one model:

  * the last quote of every leg, from the broker - the level;
  * each leg's own implied volatility, from the broker - how the level moves;
  * every two-trading-day move SPY has actually made since 2016 - what to move it by;
  * Black-Scholes, used ONLY for the DIFFERENCE between two prices, never for the
    level. The market's own mid is the anchor; the model says how far the mid
    travels when the spot moves and two days fall off the clock. A model that is
    wrong about the level by a few cents - and every model is - cancels.

What this deliberately does NOT model, and both matter:

  * the volatility smile moves too. Each leg is repriced at its own volatility
    as it stands today (sticky-strike). A sell-off would raise it, which makes
    the SHORT call side of this look slightly better than reality;
  * the empirical distribution is drawn from ten years including 2020 and 2022.
    Those tails belong to a market that was not this one, so the run also reports
    the answer conditioned on periods whose realised volatility looked like now.
    The conditional number is the answer; the unconditional one is the reminder
    that the tail is not zero.

    arena/trials/hold-or-close.py [bars.json]
"""
import json
import math
import os
import subprocess
import sys
from datetime import datetime, timezone

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
UPSTREAM = os.environ.get("ARENA_UPSTREAM", "http://127.0.0.1:8000/mcp")
# The interpreter that has the `mcp` package. Named through the environment
# rather than hard-coded: the one on PATH is usually not it.
PYTHON = os.environ.get("MCP_PYTHON", "python3")

# The moment everything is anchored at, and the moment the result is counted.
# Both are closes of the exchange, in UTC, because that is when the mark that
# lands in the number is taken.
ANCHOR = datetime(2026, 9, 1, 20, 0, tzinfo=timezone.utc)
# The end of THURSDAY the 3rd, not Friday. Alpaca takes the account's equity at
# Thursday's close; LabLab counts at 15:00 UTC on Friday, four hours before that
# session ends. Both dates are in the team's own declaration, three times over
# (agent/alpaca-agent-1.yaml). Read as Friday, this measurement gives every
# position one more day of decay than it will get and quietly flatters holding -
# which is exactly what it did on its first run.
JUDGED = datetime(2026, 9, 3, 20, 0, tzinfo=timezone.utc)
# How many trading days separate them: Wednesday's close and Thursday's.
HORIZON = 2

# The positions, as the team reported them on 2 September. A short call spread:
# the near strike is sold, the far one bought.
POSITIONS = [
    {"book": "1", "sold": "SPY260908C00775000", "bought": "SPY260908C00776000",
     "sets": 112, "credit": 0.19},
    {"book": "1", "sold": "SPY260909C00768000", "bought": "SPY260909C00769000",
     "sets": 133, "credit": 0.31},
    {"book": "2", "sold": "SPY260908C00772000", "bought": "SPY260908C00773000",
     "sets": 128, "credit": 0.31},
]


def norm_cdf(x):
    return 0.5 * math.erfc(-x / math.sqrt(2))


def bs(call, spot, strike, years, sigma):
    """Black-Scholes at a zero rate. Past expiry, or with no volatility, the
    intrinsic value - the formula's own limit, and the case a walk into the last
    hour hits."""
    if years <= 0 or sigma <= 0:
        return max(0.0, spot - strike) if call else max(0.0, strike - spot)
    d1 = (math.log(spot / strike) + sigma * sigma / 2 * years) / (sigma * math.sqrt(years))
    d2 = d1 - sigma * math.sqrt(years)
    if call:
        return spot * norm_cdf(d1) - strike * norm_cdf(d2)

    return strike * norm_cdf(-d2) - spot * norm_cdf(-d1)


def occ(symbol):
    """Strike and expiry out of the contract's own name."""
    tail = symbol[-15:]
    expires = datetime.strptime(tail[:6], "%y%m%d").replace(hour=20, tzinfo=timezone.utc)

    return float(tail[7:]) / 1000, expires


def mcp(tool, args):
    raw = subprocess.run([PYTHON, f"{ROOT}/arena/trials/mcp-call.py", UPSTREAM, tool,
                          json.dumps(args)], capture_output=True, text=True, check=True).stdout

    return json.loads(raw)["data"]


def percentile(values, p):
    ordered = sorted(values)
    at = (len(ordered) - 1) * p / 100

    lo, hi = math.floor(at), math.ceil(at)
    if lo == hi:
        return ordered[lo]

    return ordered[lo] + (ordered[hi] - ordered[lo]) * (at - lo)


# --- the market, as it stands -------------------------------------------------

legs = sorted({p[side] for p in POSITIONS for side in ("sold", "bought")})
snap = mcp("get_option_snapshot", {"symbols": ",".join(legs)})["snapshots"]
missing = [s for s in legs if s not in snap]
if missing:
    raise SystemExit(f"the broker quotes nothing for {missing}: nothing can be marked")

quotes = {}
for symbol in legs:
    row = snap[symbol]
    q = row["latestQuote"]
    iv = row.get("impliedVolatility")
    if not iv:
        raise SystemExit(f"{symbol} came back with no implied volatility, and the mark cannot be modelled without one")
    quotes[symbol] = {"bid": q["bp"], "ask": q["ap"], "mid": (q["bp"] + q["ask"]) / 2 if False else (q["bp"] + q["ap"]) / 2,
                      "iv": iv, "at": q["t"]}

bars_path = sys.argv[1] if len(sys.argv) > 1 else None
if bars_path:
    bars = json.load(open(bars_path))["data"]["bars"]["SPY"]
else:
    bars = mcp("get_stock_bars", {"symbols": "SPY", "timeframe": "1Day",
                                  "start": "2015-01-01", "limit": 10000})["bars"]["SPY"]
closes = [b["c"] for b in bars]
spot = closes[-1]

# --- what SPY actually does over two days ------------------------------------

moves = [closes[i + HORIZON] / closes[i] for i in range(len(closes) - HORIZON)]

# Realised volatility over the trailing ten days, for each of those moments, so
# the sample can be narrowed to markets that looked like this one.
def realised(at):
    window = closes[at - 10:at + 1]
    rets = [math.log(window[i + 1] / window[i]) for i in range(len(window) - 1)]
    mean = sum(rets) / len(rets)
    var = sum((r - mean) ** 2 for r in rets) / (len(rets) - 1)

    return math.sqrt(var * 252)


now_vol = realised(len(closes) - 1)
quiet = [moves[i] for i in range(10, len(moves)) if abs(realised(i) - now_vol) <= 0.3 * now_vol]

print(f"SPY closed at {spot:.2f} on {bars[-1]['t'][:10]}. Realised volatility over the last "
      f"ten days: {now_vol:.1%}.")
print(f"Two-day moves on record since {bars[0]['t'][:10]}: {len(moves)}, of which {len(quiet)} "
      f"happened in a market whose realised volatility was within 30% of today's.")
print(f"Quotes are the broker's own, stamped {quotes[legs[0]]['at'][:19]}.\n")


def mark_at(position, s_thu):
    """What the spread is worth at Thursday's close if SPY is at s_thu.

    The market's own mid is the anchor and the model supplies only the travel:
    what the mid becomes when the spot moves and two days fall off the clock.
    """
    total = 0.0
    for side, sign in (("sold", 1), ("bought", -1)):
        symbol = position[side]
        q = quotes[symbol]
        strike, expires = occ(symbol)
        t_now = (expires - ANCHOR).days / 365
        t_thu = (expires - JUDGED).days / 365
        travel = bs(True, s_thu, strike, t_thu, q["iv"]) - bs(True, spot, strike, t_now, q["iv"])
        total += sign * max(0.0, q["mid"] + travel)

    return max(0.0, total)


def debit_now(position):
    """What it costs to close the spread today: buy back the sold leg at the ask,
    sell the bought one at the bid. The middle is not a price you can trade."""
    return quotes[position["sold"]]["ask"] - quotes[position["bought"]]["bid"]


# --- is the model allowed to be trusted with the travel? ----------------------
#
# The check that this measurement could have come out differently. The model is
# used for a DIFFERENCE, so what matters is not that it hits the level but that
# its error is the same at both ends - and the way to see that is to price today
# with it and compare. An error of a few cents on a leg that CANCELS inside the
# spread is the good case; an error that does not cancel is a warning printed
# where the reader will see it.
print("the model against the market, today, before anything is moved:")
print(f"  {'spread':>12} {'market':>8} {'model':>8} {'error':>8}")
for p in POSITIONS:
    total_market = total_model = 0.0
    for side, sign in (("sold", 1), ("bought", -1)):
        symbol = p[side]
        strike, expires = occ(symbol)
        t_now = (expires - ANCHOR).days / 365
        total_market += sign * quotes[symbol]["mid"]
        total_model += sign * bs(True, spot, strike, t_now, quotes[symbol]["iv"])
    name = f"{occ(p['sold'])[0]:.0f}/{occ(p['bought'])[0]:.0f}"
    print(f"  {name:>12} {total_market:>8.3f} {total_model:>8.3f} {total_model - total_market:>+8.3f}")
print()

# One asymmetry that is real and must not be quietly enjoyed: closing pays the
# SPREAD - the sold leg at the ask, the bought one at the bid - while Thursday's
# number is a MARK, taken at the middle. Half the width of the market is
# therefore in favour of holding before any price has moved at all. That is not
# a trick of the measurement: the result is counted off the account's equity,
# and equity is marked. But a position marked at the middle and a position
# turned back into money are not the same thing, and only the second one is safe.
print("closing pays the market's spread; Thursday's number is a mark at the middle.")
print("Half the width therefore favours holding before any price has moved.\n")

print(f"{'book':>4} {'spread':>12} {'sets':>5} {'credit':>7} {'close now':>10} "
      f"{'hold: median':>13} {'mean':>8} {'95th':>8} {'worse than closing':>19}")
print("-" * 100)

per_book = {}
for p in POSITIONS:
    name = f"{occ(p['sold'])[0]:.0f}/{occ(p['bought'])[0]:.0f}"
    d = debit_now(p)
    for label, sample in (("all", moves), ("quiet", quiet)):
        marks = [mark_at(p, spot * m) for m in sample]
        worse = sum(1 for m in marks if m > d) / len(marks)
        med, mean = percentile(marks, 50), sum(marks) / len(marks)
        p95 = percentile(marks, 95)
        if label == "quiet":
            print(f"{p['book']:>4} {name:>12} {p['sets']:>5} {p['credit']:>7.2f} {d:>10.2f} "
                  f"{med:>13.2f} {mean:>8.2f} {p95:>8.2f} {worse:>18.0%}")
            money = [(p["credit"] - m) * 100 * p["sets"] for m in marks]
            certain = (p["credit"] - d) * 100 * p["sets"]
            per_book.setdefault(p["book"], []).append((certain, money))
            print(f"{'':>4} {'in dollars':>12} closing now locks {certain:>+9,.0f}; "
                  f"holding: median {percentile(money, 50):+,.0f}, mean {sum(money)/len(money):+,.0f}, "
                  f"5th {percentile(money, 5):+,.0f}, worst on record {min(money):+,.0f}")

# The third choice, and the one neither side of the question contains: close the
# spread that carries the tail and hold the rest. The tail of a book is not
# spread evenly across it - almost all of it sits in the leg nearest the money -
# so "hold or close" asked of the whole book is a coarser question than the
# position allows.
print("\nPer book, holding everything against closing everything (quiet markets only):")
for book, rows in sorted(per_book.items()):
    certain = sum(r[0] for r in rows)
    n = min(len(r[1]) for r in rows)
    together = [sum(r[1][i] for r in rows) for i in range(n)]
    worse = sum(1 for t in together if t < certain) / n
    print(f"  book {book}: closing now locks {certain:+,.0f}. Holding: median {percentile(together, 50):+,.0f}, "
          f"mean {sum(together)/n:+,.0f}, 5th {percentile(together, 5):+,.0f}, "
          f"worst on record {min(together):+,.0f}. Holding is worse than closing in {worse:.0%} of the sample.")
    if len(rows) > 1:
        # Close the nearest strike - the one whose worst case dominates - and hold
        # the others. The samples are PAIRED: the same historical move values every
        # spread in one row, so a mixture can be read off without re-running.
        nearest = max(range(len(rows)), key=lambda i: -min(rows[i][1]))
        mixed = [sum(rows[j][1][i] for j in range(len(rows)) if j != nearest) + rows[nearest][0]
                 for i in range(n)]
        worse = sum(1 for t in mixed if t < certain) / n
        print(f"           close the nearest spread and hold the rest: median {percentile(mixed, 50):+,.0f}, "
              f"mean {sum(mixed)/n:+,.0f}, 5th {percentile(mixed, 5):+,.0f}, worst on record {min(mixed):+,.0f}.")
