# An options agent that trades inside limits it can read

An autonomous agent trades defined-risk option structures on an Alpaca paper account. A schedule decides *when* it runs and tells it *why*; the agent decides *what* to trade. Everything it did and everything it meant to do is written down as it happens, and the demo page shows both.

## The AI logic

The agent is a model session with tools, not a script with a model in it. Its schedule is a declaration, one file per agent in `agent/`:

```yaml
  - name: entry
    cause: "the main entry window of the day"
    at: "14:20"
    within: 45m
    days: [mon, tue, wed, thu, fri]
    task: |
      The main entry window of the day. Run premium-harvest.
```

`at` and `within` mean *fire at 14:20, and still count as due for 45 minutes* - so a restart at 14:35 does not lose the window, and a restart at 18:00 does not open a position nobody asked for. `every` with `between` fires repeatedly inside a window. `model` names a cheaper model for a session that only reads the news.

One session at a time holds the agent, because two sessions on one account close each other's positions: a session that comes due while a turn is running waits and tries again a minute later. `cannot_wait: true` is the exception, and the window that empties the book before the bell carries it - waiting past that window is the same as not running at all, so the task is said into the turn already running instead.

Three things wake a session: the schedule, a person writing in the chat, and a wake-up the session set for itself (`wake_me_at`, `wake_me_on_price`). It keeps one conversation across all of them, so the session that closes a position remembers opening it - and the conversation survives a restart, because the thread is kept on disk.

The session carries tools the broker's server does not have:

| Tool | What it answers |
|---|---|
| `record_intent` | states the thesis, the structure and the accepted loss - called *before* any order |
| `read_state` | what earlier sessions did, meant to do, and what came back |
| `read_schedule` | when it will be woken and why, read from the declaration rather than guessed |
| `read_volatility_history` | where today's implied volatility sits in its own recorded history, ranked 0 to 100 |
| `read_candidates` | what the screener's last pass found, with the edge it measured on each structure |
| `score_placements` | where to put the legs of a structure whose worst case sits in the middle: every placement the limits allow, priced at the sides of the book and replayed against that underlying's own history in weather like today's |
| `wake_me_at`, `wake_me_on_price`, `list_wakeups`, `cancel_wakeup` | its own standing requests |

The volatility history is ours: the broker answers what an option costs now, and two of the three entry rules compare today with its own past. A recorder reads the option closest to the money on every watched underlying every few minutes and writes it to `volatility_samples`.

## Risk gates

- **Defined risk only.** Every position is a spread whose largest possible loss is known before it is opened. No naked short options.
- **Size comes from the envelope, not from the prompt.** The agent asks what one position may lose (`position_max_loss`, 10% of equity today), what everything staked on one side of the market may lose together (`same_direction_max_loss`, 35%), and what the whole book may lose (`portfolio_max_loss`, 80%). It sizes to nine tenths of the position ceiling, because the ceiling is a share of equity and equity moves while the order rests in the book.
- **Intent before order.** The agent calls `record_intent` with the thesis, the structure and the maximum loss before it orders, and the record carries both, so what was declared can be checked against what was done. It is a rule the agent follows and the record exposes, not a lock on the broker: the order goes to a different server, and an order that skipped the intent would show as a fill with nothing behind it.
- **Defence on a clock.** Every thirty minutes the agent looks at what it holds and closes a vertical whose BOUGHT strike the price has passed - not the sold one. Measured over 638 trades and two and a half years: closing on a touch of the sold strike returns -$0.33 a trade, closing on the bought strike +$0.86, because at the sold strike the spread is already worth about 0.62 of its width and 37% of those cases finish out of the money anyway. It counts the legs before it decides: a structure with more legs, or more bought than sold, is not a vertical and is left alone.
- **Winners are bought back by a watch, not by a turn.** Every thirty seconds a process checks each open structure against the book and closes it once the buy-back costs no more than 0.35 of the credit it was opened for. An agent's turn costs a minute and a half and defence comes round every thirty minutes; a number crossing a line is arithmetic on a clock. Measured on the minute-by-minute path of 553 trades over 646 days: holding to expiry returns $2,287 with 26% losing trades, closing at 0.35 returns $6,292 with 9%.
- **Nothing else is closed early.** A same-day spread lives on time decay, and closing it early pays the spread twice while collecting half of what it was opened for.
- **A daily halt.** Down 2% from yesterday's close, the entry windows open nothing for the rest of the day.
- **Flat by the close.** A session at 15:40 closes anything whose short strike sits within fifty cents of price - the positions that risk assignment - and lets the rest expire. It starts no later than 15:49, so the market-order fallback still has the time it needs before the bell.
- **Every call written down.** `tool_calls` carries the server, the tool, the arguments and the outcome of every call the session made. A call still in flight when a process dies is recorded as `unknown`, never as done: an order in that state may or may not have reached the broker, and the record does not choose.

## Deciding and executing are different jobs

The agent states the structure, the size and the price it wants. A separate module walks that order toward the price the book is showing, a tick at a time, never past it, and cancels what the book refuses. That split is deliberate: a model choosing what to trade is judgement, a limit price moving by a cent is arithmetic on a clock, and each is done by the thing that is good at it. The order is still sent by the session, through Alpaca's own MCP server.

## Alpaca's infrastructure

Orders and market data go through Alpaca's own MCP server - the released `alpaca-mcp-server` package, pinned, unmodified - and it holds the only copy of the account keys. Nothing here reimplements it or calls Alpaca's REST in its place.

The policy gateway stands in FRONT of that server, not instead of it: the agent calls the gateway, the gateway decides and records, and the call it forwards is an MCP call to Alpaca's own server. One process per account, because that server reads its keys from its own environment and therefore serves exactly one.

What we measured on the account rather than read in a document:

- greeks and implied volatility come from `get_option_snapshot`, and only for contracts with a two-sided quote; the option chain does not carry them;
- index options (SPXW) carry neither on this account tier, while ETF options (SPY, QQQ, IWM) carry both - which is why the agent trades ETF options;
- `get_option_chain` needs `feed=indicative` on this tier;
- a vertical spread is **one** order: `place_option_order` with `order_class=mleg` and a negative limit price for a credit. The agent never sends two orders and never risks half a structure.

## How to check any of this

The page shows the account, the equity line, open positions, every order with its legs, every tool call with its arguments and outcome, the intents and the turns. It is a read side: it decides nothing and can only read.

Everything it shows comes from Postgres and from the broker, and both are the same sources the agent used.
