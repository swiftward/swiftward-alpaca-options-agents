#!/usr/bin/env python3
"""Read out one run of the trial "the important on top of the unimportant".

Everything here comes from records the stand keeps anyway: the harness log, the
agent's own record in postgres, and the arena's book. Nothing is typed in by
hand except which run to read.

    arena/trials/measure-interrupt-priority.py [book.db]

Take the book from an archive rather than the live one by naming it. Archive it
with `run-proxy.sh stop`, never with `cp`: the store runs in WAL mode and the
.db file between runs is a stub.

The one conversion worth naming: an order in the book is stamped with the
BROKER's clock, and under a scenario that clock is the staged one. It is turned
back into wall time by the two anchors the proxy prints at startup - when the
stage began, and what its clock said then.
"""
import json
import os
import subprocess
import sys
from datetime import datetime, timezone, timedelta

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PARTICIPANTS = [("agent-3", "claude-agent-3", "Claude Code / Opus 5"),
                ("agent-4", "agy-agent-4", "Antigravity / Gemini 3.1 Pro"),
                ("agent-5", "codex-agent-5", "codex / gpt-5.6-sol, mailbox + Stop hook"),
                ("agent-6", "codex-agent-6", "codex / gpt-5.6-sol, THE PRODUCTION PATH")]
# The stage: when it began in wall time, and what its own clock said then. Both
# are taken from the proxy's own log and the scenario file rather than typed in.
# Typed in, they are wrong on the second run of the day and quietly turn every
# order's timestamp into a plausible lie - which is exactly what happened the
# first time this was read out.
def _stage():
    began, path = None, None
    with open(f"{ROOT}/arena/proxy.log", errors="replace") as fh:
        for line in fh:
            if "a STAGED MARKET is on" not in line:
                continue
            stamp = line[:19]
            path = line.split(" from ")[1].split(".")[0] + ".json"
            began = datetime.strptime(stamp, "%Y/%m/%d %H:%M:%S").replace(tzinfo=timezone.utc)
    if began is None:
        raise SystemExit("the proxy log has no staged market: was this run staged at all?")
    scenario = json.load(open(f"{ROOT}/{path}"))
    # An anchored scenario has no start of its own: its clock IS the wall clock,
    # so the conversion below becomes the identity.
    start = began if scenario.get("anchor") == "now" else \
        datetime.fromisoformat(scenario["start"].replace("Z", "+00:00"))

    return began, start, float(scenario.get("speed") or 1)


STAGE_BEGAN, STAGE_START, SPEED = _stage()
# How long after the event still counts as a reaction to it. Past that, whatever
# the session is doing belongs to the next window.
WINDOW = 20

def wall(staged_iso):
    s = staged_iso.replace("Z", "+00:00")
    if "." in s:
        head, rest = s.split(".", 1)
        frac, tz = rest[:6], rest[len(rest) - 6:]
        s = f"{head}.{frac}{tz}"
    staged = datetime.fromisoformat(s)
    return STAGE_BEGAN + (staged - STAGE_START) / SPEED


def log_lines(name):
    out = []
    with open(f"{ROOT}/arena/{name}.log") as fh:
        for line in fh:
            try:
                d = json.loads(line)
            except ValueError:
                continue
            if d.get("ts", 0) < STAGE_BEGAN.timestamp():
                continue
            d["when"] = datetime.fromtimestamp(d["ts"], timezone.utc)
            out.append(d)
    return out


def sql(db, query):
    # Through json, not through a separator. A line of `said` is a paragraph
    # with newlines in it, and splitting the answer by rows put half of one
    # reply into the next row and then failed to unpack it.
    raw = subprocess.run(
        ["psql", f"postgres:///arena_{db.replace('-', '_')}?host=/var/run/postgresql",
         "-At", "-c", f"select coalesce(json_agg(t)::text, '[]') from ({query}) t"],
        capture_output=True, text=True, check=True).stdout
    return [list(row.values()) for row in json.loads(raw.strip() or "[]")]


BOOK = sys.argv[1] if len(sys.argv) > 1 else f"{ROOT}/arena/arena.db"


def book(query):
    raw = subprocess.run(["sqlite3", "-separator", "\t", BOOK, query],
                         capture_output=True, text=True, check=True).stdout
    return [r.split("\t") for r in raw.strip().splitlines() if r]


def gap(a, b):
    return "-" if not (a and b) else f"{(b - a).total_seconds():+.1f}s"


