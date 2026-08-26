# Architecture

Seven services, two networks, one binary with four roles. The shape exists to make one property true: **the agent reaches nothing except the services beside it and the hosts the proxy allows.**

Today it reaches the broker's own server directly, over `BROKER_MCP_URL`. That is the development account, and it is the one claim on this page that describes an intention rather than the running system: the policy gateway is not yet in front of the broker, so nothing between the agent and Alpaca refuses an order.

Its limits, though, already come from outside it. The session asks `read_envelope` before it builds anything and is told what applies to it and by which version of the rules; no number it sizes with is written anywhere in its prompt. Until the gateway is in front of the broker that answer comes from the `envelope` service beside it, under the same server name the gateway will use - so the limits are **disclosed and not yet enforced**, and the swap changes an address rather than the agent.

## Services

| Service | What it is | Where it can go |
|---|---|---|
| `agent` | our binary holding the clock, the volatility history and the session's tools, and the agent it starts | the broker's server, the envelope, Postgres, and the egress proxy |
| `envelope` | the same binary answering what one caller may do on one tool, from `agent/envelope.yaml` | nowhere: it reads a file |
| `page` | the same binary serving the read side and the built page | Postgres, and the broker for the money it shows |
| `migrate` | applies `postgres/migrations` in name order, then exits | Postgres |
| `alpaca-mcp` | Alpaca's own MCP server, pinned to a released version | Alpaca |
| `egress` | forward proxy with a host allowlist (`docker/egress/filter.txt`) | the hosts it allows |
| `postgres` | state | nowhere |

The policy gateway is not in this stack. It is a network service addressed by `GATEWAY_URL`, the same way Alpaca and the model provider are.

`envelope` stands in that address until the gateway does. It answers the envelope and nothing else: it holds no broker credential, reaches nothing, and cannot judge or refuse an order - those are the gateway's, and a stand-in that grew them would be a second engine nobody agreed to run. Which limits a caller gets is decided by the bearer token it was started with, so the two agents on the stack are under different ceilings and neither can ask for the other's.

The file it reads is mounted from the checkout rather than baked into the image, because lowering a ceiling has to be one edit that a running session sees on its next question. A limit that needs a restart to change is a limit nobody can be shown changing.

## Networks

`internal` has no route out, and a port published on it is never bound. `outbound` is ordinary bridged networking.

The agent sits on `internal` alone. Everything it can do is therefore enumerable: call the services beside it, or ask the proxy, which answers only for the hosts in its allowlist and logs every refusal. Widening that list is a change to this repository, not a decision the session can make.

`alpaca-mcp` also sits on `outbound`, because it talks to Alpaca. So does `page`: a browser has to reach it, and a port cannot be published on `internal`.

## The three roles

`ROLES` selects them. `harness` and `mcp` run together in `agent`, because the session reaches its own toolbox over localhost. `api` runs alone in `page`: it reads the record out of Postgres, so it needs nothing the session holds.

- **`harness`** decides *when* a session runs and says *why* it woke it. It never decides what to trade - the session does, and the autonomy requirement rests on that line. Two causes wake a session: the schedule in the declaration, and a person writing in the chat. With neither it refuses to start, because a harness that runs while waking nobody looks exactly like a working one.

  The harness runs the agent as a child process and reads its event stream: every message the session produces is posted to the chat, and each tool call replaces one status line rather than adding a message. A person writing back wakes the next session on the same thread, so the conversation continues rather than restarting. **The agent knows nothing about the chat** - that is what makes a session woken by the clock and one woken by a person the same thing from inside.
- **`api`** serves the read side: `/healthz`, `/state`, `/money`, `/equity` and the built page from `WEB_DIR`. It decides nothing and orders nothing; it reads the record, and it asks the broker what the account is worth.
- **`mcp`** is the session's own toolbox (`internal/sessiontools`), carrying what Alpaca's cannot: `record_intent`, which a session calls *before* it orders anything; `read_state`, which the next session calls to learn what already happened; and `post_to_chat`, which tells the people watching. A judge can see fills anywhere; only the first of these says what the session meant to do.

  `post_to_chat` exists only when a chat is configured. An agent that can see a tool assumes it works, so an unconfigured channel offers no tool rather than one that fails.

## The record

Five tables. Three carry what the agent did, and each answers its own question: `turns` - when a session ran and why; `tool_calls` - what it did with its hands, with the arguments it sent and what came back; `intents` - what it meant to do before it ordered anything. The harness writes the first two from the agent's own stream, whether or not a chat is watching; `record_intent` writes the third.

`volatility_samples` and `account_snapshots` carry what the market and the account were worth over time, written by the recorders below.

A refused order is in `tool_calls`, in the broker's own words. There is no separate table of refusals: the structured refusal is the gateway's, the gateway is a service outside this stack with no path into this database, and a table that can only be empty reads as "the agent was never stopped" rather than as "we do not know". It returns with the gateway that fills it.

It lives in Postgres because it is the evidence: a restart must not empty the page a judge is reading. `RECORD_SHOWS` bounds how many rows of each kind that page carries.

## The volatility history

An entry rule that compares today's implied volatility with its own past needs a past, and the broker answers only for today. So the process holding the clock reads one contract per underlying every few minutes while the market is open - the at-the-money put about three weeks out - and writes it to `volatility_samples`. The session asks `read_volatility_history` where the latest reading sits in that series.

Three weeks out, and always a put, on purpose. The option this project trades expires the same day: its implied volatility swings with the hour, so a series built from it measures the clock. And a series that mixes calls with puts moves when the skew moves, saying nothing about the level.

Nothing here decides anything: the reading is mechanical, no session is woken for it, and what a rank of 80 means is the session's to say.

`VOLATILITY_UNDERLYINGS` and `VOLATILITY_EVERY` turn it on. The series is worth what its length is, so it starts on the first day of the event and is never paused.

## Getting a decided order filled

The session decides what to sell, how large, and the price it wants. Walking a limit a cent at a time until the book takes it is not that kind of decision: it is arithmetic on a clock, it has to happen in seconds, and one turn of the agent costs a minute and a half. So the two are split.

`internal/execution` walks an unfilled multi-leg order toward the price the book is actually showing - every leg it sells at the bid, every leg it buys at the ask - one tick at a time, and stops there. It never asks for a worse price than the book's own, and it cancels what the book has refused for `EXECUTION_PATIENCE`. It can move a price and cancel an order; it can open nothing.

Measured on the account on 25 August 2026: a spread resting at the middle of the spread did not fill in ten minutes, which costs about a fifth of the credit on a dollar-wide structure - the reason this exists at all.

## Where the bound is

The session runs with the agent's own sandbox off and the container as the boundary: root filesystem read-only, one writable directory, no route out except the proxy, no broker credential, no docker socket. The agent's internal sandbox needs user namespaces the container does not grant, so switching it on stalls every command it tries to run - two boundaries where only one is real.

Approvals are answered the same way. Nobody sits at this keyboard, so the thread is opened with a policy that never asks: the agent acts inside the container or fails, and a request to leave it is not a question anyone is there to answer.

## Credentials

The broker keys are the environment of `alpaca-mcp` and reach nothing else. The agent holds a gateway token and no broker key; `page` holds neither. The agent's own login is Codex's, mounted read-only from the host that performed it and copied into a writable volume, because the CLI refreshes its token as it runs.
