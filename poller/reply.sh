#!/bin/sh
# Say something back into a turn, and end it.
#
# The mailbox is one direction only in each request: the harness parks turns and
# the agent takes them; what the agent says comes back here. Everything posted
# names the turn it belongs to, because the room and the record file it under
# the cause that woke the session, and a reply with no turn has no cause.
#
# Usage:
#   reply.sh <mailbox-url> say  <turn-id> <text>
#   reply.sh <mailbox-url> done <turn-id> [failure]
#
# Say as often as there is something worth saying - each one appears in the room
# as the session speaking. Say done exactly once, when the turn is over. A turn
# that is never ended is given up on by the harness after its own limit, and
# until then the next window is refused because one session is already running.
set -eu

usage() {
	echo "usage: $(basename "$0") [mailbox-url] say|done <turn-id> [text]   (url may come from MAILBOX)" >&2
	exit 64
}

# Given, or already in the environment as MAILBOX. What tells the two forms
# apart is the first word: an address begins with http, and an action is `say`
# or `done`.
case "${1:-}" in
	http://*|https://*) url=$1; shift ;;
	*) url=${MAILBOX:-} ;;
esac
[ -n "$url" ] || { echo "no mailbox: pass the url or set MAILBOX" >&2; usage; }

[ $# -ge 2 ] || usage
action=$1
turn=$2
text=${3:-}

case "$action" in
	say)
		[ -n "$text" ] || { echo "say needs something to say" >&2; usage; }
		body=$(TURN=$turn TEXT=$text python3 -c 'import json,os; print(json.dumps({"turn":os.environ["TURN"],"text":os.environ["TEXT"]}))')
		;;
	done)
		body=$(TURN=$turn FAILURE=$text python3 -c 'import json,os; out={"turn":os.environ["TURN"]}; f=os.environ.get("FAILURE",""); out.update({"failure":f} if f else {}); print(json.dumps(out))')
		;;
	*) usage ;;
esac

code=$(printf '%s' "$body" | curl --silent --show-error --max-time 30 \
	--output /dev/null --write-out '%{http_code}' \
	--header 'Content-Type: application/json' \
	--data-binary @- \
	"$url/$action") || { echo "the mailbox could not be reached" >&2; exit 69; }

case "$code" in
	202) exit 0 ;;
	404) echo "no mailbox at that url" >&2; exit 4 ;;
	*) echo "the mailbox answered $code" >&2; exit 69 ;;
esac
