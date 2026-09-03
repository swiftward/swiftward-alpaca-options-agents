"""What it costs to cross the book. Not invented - taken from the live one.

Alpaca serves no historical option book (`/v1beta1/options/quotes` answers 404),
so the markup comes from the book that can be seen: a snapshot of the SPY, QQQ and
IWM chains on the expirations of the coming week, taken after the close on
27 August 2026. That is the last quote of the day and therefore the WIDEST of the
day rather than the narrowest, which is the direction a conservative markup wants
to be wrong in.

It computes what the product computes: cost = (ask-bid) of the sold leg plus
(ask-bid) of the bought leg. One strike wide, as most of the selected structures
are.

This is the source of the 0.070 median that `grid.py` names when it explains why
the assigned slippage is 0.10.
"""

import datetime as dt
import statistics
from collections import defaultdict

import alpaca

RANGE = 0.03  # the sold strike stands 0.3 to 3 per cent away from the price


def chain(underlying, spot, until):
    out, token = {}, None
    while True:
        params = {"limit": 1000, "expiration_date_lte": until.isoformat(),
                  "strike_price_gte": f"{spot * 0.95:.2f}",
                  "strike_price_lte": f"{spot * 1.05:.2f}"}
        if token:
            params["page_token"] = token
        answer = alpaca.get(f"{alpaca.DATA}/v1beta1/options/snapshots/{underlying}", params)
        out.update(answer.get("snapshots") or {})
        token = answer.get("next_page_token")
        if not token:
            break
    return out


def parse(symbol):
    """Reads an OCC symbol: SPY260903C00774000."""
    i = 0
    while not symbol[i].isdigit():
        i += 1
    root, rest = symbol[:i], symbol[i:]
    expiry = dt.date(2000 + int(rest[:2]), int(rest[2:4]), int(rest[4:6]))
    kind = "call" if rest[6] == "C" else "put"
    strike = int(rest[7:]) / 1000
    return root, expiry, kind, strike


def main():
    today = dt.date(2026, 8, 27)
    spots = {}
    for name in ("SPY", "QQQ", "IWM"):
        bar = alpaca.get(f"{alpaca.DATA}/v2/stocks/{name}/bars",
                         {"timeframe": "1Day", "start": "2026-08-26", "end": "2026-08-26",
                          "limit": 5, "feed": "sip"})
        spots[name] = (bar.get("bars") or [{}])[-1]["c"]

    print(f"{'name':6} {'day':>3} {'pairs':>5} {'median cost':>13} {'mean':>9} {'90%':>7} {'median leg width':>18}")
    everything = []
    for name, spot in spots.items():
        snaps = chain(name, spot, today + dt.timedelta(days=9))
        rows = []
        for symbol, body in snaps.items():
            quote = body.get("latestQuote") or {}
            bid, ask = quote.get("bp") or 0, quote.get("ap") or 0
            if bid <= 0 or ask <= 0:
                continue
            root, expiry, kind, strike = parse(symbol)
            rows.append((expiry, kind, strike, bid, ask))
        by = defaultdict(dict)
        for expiry, kind, strike, bid, ask in rows:
            by[(expiry, kind)][strike] = (bid, ask)
        for (expiry, kind), book in sorted(by.items()):
            days = (expiry - today).days
            if days < 0 or days > 9:
                continue
            costs, legs = [], []
            for strike, (bid, ask) in book.items():
                out = (spot - strike) / spot * 100 if kind == "put" else (strike - spot) / spot * 100
                if not (0.3 <= out <= 3.0):
                    continue
                other = strike - 1 if kind == "put" else strike + 1
                if other not in book:
                    continue
                obid, oask = book[other]
                costs.append((ask - bid) + (oask - obid))
                legs.append(ask - bid)
            if len(costs) < 4:
                continue
            costs.sort()
            print(f"{name:6} {days:3} {len(costs):5} {statistics.median(costs):13.3f} "
                  f"{statistics.mean(costs):9.3f} {costs[int(len(costs) * 0.9)]:7.3f} "
                  f"{statistics.median(legs):18.3f}")
            everything.extend(costs)

    if everything:
        everything.sort()
        print(f"\n{len(everything)} pairs in all: median cost {statistics.median(everything):.3f}, "
              f"mean {statistics.mean(everything):.3f}, "
              f"75% {everything[int(len(everything)*0.75)]:.3f}, "
              f"90% {everything[int(len(everything)*0.9)]:.3f}")


if __name__ == "__main__":
    main()
