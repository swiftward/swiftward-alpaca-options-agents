# Staged markets

A scenario is a market the real one will not stage on request. Each file answers
a question with YES or NO and no profit in it at all: does the defence fire when
the price reaches the sold strike, does a sentry send an order at a price that
cannot exist, is a position sized from what was ordered or from what was filled.

Run one:

    ARENA_SCENARIO=arena/scenarios/walk-to-the-strike.json arena/run-proxy.sh

The proxy says so at startup and every five minutes afterwards, and **every fill
under a scenario is marked in the book** with the same mark a bench fill carries.
The judge prints that mark. Numbers from a staged market are not a measurement,
and the instrument is built so that nobody can later mistake them for one.

## What is staged and what is not

Only the three reads that carry a PRICE: the clock, the underlying's last trade
and the option snapshot. The chain, the contracts and the news still go to the
real broker - a scenario has no business inventing which contracts exist. Orders
never leave for the broker under any mode.

## Faults: taking a tool away

A market that misbehaves is only half of what breaks an agent. The other half is
a tool that stops answering while the market keeps moving - and that half leaves
no trace at all, because nothing crashes. Measured on the team's own stand on
31 August: a grant check refused every price read for eighteen minutes, and two
entry windows passed before anyone looked.

    "faults": [{
      "after": "8m", "until": "26m",
      "tools": ["get_stock_latest_trade", "get_option_snapshot"],
      "message": "rate limit reached for this session's grant check"
    }]

The named tools answer with the given message for that stretch and answer
normally outside it. Every tool the arena serves can be faulted, reads and orders
alike, because the two ask different questions: a read that stops answering asks
whether the agent notices it is deciding without data; an order that is refused
asks whether it changes its approach or repeats the same call.

Refused at load, for the same reason the rest is: a tool the arena does not
serve, a window that runs backwards, a fault with no message, a fault naming no
tools. Each of those is a fault that would never fire while looking like a test
that passed.

## Writing one

`start` is the scenario's own wall clock; it may be any date. `speed` compresses
time, so `60` makes one real second carry a scenario minute and an hour of market
fits in a minute of waiting. Steps are cumulative offsets from `start`, given in
order, and each names only what CHANGES: a contract absent from a step keeps the
book the step before gave it.

A scenario that would stage the wrong thing quietly is refused at load - steps
out of order, no start date, a price of nothing, a symbol that is not a contract,
a field nobody defined. The refusal names which step and why.

## Known weakness of qqq-crash.json, named by the Swiftward side

Reviewed 30 August against their own source. Three of the four numbers held up:
three percent in ten minutes happens; the 5.20 debit is legitimate because it is
the CROSSING price (buy the 710 at the 15.50 ask, sell the 705 at the 10.30 bid,
while the midpoints give 4.65); the depth is normal.

**The volatility is not.** Implied volatility walking 0.170 -> 0.250 across a
three percent fall is far too small for a contract expiring the same day. A
corrected version has to raise it a great deal - and cannot raise it alone: the
quoted prices carry only 0.30 to 1.20 of extrinsic value at the bottom, and that
extrinsic is what a volatility implies. Raise one without the other and the
snapshot states two numbers that contradict each other, which is the very thing a
participant is told to refuse. So the fix is a re-tuning of prices and greeks
together, not an edit of two fields.

**And the worry we named ourselves was backwards.** We asked whether a 0.50-0.60
spread at a price of 15 was too NARROW for a crash. It is too WIDE for an
ordinary day: one-day QQQ options quote in pennies. The staged book is therefore
pessimistic about liquidity, not optimistic - which is the safer direction to be
wrong in, but it is still wrong, and a run that reports slippage off this book
reports too much of it.
