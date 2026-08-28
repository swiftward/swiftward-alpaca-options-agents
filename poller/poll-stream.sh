#!/bin/sh
# Print everything the harness wants this agent to do, one JSON object per line,
# forever.
#
# This is the STREAMING shape. It never exits on its own: it holds a poll open,
# prints whatever comes back, and holds another. A harness that watches a
# long-running command and wakes on each line of its output runs this one.
#
# Keep the two shapes apart. This script must never be given to something that
# waits for the process to exit - it does not - and poll-once.sh must never be
# given to something that reads lines, because it says one thing and leaves.
#
# Everything that is not an event goes to stderr, on purpose. A watcher usually
# treats stdout as the event and stderr as noise, so a lost connection or a
# retry must not look like a turn to take.
#
# Usage: poll-stream.sh <mailbox-url> [seconds]
set -eu

usage() {
	echo "usage: $(basename "$0") <mailbox-url> [seconds]" >&2
	exit 64
}

[ $# -ge 1 ] || usage
url=$1
wait_for=${2:-90}

case "$url" in
	http://*|https://*) ;;
	*) echo "the mailbox url must start with http:// or https://" >&2; usage ;;
esac
case "$wait_for" in
	''|*[!0-9]*) echo "seconds must be a whole number" >&2; usage ;;
esac

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

# backoff grows while the mailbox is unreachable and resets the moment it
# answers. Without it a harness that is down for an hour is asked about a
# thousand times, and the log that would have shown why is buried.
backoff=1

while :; do
	set +e
	event=$("$here/poll-once.sh" "$url" "$wait_for" 2>/dev/null)
	status=$?
	set -e

	case "$status" in
		0)
			backoff=1
			printf '%s\n' "$event"
			;;
		3)
			# The hold expired with nothing to say. This is the normal state of a
			# quiet market and costs nothing; say nothing about it.
			backoff=1
			;;
		4)
			echo "no mailbox at that url - wrong token, or the harness serves none" >&2
			exit 4
			;;
		5)
			echo "that mailbox is gone" >&2
			exit 5
			;;
		64)
			exit 64
			;;
		*)
			echo "the mailbox could not be reached; trying again in ${backoff}s" >&2
			sleep "$backoff"
			backoff=$((backoff * 2))
			[ "$backoff" -gt 60 ] && backoff=60
			;;
	esac
done
