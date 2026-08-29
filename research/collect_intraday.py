"""Full-session minute bars for the legs of every candidate, on a sample of days.

The question this exists for: the declaration lets the agent open only inside
three windows - 10:20, 12:30 and 14:20 New York, forty-odd minutes each, about two
hours of a six-and-a-half hour session. Whether that is where the good entries are,
or merely where we happened to look, is a measurement nobody has made.

It could not be made from what was collected before. `candidates_bt.parquet` holds
one row per candidate per DAY, priced from the 13:50-14:40 window, because the
whole set was built around a 14:20 entry. It can answer what a 14:20 entry gives
and nothing else.

A SAMPLE of days rather than all of them: one day costs about eight seconds and
four megabytes, so the full period would be an hour and a half and some three
gigabytes. A hundred and twenty days spread evenly across the period is enough to
see whether the hour of the day moves the edge at all; if it does, the sample can
be deepened where it matters.
"""

import json
import sys
from pathlib import Path

import pandas as pd

import alpaca

HERE = Path(__file__).resolve().parent
OUT = HERE / "data" / "intraday.jsonl"
DAYS = int(sys.argv[1]) if len(sys.argv) > 1 else 120


def occ(underlying: str, expiry, kind: str, strike: float) -> str:
    day = pd.Timestamp(expiry).strftime("%y%m%d")
    return f"{underlying}{day}{'C' if kind == 'call' else 'P'}{int(round(strike * 1000)):08d}"


def main() -> None:
    frame = pd.read_parquet(HERE / "data" / "candidates_bt.parquet")
    every = sorted(frame.day.astype(str).unique())
    # Spread evenly rather than taking the newest: the volatility regime moves
    # over the period, and a sample from one end would measure that instead.
    step = max(1, len(every) // DAYS)
    chosen = every[::step][:DAYS]
    print(f"days available {len(every)}, sampled {len(chosen)}")

    done = set()
    if OUT.exists():
        with OUT.open() as handle:
            done = {json.loads(line)["day"] for line in handle}
        print(f"already collected {len(done)}")

    OUT.parent.mkdir(exist_ok=True)
    with OUT.open("a") as out:
        for number, day in enumerate(chosen, 1):
            if day in done:
                continue
            rows = frame[frame.day.astype(str) == day]
            symbols = sorted(
                {occ(r.underlying, r.expiry, r.kind, r.short_strike) for r in rows.itertuples()}
                | {occ(r.underlying, r.expiry, r.kind, r.long_strike) for r in rows.itertuples()}
            )
            start = pd.Timestamp(day + " 09:30").tz_localize("America/New_York").tz_convert("UTC")
            end = pd.Timestamp(day + " 16:00").tz_localize("America/New_York").tz_convert("UTC")

            merged: dict[str, list] = {}
            # In batches: the answer for two hundred contracts across a session is
            # already several megabytes, and one refusal should not cost the day.
            for at in range(0, len(symbols), 150):
                chunk = symbols[at:at + 150]
                for page in alpaca.pages(
                    f"{alpaca.DATA}/v1beta1/options/bars",
                    {"symbols": ",".join(chunk), "timeframe": "1Min",
                     "start": start.isoformat(), "end": end.isoformat(), "limit": 10000},
                    "bars", most=40,
                ):
                    if isinstance(page, dict):
                        for symbol, bars in page.items():
                            merged.setdefault(symbol, []).extend(bars)

            out.write(json.dumps({"day": day, "bars": merged}) + "\n")
            out.flush()
            if number % 10 == 0:
                print(f"  {number}/{len(chosen)} days, contracts in the last {len(merged)}", flush=True)

    print("done")


if __name__ == "__main__":
    main()
