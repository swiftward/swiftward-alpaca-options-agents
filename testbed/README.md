# testbed

A stand that plays an agent market conditions and failures the real market will
not produce on request, and measures what the agent does in reply.

**No submitted order ever passed through it. Every order on both accounts went to
Alpaca's own MCP server.** The testbed sits beside the trading path, never on it:
it holds no broker credential, and the agent it questions is a separate process
on a separate account.

It is not a backtest and not a trading simulator. It replays no history and
predicts no price. It stands where the broker would stand, answers a question,
and records what came back.

Two ways to move a market, and the difference matters:

- **staged** (`scenarios/`) — prices, clock and option book come from a file. For
  questions the market will not stage on request: the price reaching a sold
  strike, an assignment between the legs of a spread, a tool that stops
  answering mid-session.
- **overlay** (`proxy/overlay.go`) — every read goes to the real broker and one
  number is moved: the underlying, along a curve. Each contract is repriced from
  that move by its own live implied volatility, so the book is the real one
  everywhere else. At zero displacement the overlay equals the live market to the
  cent, which is the property the whole thing rests on.

## What it has caught

- A guard that decided what to close by counting LEGS - right while only verticals
  were held, wrong from the first backspread (`trials/profit-watch`, and the test
  that now refuses a new shape without a verdict,
  `golang/internal/takeprofit/shapes_test.go`).
- A defence that looks every fifteen minutes at a corridor the price stands inside
  for a median of fourteen. On 415 traverses of SPY this year no scheduled check
  fell inside the corridor on 132 of them (`trials/strike-corridor`,
  `trials/defence-corridor.py`).
- A session that reported a failed gate check twice, eleven minutes apart, and then
  did nothing while the position stood undefended. It had already happened before
  it was made into a trial (`trials/broken-gate`).
- Price reads refused for eighteen minutes while the underlying walked through the
  sold strike, with nothing crashing and two entry windows passing
  (`trials/blind-defence`).
- A candidate paying three times the ordinary credit because one short leg is
  naked and its loss has no floor (`trials/no-floor-ratio`).

`trials/` holds the questions that have been asked and what each one found — a
scenario, the sessions that ask it, and a README saying what separates a right
answer from a wrong one. `trials/defence-corridor.py` and
`trials/hold-or-close.py` need no agent at all: they are arithmetic on the
declaration and on the market's own record.

Build and test: `make -C testbed test`. This module is deliberately outside the
repository's `go.work`: a stand for questioning the agent has no business sharing
a build with the agent.
