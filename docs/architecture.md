# Architecture

Five services, two networks, one binary with three roles. The shape exists to make one property true: **the agent cannot reach the broker except through the policy gateway, and cannot reach anything else at all.**

## Services

| Service | What it is | Where it can go |
|---|---|---|
| `app` | our binary, running any of three roles | the gateway, Postgres, the internet |
| `alpaca-mcp` | Alpaca's own MCP server, pinned to a released version | Alpaca |
| `agent` | the trading session: `codex` and nothing else | only the services beside it, and the egress proxy |
| `egress` | forward proxy with a host allowlist (`docker/egress/filter.txt`) | the hosts it allows |
| `postgres` | state | nowhere |

The policy gateway is not in this stack. It is a network service addressed by `GATEWAY_URL`, the same way Alpaca and the model provider are.

## Networks

`internal` has no route out and no published ports. `outbound` is ordinary bridged networking.

The agent sits on `internal` alone. Everything it can do is therefore enumerable: call the services beside it, or ask the proxy, which answers only for the hosts in its allowlist and logs every refusal. Widening that list is a change to this repository, not a decision the session can make.

`app` and `alpaca-mcp` also sit on `outbound`, because one talks to the gateway and the other to Alpaca.

## The three roles

`ROLES` selects them; they share one process and one in-memory state.

- **`harness`** holds the clock. It decides *when* a session runs and records *why* it woke it. It never decides what to trade - the session does, and the autonomy requirement rests on that line. With no declaration it refuses to start: a harness that runs while waking nobody looks exactly like a working one.
- **`api`** serves the read side: `/healthz`, `/state`, and the built page from `WEB_DIR`. It decides nothing.
- **`mcp`** is our own MCP server, carrying what Alpaca's cannot: `record_intent`, which a session calls *before* it orders anything; `read_state`, which the next session calls to learn what already happened; and `post_to_chat`, which tells the people watching. A judge can see fills anywhere; only the first of these says what the session meant to do.

  `post_to_chat` exists only when a chat is configured. An agent that can see a tool assumes it works, so an unconfigured channel offers no tool rather than one that fails.

## Credentials

The broker keys are the environment of `alpaca-mcp` and reach nothing else. `app` holds a gateway token; the agent holds neither. The agent's own login is Codex's, mounted read-only from the host that performed it and copied into a writable volume, because the CLI refreshes its token as it runs.
