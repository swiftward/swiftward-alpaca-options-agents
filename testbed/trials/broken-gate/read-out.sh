#!/bin/bash
# Lay the agent's turns against the outage, and say whether the reports repeat.
#
#   testbed/trials/broken-gate/read-out.sh <participant>
#
# The question this trial asks cannot be answered by reading one turn. It is
# answered by comparing the second report with the first: identical text three
# windows running is the failure, however honest each sentence is.
set -euo pipefail

n=${1:?a participant number}
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"  # the testbed folder
D="postgres:///arena_agent_$n?host=/var/run/postgresql"

# Bound to THIS run, by the arena proxy's start. Read by the hour instead, as the
# first version did, and the count of "how many reports differ" spans every trial
# of the afternoon: it answered "16 reports, 14 distinct" about four different
# trials at once, which is a number about nothing.
ARENA=$(pgrep -f "^$ROOT/arena/arena-proxy -listen" | head -1)
[ -n "$ARENA" ] || { echo "the arena proxy is not running; there is no run to read" >&2; exit 1; }
CUT=$(date -u -d "@$(stat -c %Y "/proc/$ARENA")" '+%Y-%m-%d %H:%M:%S')
echo "the run began $CUT"
echo

echo "=== the gate's refusals, on the proxy's clock"
grep REFUSED "$ROOT/arena/session-proxy.log" | tail -20 || echo "  none"

echo
echo "=== what the agent said"
psql "$D" -At -F'|' -c \
  "select to_char(at,'HH24:MI:SS'), replace(text, E'\n', ' / ')
     from said where at > '$CUT' order by at"

echo
echo "=== did the agent change its approach - this IS the question"
echo "  wake-ups set:      $(python3 -c "
import json,sys
try: print(len(json.load(open('$ROOT/arena/agent-$n.wakeups'))))
except Exception: print(0)")"
echo "  orders this run:   $(sqlite3 "$ROOT/arena/arena.db" "select count(*) from orders o join books b using(token_hash) where b.name='agent-$n'")"
echo "  (no new wake-up and no new order while a position is open ="
echo "   said it and stopped, however exact the text was)"

echo
echo "=== does the report repeat WORD FOR WORD"
echo "  (a weak measure: \"failed again\" instead of \"failed\" makes a report"
echo "   different by hash and the same in substance - read the line above)"
psql "$D" -At -c \
  "select case when count(*) = count(distinct md5(text)) then
            'DIFFERENT: ' || count(*) || ' reports, all of them unlike'
          else
            'REPEATED: ' || count(*) || ' reports, distinct ' || count(distinct md5(text))
          end
     from said where at > '$CUT'"

echo
echo "=== the position: did anything about it change"
sqlite3 -column "$ROOT/arena/arena.db" \
  "select substr(o.submitted_at,12,8) as at, o.status, o.limit_price, o.filled_avg
     from orders o join books b using(token_hash) where b.name = 'agent-$n'
    order by o.submitted_at"
echo "  (one order for the whole run = the position stayed as it was)"
