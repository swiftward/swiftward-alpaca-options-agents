# swiftward-alpaca-options-agents

An autonomous options trading agent that reads its own limits before it hits them.

Built for the Alpaca AI Trading Agents Hackathon, 28 August - 4 September 2026. Everything runs against Alpaca's paper trading environment: simulated funds, real market data, no real money.

## What it does

The agent trades defined-risk options structures on Alpaca. Every broker call passes through a policy gateway that does three things a prompt cannot:

- **states the limit in the tool description** the agent reads, so the agent plans an order it will be allowed to place;
- **refuses in a form a program can act on** - the refusal names the boundary that stopped it, so the agent adjusts instead of retrying;
- **accepts a limit change on a running session** - an operator tightens a ceiling and the live agent sees the new one without a restart.

The same strategy declaration is read twice: by the backtester over history, and by the gateway during live trading.

## Layout

| Path | What is in it |
|---|---|
| `golang/apps/app` | one binary, three roles: `harness` holds the clock, `api` serves the read side and the built page, `mcp` carries the session's own tools |
| `golang/apps/backtest` | runs a strategy declaration over historical data |
| `golang/internal` | packages behind those two |
| `typescript/web` | the demo page: current limits, recent refusals, open positions |
| `docker` | one Dockerfile per service, including the egress proxy and its allowlist |
| `postgres/migrations` | schema, applied by the `migrate` service before anything reads it |
| `playbooks` | strategy declarations, one file per strategy |
| `agent` | what the agent itself reads: prompts, skills, MCP configuration |
| `docs` | write-up and architecture |

## Running it

```
cp .env.example .env     # fill in the keys
make up                  # local stack, built from this checkout
```

`make prod-up` runs the same stack from published images.

## Licence

MIT. See `LICENSE`.
