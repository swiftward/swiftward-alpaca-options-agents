#!/usr/bin/env python3
"""Can a defence that fires only BETWEEN the strikes ever see the corridor?

The team's defence rule, from agent/alpaca-agent-1.yaml, is deliberate and
well-argued: close a spread only while the underlying stands between the sold
and the bought strike, because beyond the bought leg the loss is already the
full width and closing pays the crossing for nothing.

The rule is sound. What this measures is something the rule cannot state about
itself: whether a session that looks every fifteen minutes is ever LOOKING while
the price is in there. The corridor of a one-dollar SPY spread is one dollar
wide. A price walking through it does not wait.

So, from SPY's own minutes this year: every time the price went up through a
whole-dollar level and then through the next one, was there a scheduled check in
between? A crossing with no check inside it is a defence that could not have
fired - not because it decided to hold, but because nobody asked it while the
answer would have been yes.

    arena/trials/defence-corridor.py <minutes.json> [width]

Nothing here is about the model or the prompt. It is arithmetic on the schedule
and on the price, and it would read the same for any agent behind that rule.
"""
import json
import sys
from collections import Counter
from datetime import datetime, timedelta, timezone

# The regular session, in UTC, while New York is on daylight time.
OPEN, CLOSE = "13:30", "20:00"
# The defence's own schedule: from 09:40 New York, every fifteen minutes, to
# 15:55. Taken from the declaration, not chosen here.
FIRST_CHECK, EVERY, LAST_CHECK = "13:40", 15, "19:55"

path = sys.argv[1]
width = float(sys.argv[2]) if len(sys.argv) > 2 else 1.0

bars = json.load(open(path))
days = {}
for b in bars:
    at = datetime.fromisoformat(b["t"].replace("Z", "+00:00"))
    hm = at.strftime("%H:%M")
    if hm < OPEN or hm >= CLOSE:
        continue
    days.setdefault(at.date(), []).append((at, b["l"], b["h"], b["c"]))


def checks(day):
    """The minutes at which the defence session is scheduled to look."""
    out, at = [], datetime.combine(day, datetime.strptime(FIRST_CHECK, "%H:%M").time(),
                                   tzinfo=timezone.utc)
    last = datetime.combine(day, datetime.strptime(LAST_CHECK, "%H:%M").time(), tzinfo=timezone.utc)
    while at <= last:
        out.append(at)
        at += timedelta(minutes=EVERY)

    return out


seen = Counter()
occupancies = []
missed_examples = []
for day, minutes in sorted(days.items()):
    if len(minutes) < 300:
        continue
    grid = {at: close for at, _, _, close in minutes}
    scheduled = [at for at in checks(day) if at in grid]
    low = min(l for _, l, _, _ in minutes)
    high = max(h for _, _, h, _ in minutes)

    for sold in range(int(low), int(high) + 1):
        bought = sold + width
        # An upward traverse: the session begins below the sold strike, and the
        # price later stands above the bought one. That is the case the rule was
        # written for and the only one in which it has anything to do.
        if minutes[0][3] >= sold or high < bought:
            continue
        # Entering and leaving are counted on the CLOSE of the minute, not on its
        # high. What an observer reads is the last trade, and a wick that pokes a
        # cent into the corridor and comes straight back out is not a minute in
        # which anyone could have seen the price there. Counting on the high made
        # the corridor look occupied for hours at a stretch while every check
        # inside it read a price below the strike - the measurement then blamed
        # the schedule for something the schedule did nothing about.
        entered = next((at for at, _, _, c in minutes if c >= sold), None)
        if entered is None:
            continue
        left = next((at for at, _, _, c in minutes if at > entered and c >= bought), None)
        if left is None:
            continue

        # How long the price was actually STANDING in the corridor, in minutes.
        # This, and not the span from entry to exit, is the window in which the
        # rule has anything to say: a price that dipped back below the sold strike
        # for an hour was not a position whose loss was growing.
        occupancy = [at for at, _, _, c in minutes if entered <= at < left and sold <= c < bought]
        inside = [at for at in scheduled if entered <= at < left and sold <= grid[at] < bought]
        seen["traverses"] += 1
        occupancies.append(len(occupancy))
        if inside:
            seen["caught"] += 1
        else:
            seen["missed"] += 1
            if len(missed_examples) < 6:
                missed_examples.append((day, sold, len(occupancy)))

print(f"SPY minutes from {bars[0]['t'][:10]} to {bars[-1]['t'][:10]}, "
      f"{len(days)} sessions, corridor {width:g} dollars wide, a check every {EVERY} minutes.\n")
print(f"  upward traverses of a corridor   {seen['traverses']}")
print(f"  a scheduled check fell inside    {seen['caught']}  ({seen['caught']/max(1,seen['traverses']):.0%})")
print(f"  none did                         {seen['missed']}  ({seen['missed']/max(1,seen['traverses']):.0%})")
ordered = sorted(occupancies)
if ordered:
    median = ordered[len(ordered) // 2]
    print(f"\n  minutes the price actually stood inside the corridor: median {median}, "
          f"quarter of them {ordered[len(ordered) // 4]} or fewer")
    print(f"  the checks are {EVERY} minutes apart")

print("\nthe rule could not have fired on these, whatever the agent decided:")
for day, sold, held in missed_examples:
    print(f"  {day}  through {sold:.0f}/{sold + width:.0f}, the price stood inside for {held} minutes")
