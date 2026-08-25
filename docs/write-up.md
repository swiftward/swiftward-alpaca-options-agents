# An options agent that trades inside limits it can read

An autonomous agent trades defined-risk option structures on an Alpaca paper account. A schedule decides *when* it runs and tells it *why*; the agent decides *what* to trade. Everything it did and everything it meant to do is written down as it happens, and the demo page shows both.

## The AI logic

The agent is a model session with tools, not a script with a model in it. Its schedule is a declaration, `agent/agent.yaml`:

```yaml
  - name: entry
    cause: "окно входа во второй половине сессии"
    at: "14:20"
    within: 45m
    days: [mon, tue, wed, thu, fri]
    task: |
      Правило входа: продажа вертикального put-спреда с истечением сегодня.
      ...
```

`at` and `within` mean *fire at 14:20, and still count as due for 45 minutes* - so a restart at 14:35 does not lose the window, and a restart at 18:00 does not open a position nobody asked for. `every` with `between` fires repeatedly inside a window. `model` names a cheaper model for a session that only reads the news.

Three things wake a session: the schedule, a person writing in the chat, and a wake-up the session set for itself (`wake_me_at`, `wake_me_on_price`). It keeps one conversation across all of them, so the session that closes a position remembers opening it - and the conversation survives a restart, because the thread is kept on disk.

The session carries tools the broker's server does not have:

| Tool | What it answers |
|---|---|
| `record_intent` | states the thesis, the structure and the accepted loss - called *before* any order |
| `read_state` | what earlier sessions did, meant to do, and what came back |
| `read_schedule` | when it will be woken and why, read from the declaration rather than guessed |
| `read_volatility_history` | where today's implied volatility sits in its own recorded history, ranked 0 to 100 |
| `wake_me_at`, `wake_me_on_price`, `list_wakeups`, `cancel_wakeup` | its own standing requests |

The volatility history is ours: the broker answers what an option costs now, and two of the three entry rules compare today with its own past. A recorder reads the option closest to the money on every watched underlying every few minutes and writes it to `volatility_samples`.

## Risk gates

- **Defined risk only.** Every position is a spread whose largest possible loss is known before it is opened. No naked short options.
- **0.5% of capital per position, at most eight open.** Sizing is arithmetic on the account the broker reports, not a number in a prompt.
- **Intent before order.** No order is sent before `record_intent` has stated the thesis, the structure and the maximum loss. A fill can be read anywhere; only that record says what the agent meant.
- **Defence on a clock.** Every thirty minutes the agent looks at what it holds and closes a position whose short strike price has crossed. Nothing else is closed early: a same-day spread lives on time decay, and closing it early pays the spread twice while collecting half of what it was opened for.
- **A daily halt.** Down 2% from yesterday's close, the entry windows open nothing for the rest of the day.
- **Flat by the close.** A session at 15:50 closes anything whose short strike sits within fifty cents of price - the positions that risk assignment - and lets the rest expire.
- **Every call written down.** `tool_calls` carries the server, the tool, the arguments and the outcome of every call the session made. A call still in flight when a process dies is recorded as `unknown`, never as done: an order in that state may or may not have reached the broker, and the record does not choose.

## Alpaca's infrastructure

Orders and market data go through Alpaca's own MCP server, pinned to a released version and holding the only copy of the account keys. The agent reaches it over MCP and holds no credential of its own.

What we measured on the account rather than read in a document:

- greeks and implied volatility come from `get_option_snapshot`, and only for contracts with a two-sided quote; the option chain does not carry them;
- index options (SPXW) carry neither on this account tier, while ETF options (SPY, QQQ, IWM) carry both - which is why the agent trades ETF options;
- `get_option_chain` needs `feed=indicative` on this tier;
- a vertical spread is **one** order: `place_option_order` with `order_class=mleg` and a negative limit price for a credit. The agent never sends two orders and never risks half a structure.

## How to check any of this

The page shows the account, the equity line, open positions, every order with its legs, every tool call with its arguments and outcome, the intents and the turns. It is a read side: it decides nothing and can only read.

Everything it shows comes from Postgres and from the broker, and both are the same sources the agent used.
