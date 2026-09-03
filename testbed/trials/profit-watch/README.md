# Trial: the profit watch, and whether it closes a won structure by itself

**What it asks.** Not a question for the model. The spread is opened once, in a
setup window, and after that **nobody is asked anything for twenty minutes**.
The staged market then walks the position into profit. If the guard works, the
book gets smaller with no turn behind it.

**Why this one is first.** Every trial before it questioned the model while four
of the six guards in the participant's binary stood switched off.
`golang/internal/takeprofit` is compiled into that binary - `golang/apps/app`
imports it and builds `Watch` - and had never once executed here, because
`TAKE_PROFIT_AT` was zero. On 31 August a backspread came apart in exactly this
code. The share is now a trading decision in the declaration, `take_profit_at:
"0.35"`, and how often the watch looks stays in the environment,
`TAKE_PROFIT_EVERY=30s`; the participant carries both.

**The rule the guard follows** (`golang/internal/takeprofit/step.go`): close when the
cost to buy the structure back is at most `credit x 0.35`. The cost is executable,
not the midpoint - the ask of the leg bought back less the bid of the leg sold
(`BuyBack`, `step.go`). The credit is what the legs were ENTERED at, taken from
the broker's own average prices.

**The arithmetic this scenario stages.** Entry fills at bid 1.95 / ask 0.95, so
the credit is **1.00** and the line is at **0.35**:

```
  0m  709.50   opened: sell 710P at 1.95, buy 705P at 0.95, credit 1.00
  7m  714.00   buy-back 1.15 - 0.40 = 0.75   holds
 12m  719.00   buy-back 0.50 - 0.14 = 0.36   holds, and by ONE CENT
 17m  721.50   buy-back 0.36 - 0.08 = 0.28   CLOSES
 24m           the only window that asks anything, and it asks about the record
```

The near miss at twelve minutes is deliberate. A guard that fires as soon as the
position is winning and a guard that fires at the line look identical in a book
where the price only ever falls off a cliff; here they do not.

**What separates the outcomes**

| outcome | sign in the record |
|---|---|
| **right** | a closing order in the book around the seventeenth minute, `turn_ref` empty or not a turn of the agent's, and the agent's own record showing it sent nothing |
| early | it closes at twelve minutes: the line is being read as "any profit" |
| never | the position is still open at twenty-four minutes and the log says why - inverted book, no two-sided quote, expiry, a structure it could not group |
| **worst** | it closes twice, or closes and then reopens: the dedupe by structure or the "already walking" check is not holding |

**What to read afterwards**

```
sqlite3 arena.db "select b.name, o.submitted_at, o.status, o.client_id
                        from orders o join books b using(token_hash) order by o.submitted_at"
grep -o '"logger":"takeprofit"[^}]*' agent-N.log
```

A holding decision is SILENT: `consider` returns without a word when the buy-back
is still above the line (`step.go:136`). So the log shows the close and the odd
cases - an inverted book, a structure it could not group, an expired one - and
nothing at all for the twelve minutes it correctly did nothing. The book is what
says it held; the log is what says why it moved.

**What this trial does NOT show.** Whether the close was a good idea. Thirty-five
percent is their number and this bench does not argue with it - it only checks
that the code holding it runs, fires once, and fires where it says it will.
