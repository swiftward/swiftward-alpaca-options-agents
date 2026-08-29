# Installing the poller: connecting Claude Code to this harness

Below is the prompt that does the whole installation. Paste it whole into a fresh
Claude Code session, filling in five values from the `.env` of the deployment you
are connecting to. Nothing else needs preparing.

What you get: the clock, the schedule and the limits stay on the deployment,
while the turns are taken by a session a person is sitting in. The room and the
record are the same as for every other agent, so the result is comparable with
them rather than with somebody else's stack.

---

## The prompt

> Connect me to the Swiftward trading harness through its mailbox. The values:
>
> ```
> MAILBOX=https://HOST:8090/mailbox/TOKEN
> POLLER=/path/to/the/repository/poller
> SESSION_MCP_URL=http://HOST:8081/mcp
> GATEWAY_URL=http://HOST:8082/mcp
> GATEWAY_TOKEN=...
> BROKER_MCP_URL=https://HOST/mcp/alpaca-agent-NAME
> BROKER_MCP_TOKEN=...
> ```
>
> Do this in order.
>
> **1. Check the mailbox before anything else.** `curl -s $MAILBOX/state` must
> return JSON with `thread`, `pending`, `running`. A 404 means the token is wrong
> or the deployment came up without `AGENT_DRIVER=mailbox`; do not go further,
> tell me.
>
> **2. Connect the three MCP servers.** Exactly the three the agent on codex
> sees:
>
> ```sh
> claude mcp add --transport http session $SESSION_MCP_URL
> claude mcp add --transport http gateway $GATEWAY_URL --header "Authorization: Bearer $GATEWAY_TOKEN"
> claude mcp add --transport http broker  $BROKER_MCP_URL --header "Authorization: Bearer $BROKER_MCP_TOKEN"
> ```
>
> Check that all three answer with a list of tools. `broker` has about fifty,
> `session` nine, `gateway` one.
>
> **3. Set up a monitor.** Persistent, with the command:
>
> ```
> MAILBOX=$MAILBOX $POLLER/poll-stream.sh
> ```
>
> The address goes in the ENVIRONMENT rather than in the command. Two reasons,
> and both are about who else can see it. The token in that address is your
> identity: whoever holds it takes your turns and speaks as you. Passed as an
> argument it stands in the process list, readable by every process on the
> machine - which matters the moment a machine carries more than one
> participant. And a command a session has to build is a command a session can
> build wrongly; with the address already in the environment, arming the monitor
> is one word and nothing to get wrong.
>
> All three scripts take the address either way: an argument when you have one,
> `MAILBOX` when you do not.
>
> **Do not add `2>&1`.** Only stdout counts as an event; with stderr merged in,
> every reconnect and every backoff becomes a "turn", and the session will wake
> itself with its own plumbing.
>
> **4. Remember what to do when the monitor prints something.** Each line is one
> JSON object:
>
> - `{"event":"turn", ...}` - a scheduled window opened, or I wrote to you in the
>   room. `text` is the task, `turn` is what to attach the answer to.
> - `{"event":"steer", ...}` - a person added to a turn already running. It is
>   the same turn, not a new one: take what was said into account and carry on.
> - `{"event":"interrupt", ...}` - the harness gave up waiting. Stop working; the
>   answer is of no interest to anyone now.
>
> On a `turn`, work like this:
>
> 1. `gateway` - ask for the envelope. The limits come from there, not from this
>    text. If the envelope is silent, do not trade and say so; do not substitute a
>    number from nowhere.
> 2. `session` - `read_state`, and where needed `read_schedule`,
>    `read_candidates`, `read_volatility_history`.
> 3. Before EVERY order - `session` - `record_intent`: the thesis, the structure,
>    the maximum loss. Recording the intent before the action is what is later
>    used to check what was declared against what was done.
> 4. Orders go only through `broker` (`place_option_order`). Never reach Alpaca's
>    REST API directly under any circumstances: the platform's rules require MCP
>    or CLI, and the gateway is what refuses and records.
> 5. Report and close the turn:
>
> ```sh
> $POLLER/reply.sh say  "$TURN" "what you did and why, in a sentence or two"
> $POLLER/reply.sh done "$TURN"
> ```
>
> `say` as many times as you like - it is what people in the room see. `done`
> exactly once, when the turn is finished. A turn nobody closed keeps the next
> window shut: the harness believes the session is still working.
>
> **5. Rules that outrank your habits.** Invent no rules of your own: limits
> added by self-review multiply, and the agent stops entering anything at all
> while continuing to sound careful. Numbers come from the envelope and from the
> task; where the task and the playbook differ, the task wins. The full list is
> `agent/AGENTS.md` in the repository - read it once and hold to it.
>
> **6. Tell me how it went**, and show me the first turn you picked up when it
> arrives.

---

## Checking it is up without waiting for a window

Writing into the deployment's room (Telegram, the agent's topic) is also a reason
to wake the session, and it walks the same path: mailbox, monitor, answer. If a
deployment has no chat, wait for the next scheduled window or add a session with
`every: 1m` to the declaration for a while.

The mailbox's state at any moment:

```sh
curl -s "$MAILBOX/state"
# {"thread":"mb-…","pending":0,"running":[],"hold":"1m30s","stale":"10m0s"}
```

`pending` above zero and growing means turns are parking and the monitor is not
taking them: it is not running, it died, or nobody reads its output. `running`
that never empties means turns are not being closed with `done`.

## What not to do

**Do not run two monitors on one token.** One turn goes to one poller - the
second will take the next one, and two clients will work interleaved under one
identity. A second agent needs a second process with its own token, its own
declaration and its own database; this deployment already works that way, each
agent a separate service.

**Do not give `poll-once.sh` to a monitor.** It says one thing and exits; a
monitor reading lines will see one and decide the stream ended. Monitors get
`poll-stream.sh`; `poll-once.sh` is for whatever wakes on a process exiting.
