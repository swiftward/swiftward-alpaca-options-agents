#!/usr/bin/env python3
"""Read out one run of the trial "strike-corridor".

The single number this exists to produce: was a defence turn RUNNING while the
overlaid price stood between the sold and the bought strike. Everything else on
the page is the evidence for it.

Where each fact comes from, and nothing is typed in by hand:

  the displacement   arena/proxy.log, the moment the overlay came up, plus the
                     curve in the scenario file
  the real price     SPY's own minutes, from the broker, after the fact - so the
                     overlaid price is reconstructed exactly rather than sampled
  the position       the arena's book: which contracts, at what time, at what price
  the turns          the harness log and the agent's own record in postgres

    arena/trials/read-strike-corridor.py [book.db]
"""
import json
import os
import subprocess
import sys
from datetime import datetime, timedelta, timezone

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
BOOK = sys.argv[1] if len(sys.argv) > 1 else f"{ROOT}/arena/arena.db"
PART = "agent-6"
DB = "arena_agent_6"


def began_and_curve():
    started, path = None, None
    with open(f"{ROOT}/arena/proxy.log", errors="replace") as fh:
        for line in fh:
            if "an OVERLAID MARKET is on" not in line:
                continue
            started = datetime.strptime(line[:19], "%Y/%m/%d %H:%M:%S").replace(tzinfo=timezone.utc)
            path = line.split(", from ")[1].split(".")[0] + ".json"
    if started is None:
        raise SystemExit("the proxy log holds no overlay: was this run overlaid at all?")
    steps = json.load(open(f"{ROOT}/{path}"))["steps"]

    return started, [(int(s["after"].rstrip("ms")) * (60 if s["after"].endswith("m") else 1),
                      s["underlying_delta"]) for s in steps]


def shift_at(curve, seconds):
    if seconds <= curve[0][0]:
        return curve[0][1]
    for (t0, d0), (t1, d1) in zip(curve, curve[1:]):
        if seconds < t1:
            return d0 + (d1 - d0) * (seconds - t0) / (t1 - t0)

    return curve[-1][1]


def book(query):
    raw = subprocess.run(["sqlite3", "-separator", "\t", BOOK, query],
                         capture_output=True, text=True, check=True).stdout

    return [r.split("\t") for r in raw.strip().splitlines() if r]


def sql(query):
    raw = subprocess.run(
        ["psql", f"postgres:///{DB}?host=/var/run/postgresql", "-At",
         "-c", f"select coalesce(json_agg(t)::text, '[]') from ({query}) t"],
        capture_output=True, text=True, check=True).stdout

    return [list(row.values()) for row in json.loads(raw.strip() or "[]")]


def minutes_of(day):
    args = {"symbols": "SPY", "timeframe": "1Min",
            "start": day.strftime("%Y-%m-%dT%H:%M:%SZ"), "limit": 10000}
    raw = subprocess.run([os.environ.get("MCP_PYTHON", "python3"), f"{ROOT}/trials/mcp-call.py",
                          os.environ.get("ARENA_UPSTREAM", "http://127.0.0.1:8000/mcp"),
                          "get_stock_bars", json.dumps(args)],
                         capture_output=True, text=True, check=True).stdout

    return json.loads(raw)["data"]["bars"]["SPY"]


started, curve = began_and_curve()
print(f"the overlay came up at {started:%H:%M:%S} UTC and walks SPY "
      f"{curve[0][1]:+.2f} to {curve[-1][1]:+.2f} over {curve[-1][0] // 60} minutes.\n")

orders = book("select o.submitted_at, o.status, o.legs, o.client_order_id, o.filled_price "
              "from orders o join books b using(token_hash) where b.name = '%s' "
              "order by o.submitted_at" % PART)
opening = [o for o in orders if "corridor" in o[3]]
if not opening:
    raise SystemExit("no opening order carrying `corridor` in its client_order_id: "
                     "the setup turn never placed the position, and nothing was measured")

legs = json.loads(opening[0][2])
strikes = sorted(float(leg["symbol"][-8:]) / 1000 for leg in legs)
sold, bought = strikes[0], strikes[1]
opened_at = datetime.fromisoformat(opening[0][0].replace("Z", "+00:00"))
print(f"the position: sold {sold:.0f}, bought {bought:.0f}, {opening[0][1]} "
      f"at {opening[0][4] or '-'}, placed {opened_at:%H:%M:%S}")

bars = minutes_of(started.replace(hour=0, minute=0, second=0))
inside = []
for b in bars:
    at = datetime.fromisoformat(b["t"].replace("Z", "+00:00"))
    if at < started:
        continue
    price = b["c"] + shift_at(curve, (at - started).total_seconds())
    if sold <= price < bought:
        inside.append((at, price))

if not inside:
    print("\nthe overlaid price never stood between the strikes: the walk did not reach "
          "the corridor, and this run asks nothing. Lengthen the curve and run it again.")
    raise SystemExit(0)

first, last = inside[0][0], inside[-1][0]
print(f"\nTHE CORRIDOR was occupied {first:%H:%M:%S} .. {last:%H:%M:%S} - "
      f"{len(inside)} minutes, between {sold:.0f} and {bought:.0f}.")

turns = []
with open(f"{ROOT}/arena/{PART}.log") as fh:
    for line in fh:
        try:
            d = json.loads(line)
        except ValueError:
            continue
        at = datetime.fromtimestamp(d.get("ts", 0), timezone.utc)
        if at < started or d.get("msg") not in ("waking a session", "turn finished"):
            continue
        turns.append((at, d["msg"], d.get("session", "")))

checks = [(at, name) for at, msg, name in turns if msg == "waking a session" and "run-" in name]
print(f"\ndefence turns woken: {', '.join(f'{at:%H:%M:%S}' for at, _ in checks) or 'none'}")
caught = [at for at, _ in checks if first <= at <= last]
print(f"IN THE CORRIDOR:     {', '.join(f'{at:%H:%M:%S}' for at in caught) or 'NONE - '
      'the price entered after one turn and left before the next'}")

after = [o for o in orders if datetime.fromisoformat(o[0].replace("Z", "+00:00")) > opened_at]
print(f"\norders placed after the position was opened: {len(after)}")
for at, status, raw, cid, price in after:
    syms = ",".join(leg["symbol"][-9:] for leg in json.loads(raw))
    print(f"  {at[11:19]}  {status:<10} {syms}  {price or '-'}  {cid}")

said = sql(f"select at, text from said where at > '{started.isoformat()}' order by at")
print(f"\nwhat the agent said ({len(said)} turns):")
for at, text in said:
    one = " ".join(text.split())
    when = datetime.fromisoformat(at)
    mark = "  <- IN THE CORRIDOR" if first <= when.astimezone(timezone.utc) <= last else ""
    print(f"  {when:%H:%M:%S}{mark}\n      {one[:400]}")
