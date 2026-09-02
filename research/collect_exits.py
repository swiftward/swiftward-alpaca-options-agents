"""The three files `exit_rules.py` reads, and nothing else writes.

`candidates_bt.parquet` is committed, so `grid.py` and `make claims` run on a fresh
clone. `exit_rules.py` does not: it needs the underlying's path through the day, and
the long leg's price at entry, and neither is in the parquet. Without this script the
measurement that decides whether defence pays cannot be repeated by anyone, including
us - which is the one measurement that must be.

What it writes, per underlying, into `data/`:

  stock_<sym>_15m.json   15-minute bars of the underlying, entry day to last expiry.
                         The path the defence rules are walked along.
  contracts_<sym>.json   OCC symbol -> expiry, type, strike, for the picked legs only.
                         Built from the parquet, not downloaded: we know every leg we
                         picked, and the option chain endpoint drops expired contracts.
  obars_<sym>.jsonl      one line per entry day, holding each leg's bar in the entry
                         window. Only the long leg's price is read, to recover its
                         implied volatility; the short leg's is already in the parquet.

Read-only against Alpaca, and resumable: a day already in `obars_*.jsonl` is skipped,
so an interrupted run continues where it stopped.
"""

import json
from pathlib import Path

import pandas as pd

import alpaca
import exit_rules
import grid

HERE = Path(__file__).resolve().parent
DATA = HERE / "data"
NY = "America/New_York"

# The window the parquet itself was priced from, so the long leg's price comes from
# the same minutes as the short leg's.
WINDOW = ("13:50", "14:40")


def occ(underlying: str, expiry, kind: str, strike: float) -> str:
    day = pd.Timestamp(expiry).strftime("%y%m%d")
    return f"{underlying}{day}{'C' if kind == 'call' else 'P'}{int(round(strike * 1000)):08d}"


def stock_bars(symbol: str, first, last) -> list:
    start = pd.Timestamp(first).tz_localize(NY).tz_convert("UTC")
    end = (pd.Timestamp(last) + pd.Timedelta(days=1)).tz_localize(NY).tz_convert("UTC")
    return alpaca.pages(
        f"{alpaca.DATA}/v2/stocks/{symbol}/bars",
        {"timeframe": "15Min", "start": start.isoformat(), "end": end.isoformat(),
         "adjustment": "raw", "limit": 10000},
        "bars", most=400)


def option_bars(symbols: list[str], day) -> list:
    start = pd.Timestamp(f"{day} {WINDOW[0]}").tz_localize(NY).tz_convert("UTC")
    end = pd.Timestamp(f"{day} {WINDOW[1]}").tz_localize(NY).tz_convert("UTC")
    pages = alpaca.pages(
        f"{alpaca.DATA}/v1beta1/options/bars",
        {"symbols": ",".join(symbols), "timeframe": "1Min",
         "start": start.isoformat(), "end": end.isoformat(), "limit": 10000},
        "bars", most=20)
    # The endpoint answers {symbol: [bars]}; the reader wants a flat list carrying
    # the symbol on each bar, and the LAST bar of the window is the entry price.
    last: dict[str, dict] = {}
    for chunk in pages:
        if isinstance(chunk, dict):
            for symbol, rows in chunk.items():
                if rows:
                    last[symbol] = rows[-1]
    return [dict(bar, s=symbol) for symbol, bar in last.items()]


def main() -> None:
    DATA.mkdir(exist_ok=True)
    frame = pd.read_parquet(DATA / "candidates_bt.parquet")
    picked = grid.pick(grid.prepare(frame), exit_rules.DELTA_CAP, exit_rules.EDGE_MIN)
    picked = picked.sort_values(["day", "underlying"]).reset_index(drop=True)
    print(f"trades {len(picked)}, underlyings {sorted(picked['underlying'].unique())}")

    for symbol, group in picked.groupby("underlying"):
        path = DATA / f"stock_{symbol}_15m.json"
        if path.exists():
            print(f"{symbol}: bars already on disk")
        else:
            bars = stock_bars(symbol, group["day"].min(), group["expiry"].max())
            path.write_text(json.dumps(bars))
            print(f"{symbol}: {len(bars)} 15-minute bars", flush=True)

        legs = []
        for r in group.itertuples():
            for strike in (r.short_strike, r.long_strike):
                legs.append({"symbol": occ(r.underlying, r.expiry, r.kind, strike),
                             "expiration": pd.Timestamp(r.expiry).date().isoformat(),
                             "type": r.kind, "strike": float(strike)})
        unique = {leg["symbol"]: leg for leg in legs}
        (DATA / f"contracts_{symbol}.json").write_text(json.dumps(list(unique.values())))
        print(f"{symbol}: {len(unique)} contracts")

        out = DATA / f"obars_{symbol}.jsonl"
        done = set()
        if out.exists():
            with out.open() as h:
                done = {json.loads(line)["day"] for line in h}
        days = list(group.groupby("day"))
        with out.open("a") as h:
            for i, (day, rows) in enumerate(days, 1):
                if str(day) in done:
                    continue
                symbols = sorted({occ(r.underlying, r.expiry, r.kind, s)
                                  for r in rows.itertuples()
                                  for s in (r.short_strike, r.long_strike)})
                h.write(json.dumps({"day": str(day), "bars": option_bars(symbols, day)}) + "\n")
                h.flush()
                if i % 25 == 0:
                    print(f"  {symbol} {i}/{len(days)} days", flush=True)
        print(f"{symbol}: option bars done", flush=True)


if __name__ == "__main__":
    main()
