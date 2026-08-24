# Architecture

Six services, two networks, one binary with three roles. The shape exists to make one property true: **the agent cannot reach the broker except through the policy gateway, and cannot reach anything else at all.**

## Services

| Service | What it is | Where it can go |
|---|---|---|
| `agent` | our binary holding the clock and the session's tools, and the agent it starts | the broker's server, Postgres, and the egress proxy |
| `page` | the same binary serving the read side and the built page | Postgres |
| `migrate` | applies `postgres/migrations` in name order, then exits | Postgres |
| `alpaca-mcp` | Alpaca's own MCP server, pinned to a released version | Alpaca |
| `egress` | forward proxy with a host allowlist (`docker/egress/filter.txt`) | the hosts it allows |
| `postgres` | state | nowhere |

The policy gateway is not in this stack. It is a network service addressed by `GATEWAY_URL`, the same way Alpaca and the model provider are.

## Networks

`internal` has no route out, and a port published on it is never bound. `outbound` is ordinary bridged networking.

The agent sits on `internal` alone. Everything it can do is therefore enumerable: call the services beside it, or ask the proxy, which answers only for the hosts in its allowlist and logs every refusal. Widening that list is a change to this repository, not a decision the session can make.

`alpaca-mcp` also sits on `outbound`, because it talks to Alpaca. So does `page`: a browser has to reach it, and a port cannot be published on `internal`.

## The three roles

`ROLES` selects them. `harness` and `mcp` run together in `agent`, because the session reaches its own toolbox over localhost. `api` runs alone in `page`: it reads the record out of Postgres, so it needs nothing the session holds.

- **`harness`** decides *when* a session runs and says *why* it woke it. It never decides what to trade - the session does, and the autonomy requirement rests on that line. Two causes wake a session: the schedule in the declaration, and a person writing in the chat. With neither it refuses to start, because a harness that runs while waking nobody looks exactly like a working one.

  The harness runs the agent as a child process and reads its event stream: every message the session produces is posted to the chat, and each tool call replaces one status line rather than adding a message. A person writing back wakes the next session on the same thread, so the conversation continues rather than restarting. **The agent knows nothing about the chat** - that is what makes a session woken by the clock and one woken by a person the same thing from inside.
- **`api`** serves the read side: `/healthz`, `/state`, and the built page from `WEB_DIR`. It decides nothing and reaches nothing but the record.
- **`mcp`** is the session's own toolbox (`internal/sessiontools`), carrying what Alpaca's cannot: `record_intent`, which a session calls *before* it orders anything; `read_state`, which the next session calls to learn what already happened; and `post_to_chat`, which tells the people watching. A judge can see fills anywhere; only the first of these says what the session meant to do.

  `post_to_chat` exists only when a chat is configured. An agent that can see a tool assumes it works, so an unconfigured channel offers no tool rather than one that fails.

## The record

Three tables, and each answers its own question: `turns` - when a session ran and why; `intents` - what it meant to do before it ordered anything; `refusals` - where a boundary stopped it. The harness writes a turn when it starts one and closes it when the agent's stream ends, whether or not a chat is watching; `record_intent` writes the second; the gateway's refusal writes the third.

It lives in Postgres because it is the evidence: a restart must not empty the page a judge is reading. `RECORD_SHOWS` bounds how many rows of each kind that page carries.

## Where the bound is

The session runs with the agent's own sandbox off and the container as the boundary: root filesystem read-only, one writable directory, no route out except the proxy, no broker credential, no docker socket. The agent's internal sandbox needs user namespaces the container does not grant, so switching it on stalls every command it tries to run - two boundaries where only one is real.

Approvals are answered the same way. Nobody sits at this keyboard, so the thread is opened with a policy that never asks: the agent acts inside the container or fails, and a request to leave it is not a question anyone is there to answer.

## Credentials

The broker keys are the environment of `alpaca-mcp` and reach nothing else. The agent holds a gateway token and no broker key; `page` holds neither. The agent's own login is Codex's, mounted read-only from the host that performed it and copied into a writable volume, because the CLI refreshes its token as it runs.
