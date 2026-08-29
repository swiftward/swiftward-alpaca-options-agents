"""The minute-by-minute path of every chosen trade: what buying the spread back
cost in each minute of its life.

Without this the take-profit share cannot be measured, only assigned. The outcome
at expiry is on hand; the path to it is not, and the whole of the rule "close once
this much of the credit has come back" lives in the path.

Only the legs of the trades the rule actually picks are pulled - 553 of them -
rather than the whole chain: the rest does not bear on the question and would cost
hours of downloading.
"""
import json
from pathlib import Path

import pandas as pd

import alpaca
from grid import pick, prepare

HERE = Path(__file__).resolve().parent
OUT = HERE / "data" / "paths.jsonl"


def occ(underlying: str, expiry: str, kind: str, strike: float) -> str:
    day = pd.Timestamp(expiry).strftime("%y%m%d")
    return f"{underlying}{day}{'C' if kind == 'call' else 'P'}{int(round(strike * 1000)):08d}"


raw = pd.read_parquet("data/candidates_bt.parquet")
kept = pick(prepare(raw), 0.30, 3.0).reset_index(drop=True)
print(f"trades: {len(kept)}")

kept["short_symbol"] = [occ(r.underlying, r.expiry, r.kind, r.short_strike) for r in kept.itertuples()]
kept["long_symbol"] = [occ(r.underlying, r.expiry, r.kind, r.long_strike) for r in kept.itertuples()]
kept.to_parquet("data/picked.parquet")

# Grouped by the day of entry: trades opened the same day share one time window.
done = set()
if OUT.exists():
    with OUT.open() as h:
        done = {json.loads(line)["day"] for line in h}
    print(f"days already collected: {len(done)}")

groups = kept.groupby("day")
with OUT.open("a") as out:
    for i, (day, group) in enumerate(groups, 1):
        if day in done:
            continue
        symbols = sorted(set(group.short_symbol) | set(group.long_symbol))
        start = pd.Timestamp(day).tz_localize("America/New_York").tz_convert("UTC")
        end = (pd.Timestamp(group.expiry.max()) + pd.Timedelta(days=1)).tz_localize("America/New_York").tz_convert("UTC")
        bars = alpaca.pages(
            f"{alpaca.DATA}/v1beta1/options/bars",
            {"symbols": ",".join(symbols), "timeframe": "1Min",
             "start": start.isoformat(), "end": end.isoformat(), "limit": 10000},
            "bars", most=60)
        merged: dict[str, list] = {}
        for chunk in bars:
            if isinstance(chunk, dict):
                for symbol, rows in chunk.items():
                    merged.setdefault(symbol, []).extend(rows)
        out.write(json.dumps({"day": str(day), "bars": merged}) + "\n")
        out.flush()
        if i % 25 == 0:
            print(f"  {i}/{len(groups)} days, symbols in the last: {len(merged)}", flush=True)

print("done")