for name, home, who in PARTICIPANTS:
    print(f"\n=== {name}  {who} " + "=" * 30)
    lines = log_lines(name)

    # By prefix, not by the exact name. A repeat on the same day has to rename
    # the window - the harness restores "already ran today" from the record and
    # an `at:` window fires once a day - so the second run is research-b.
    research = next((d["when"] for d in lines
                     if d["msg"] == "waking a session"
                     and str(d.get("session", "")).startswith("research")), None)

    woke = next((d for d in lines if d["msg"] == "waking the session for its own reason"), None)
    steered = next((d for d in lines if d["msg"] == "said into the running turn"), None)
    missed = next((d for d in lines if d["msg"].startswith("could not reach the running turn")), None)

    t_important = steered["when"] if steered else (woke["when"] if woke else None)
    delivery = ("into the running turn" if steered else
                "as a turn of its own - the first turn was NOT running" if missed else
                "not delivered")

    finished = next((d["when"] for d in lines
                     if d["msg"] == "turn finished" and research and d["when"] > research), None)
    # The parked moment is not the seen moment. A mailbox is a queue: the steer
    # is delivered when the agent next polls, and a harness whose poller only
    # runs BETWEEN turns cannot poll until the turn it is in has been closed.
    # So the earliest the message could have been read is the later of the two.
    # Filled in below, once it is known whether anything happened before the
    # turn closed: a reaction inside the turn PROVES the message was read
    # inside it, and nothing else does.
    seen = t_important

    if t_important:
        # Bounded at both ends. Without an upper bound the reading picks up
        # every ordinary turn that ran afterwards, and the run reads as if the
        # session had gone on talking about the event for hours.
        cut = t_important.isoformat()
        until = (t_important + timedelta(minutes=WINDOW)).isoformat()
        said = sql(name, f"select at, text from said "
                         f"where at > '{cut}' and at < '{until}' order by at limit 6")
        intents = sql(name, f"select recorded_at, structure, thesis from intents "
                            f"where recorded_at > '{cut}' and recorded_at < '{until}' "
                            f"order by recorded_at limit 4")
    else:
        said, intents = [], []

    orders = [r for r in book(
        "select o.submitted_at, o.status, o.legs, o.turn_ref from orders o "
        "join books b using(token_hash) where b.name = '%s' order by o.submitted_at" % name)
        if "QQQ" in r[2]]
    after = [r for r in orders if t_important
             and t_important < wall(r[0]) < t_important + timedelta(minutes=WINDOW)]

    first_order = wall(after[0][0]) if after else None
    first_said = datetime.fromisoformat(said[0][0]) if said else None
    first_intent = datetime.fromisoformat(intents[0][0]) if intents else None

    inside = [t for t in (first_intent, first_order)
              if t and finished and t < finished]
    if not inside and finished and t_important and finished > t_important:
        seen = finished
    plumbing = (seen - t_important).total_seconds() if (seen and t_important) else None

    # THE number. The market does not care which harness was holding the
    # message: what happened to the position is decided by how long it took
    # from the event to an order. Everything under the rule is the breakdown of
    # that one interval, and none of it may be quoted instead of it.
    print(f"  the event                    {t_important and t_important.strftime('%H:%M:%S')}")
    print(f"  AN ACTION ON THE MARKET      {first_order and first_order.strftime('%H:%M:%S') or 'never'}"
          f"   {gap(t_important, first_order) if first_order else 'no order at all'}")
    print("  " + "-" * 60)
    print(f"    intent recorded            {first_intent and first_intent.strftime('%H:%M:%S')}"
          f"   {gap(t_important, first_intent)}")
    print(f"    first word about it        {first_said and first_said.strftime('%H:%M:%S')}"
          f"   {gap(t_important, first_said)}")
    print(f"    the unimportant turn ran   {research and research.strftime('%H:%M:%S')}"
          f" .. {finished and finished.strftime('%H:%M:%S')}")
    print(f"    of the interval, plumbing  "
          f"{('%.1fs' % plumbing) if plumbing is not None else '-'}"
          f"   ({'read MID-TURN' if seen == t_important else 'unreadable until the turn was closed'})")

    path = f"{ROOT}/{home}/research.md"
    if os.path.exists(path):
        wrote = datetime.fromtimestamp(os.path.getmtime(path), timezone.utc)
        side = ("no action to compare with" if not first_order else
                "before the action" if wrote < first_order else "after the action")
        print(f"    research.md written        {wrote.strftime('%H:%M:%S')}   {side}"
              f"  ({os.path.getsize(path)} bytes)")
    else:
        print("    research.md                never written")

    for at, text in said:
        one = " ".join(text.split())
        print(f"    said {datetime.fromisoformat(at).strftime('%H:%M:%S')}  {one[:200]}")
    for at, structure, thesis in intents:
        print(f"    intent {datetime.fromisoformat(at).strftime('%H:%M:%S')}  "
              f"{' '.join(structure.split())[:70]} | {' '.join(thesis.split())[:100]}")
