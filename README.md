# swiftward-alpaca-options-agents

An autonomous options trading agent that reads its own limits before it hits them.

Built for the Alpaca AI Trading Agents Hackathon, 28 August - 4 September 2026. Everything runs against Alpaca's paper trading environment: simulated funds, real market data, no real money.

## What it does

The agent trades defined-risk options structures on Alpaca. Every broker call passes through a policy gateway that does three things a prompt cannot:

- **states the limit in the tool description** the agent reads, so the agent plans an order it will be allowed to place;
- **refuses in a form a program can act on** - the refusal names the boundary that stopped it, so the agent adjusts instead of retrying;
- **accepts a limit change on a running session** - an operator tightens a ceiling and the live agent sees the new one without a restart.

## Layout

| Path | What is in it |
|---|---|
| `golang/apps/app` | one binary, four roles: `harness` holds the clock, `api` serves the read side and the built page, `mcp` carries the session's own tools, `envelope` answers what a caller may do |
| `golang/internal` | the packages behind it |
| `typescript/web` | the demo page: what the agent did, what it meant to do, where it was stopped |
| `docker` | one Dockerfile per service, including the egress proxy and its allowlist |
| `postgres/migrations` | schema, applied by the `migrate` service before anything reads it |
| `agent` | what the agent itself reads: its instructions, its skills and the declaration of when it wakes |
| `docs` | the submission write-up (`write-up.md`), the architecture and how the stack is deployed |
| `AGENTS.md` | how work is done in this repository, for a person or an agent editing it |

## Running it

```
cp .env.example .env     # fill in the keys
make local-up            # the whole stack, built from this checkout, agents included
```

`make prod-up` runs the same stack from published images. Both bring the agents up; a bare `docker compose up` does not, because the agents sit behind a compose profile.

Before a trading day, and it is worth doing while the market is closed:

```
make rehearse            # send a trading day's reads and print what was refused
```

It goes down the road the screener uses - each agent's own entry point, its own
credential, the same batch size, every caller at once. Reads work at the weekend,
so it answers on a Sunday the question a Monday would ask. What it exists for is
the failure a green test suite cannot see: a limit somewhere on the call path
sized for one caller, met by four.

## Licence

MIT. See `LICENSE`.
