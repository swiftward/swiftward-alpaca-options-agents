# The read API

Five routes, no credential, no write. They are what the page reads, what
`make account-claims` checks, and what anyone else can query directly. Everything
below is live at `https://alpaca.swiftward.dev`.

The read side decides nothing and can order nothing: its broker credential is an
observer's, and the code that answers these routes has no path to `place_option_order`.

| Route | Answers | Source |
|---|---|---|
| `GET /api/money` | the account, every open position, and every order with its legs | the broker, read live |
| `GET /api/equity` | the equity line, snapshot by snapshot | the record |
| `GET /api/state` | turns, what woke each, the agent's own words, every tool call with its arguments and answer, the intents, the execution steps | the record |
| `GET /api/limits` | the limits in force for this agent, as the agent is told them | the risk engine, read live |
| `GET /api/sweep` | what the screener's last pass found, with the measure it ranked on | the record |

## What each one carries

**`/api/money`** - `account` with `number`, `status`, `options_trading_level`,
`equity`, `equity_yesterday`, `cash`, `buying_power`, `options_buying_power` and
`position_market_value`; `positions[]` with the symbol, side, quantity, average
entry, current price, market value and unrealised profit; `orders[]` with `id`,
`client_id`, `status`, `position_intent`, quantities, prices, timestamps and
`legs[]`. This is the broker's own answer, shaped and not summarised.

**`/api/equity`** - a list, oldest first, each row `recorded_at` with the same money
fields as the account. The line the week is judged on.

**`/api/state`** - six lists. `turns` (when a session ran and how it ended),
`causes` (what was put in front of each turn, and by what), `said` (what the session
wrote, in its own words), `calls` (`server`, `tool`, `arguments`, `status`, `answer` -
the refusals among them included), `intents` (the thesis, the structure, the accepted
loss, the underlying's price at the moment it decided, and whether the limits were
read in that same turn), and `steps` (each rung of an execution ladder).

**`/api/limits`** - `tool`, `identity`, `ruleset_version`, `governed`, and
`constraints[]`. Each constraint carries `rule` and `disclosure`: a `boundary`
also carries `subject`, `kind`, `value` and `unit`; an `existence` carries only
`says`, because some rules are disclosed as present without their number.

**`/api/sweep`** - `candidates[]` with the underlying, type, expiration, both legs
and strikes, the credit, the risk, the crossing cost, the credit after it, the short
leg's delta, `edge_points` and `edge_from` - and `taken_at`.

## Checking the account against these documents

```
make account-claims PAGE=https://alpaca.swiftward.dev
```

Six checks, no credential: every order a structure rather than a naked leg, every
leg declaring whether it opens or closes, one server behind every order, the limits
in force disclosed, and no intent recorded knowing its limits had not been read. The
tool is `tools/account-claims.py`, and `DIR=docs/account-evidence` runs the same
checks against a copy of those five answers committed here, with no network at all -
see `docs/account-evidence/README.md`.
