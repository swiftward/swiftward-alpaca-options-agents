#!/bin/sh
# Wait for one thing the harness wants this agent to do, print it, and exit.
#
# This is the WAKE-ON-EXIT shape. It runs once, says at most one thing on
# stdout, and its exit status is the whole answer:
#
#   0   something happened; the one JSON object is on stdout
#   3   the hold expired with nothing to say; run it again
#   4   no mailbox at that URL - wrong token, or the harness is not serving one
#   5   the mailbox is gone
#   64  called wrong
#   69  the mailbox could not be reached
#
# It is meant for a harness that can run a command and wake when it exits -
# which is most of them. Do not use it where a stream is wanted: mixing the two
# shapes is how a poller ends up printing an event nobody is reading, and the
# event is then simply lost. For a stream use poll-stream.sh, which never exits.
#
# Usage: poll-once.sh <mailbox-url> [seconds]
#
#   poll-once.sh https://host:8090/mailbox/TOKEN 90
#
# The token is in the URL and nowhere else: it names WHICH agent's turns these
# are, so one URL is one identity. Keep it out of shell history and out of the
# process list of a shared machine - pass it in a file or an environment
# variable if that matters where you are running.
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

body=$(mktemp)
trap 'rm -f "$body"' EXIT INT TERM

# --max-time is the hold plus a margin: the mailbox answers by itself when the
# hold ends, so curl's own clock is there for the case where nothing answers at
# all, not to end the wait.
code=$(curl --silent --show-error \
	--max-time "$((wait_for + 15))" \
	--output "$body" \
	--write-out '%{http_code}' \
	"$url/poll?wait=$wait_for" 2>/dev/null) || {
	echo "the mailbox could not be reached" >&2
	exit 69
}

case "$code" in
	200)
		cat "$body"
		# Some servers do not end the object with a newline; a reader waiting for
		# a line would then wait forever.
		case "$(tail -c 1 "$body")" in "") ;; *) echo ;; esac
		exit 0
		;;
	204) exit 3 ;;
	404) echo "no mailbox at that url" >&2; exit 4 ;;
	410) echo "that mailbox is gone" >&2; exit 5 ;;
	*)
		echo "the mailbox answered $code" >&2
		head -c 400 "$body" >&2 2>/dev/null || true
		exit 69
		;;
esac
