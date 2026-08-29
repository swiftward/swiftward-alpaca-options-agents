# The mailbox: driving this harness from an agent it does not start

The harness holds the clock, the wake-ups, the room and the record, and hands all
of it to one thing it starts itself — a codex process on the other end of a pipe.
That pipe is the only part of the program that names a vendor, and it is also the
part that decides which agents may ever run here: a session a person is sitting
in cannot be started as our child process, and so cannot be woken by this clock
at all.

A **mailbox** is the same conversation with the pipe taken out. A turn is not
written to a process; it is parked, and whoever holds the token comes and takes
it. What the agent says comes back the same way, by a request.

Nothing else changes. The schedule still decides when, the wake-ups still fire,
the room still shows the work, the record still says what happened and why. That
sameness is the whole point: two agents driven differently are still comparable,
because only the driver differs.

```
AGENT_DRIVER=mailbox
MAILBOX_ADDR=:8090
MAILBOX_TOKEN=$(openssl rand -hex 32)
```

The process refuses to start with a mailbox it cannot serve — no address, or no
token. That refusal is deliberate: a mailbox served nowhere would let the harness
start, keep its clock, wake every session on time and park every one of them
where nobody can ever take it. From outside, that is indistinguishable from a
quiet day.

## The token is the identity

`https://host:8090/mailbox/<token>` — the token in the path names **whose** turns
those are, and there is no way to ask for someone else's. Ten agents means ten
mailboxes with ten tokens, and nothing joins them. That is what makes running
several agents against one schedule a comparison rather than a mess.

It travels in the path rather than in a header because the client that most needs
this is a shell script holding a URL, and a URL that works with a bare `curl`
works everywhere. It is a credential all the same: it is never logged, and a
wrong one is answered exactly the way a path that does not exist is answered, so
guessing tells the guesser nothing.

## What a client does

| | |
|---|---|
| `GET  {url}/poll?wait=90` | one delivery, or `204` when the hold expires |
| `POST {url}/say`  | `{"turn":"…","text":"…"}` — appears in the room, goes in the record |
| `POST {url}/done` | `{"turn":"…","failure":"…"}` — ends the turn |
| `GET  {url}/state` | what is waiting, for an operator |

A delivery is one JSON object:

```json
{"event":"turn","thread":"mb-…","turn":"mb-…-t3","text":"…","model":"sonnet","at":"…"}
{"event":"steer","turn":"mb-…-t3","text":"and look at QQQ"}
{"event":"interrupt","turn":"mb-…-t3"}
```

`turn` is a window opening or a person writing. `steer` is a person writing into
a turn already running — the same turn, not a new one. `interrupt` is the harness
giving up on a turn that outran its limit; it ends the turn here without waiting
to be told that it did, because the case it exists for is a client that has
stopped answering.

Say as often as there is something worth saying. Say `done` exactly once. A turn
that is never ended is given up on after the harness's own limit, and until then
the next window is refused because one session is already running.

## Two shapes, and never cross them

**`poll-once.sh`** — waits for one thing, prints it, exits. The exit status is
the whole answer:

| | |
|---|---|
| `0` | something happened; the JSON object is on stdout |
| `3` | the hold expired with nothing to say; run it again |
| `4` | no mailbox there — wrong token, or none is served. Do not retry |
| `5` | that mailbox is gone |
| `64` | called wrong |
| `69` | could not be reached. Worth retrying |

**`poll-stream.sh`** — prints one JSON object per line, forever, and backs off
while the mailbox is unreachable. Everything that is not an event goes to stderr.

Give the first to something that waits for a process to exit, the second to
something that reads lines. Swapping them is how an event ends up printed where
nobody is reading, and it is then simply lost.

And read the exit table again before wiring the first shape to a harness that
wakes on it: `3` is an exit too. A harness that treats "the command finished" as
"something happened" wakes every hold, finds nothing, arms the poller again, and
wakes again - a carousel of turns in which nothing occurs but spend. Measured on
29 August: a session did exactly this until a human looked at the screen. The fix
is below.

## Attaching Claude Code

The whole installation as one prompt to paste into a session: **`INSTALL.md`**
beside this file. What follows is what that prompt does.

Claude Code wakes on the output of a long-running command, so it takes the
streaming shape:

```
Monitor: MAILBOX=https://host:8090/mailbox/$TOKEN /path/to/poller/poll-stream.sh
```

The address goes in the environment rather than in the command, and the scripts
take it either way. The token in it is the agent's identity - whoever holds it
takes that agent's turns and speaks as it - and an argument stands in the process
list for every other process on the machine to read. It also leaves the session
nothing to assemble: a command an agent has to build is one it can build wrongly,
and the wrong version reads a settings file it had no business opening.

Persistent. **Do not add `2>&1`.** Only stdout is an event; folding stderr into
it makes every reconnection and every backoff look like a turn to take, and the
session is then woken by its own plumbing.

Two more servers belong in the same session, and they are the ones that do the
work:

- **this project's own MCP** (`MCP_ADDR`, role `mcp`) — `record_intent`,
  `read_state`, `read_schedule`, `read_candidates`, `read_volatility_history`,
  `wake_me_at`, `wake_me_on_price`, `list_wakeups`, `cancel_wakeup`;
- **the broker, through the policy gateway** (`BROKER_MCP_URL`) — the orders. Not
  the broker directly: the gateway is what refuses, and it is what the record
  shows.

So the session reads its cause from the mailbox, does the work with those two,
and reports back with `reply.sh`:

```sh
reply.sh say  "$TURN" "sold the 640/635 put, credit 0.31, risk $469"
reply.sh done "$TURN"
```

## Attaching a harness that wakes when a command finishes

Some harnesses have no monitor and no stream: what wakes them is a background
task ENDING. Antigravity CLI is one, measured on 29 August - a live `tail -f`
never woke it, while a command that exited woke it within seconds and without a
word from the person.

That is the first shape's own case, with one thing to get right: `poll-once.sh`
also exits when the hold passes with nothing to say, and to such a harness that
exit looks exactly like an event. Left alone it becomes a carousel: wake, find
nothing, arm again, wake again, ninety seconds at a time, forever.

So give that harness a wrapper that ends only when there is something to hand
over:

```sh
#!/bin/sh
# Wait until a turn exists, then exit with it. Nothing else ends this.
while :; do
	out=$(poll-once.sh 90); code=$?
	case $code in
		0) printf '%s\n' "$out"; exit 0 ;;   # a turn: hand it over and end
		3) ;;                                # nothing yet: go round again
		*) echo "poller gave up, $code" >&2; exit "$code" ;;
	esac
done
```

Then the instruction to the session is one word - run that script in the
background - and the wake means what the harness thinks it means.

The same wrapper is worth writing even for a harness that reads lines, because
it puts the address inside the file: the session runs one word, constructs
nothing, and the token stays out of both its context and the process list.

## What this is for

Two things, and the second is the reason to build it now.

**A person can trade in the harness they already sit in**, watching the agent
decide and advising it in the moment, instead of reading a transcript afterwards.
The schedule, the limits and the record are the deployment's, not the laptop's.

**Agents become comparable.** Everything around the agent is the same code
reading the same declaration; the only difference is who takes the turn. That is
the condition an arena needs — several agents, several models, several harnesses,
one schedule and one way of recording what each of them did — and without it a
comparison between two agents is a comparison between two whole stacks.
