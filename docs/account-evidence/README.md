# The account, frozen

A copy of what the demo page served for the judged account, taken at the moment in
`taken-at.txt`. Five files, exactly the five routes the page reads:

| File | What is in it |
|---|---|
| `money.json` | the account, every open position, and every order with its legs |
| `equity.json` | the equity line, snapshot by snapshot |
| `state.json` | turns, what woke each one, what the session said, every tool call with its arguments and answer, the intents, the execution steps |
| `limits.json` | the limits in force, as the agent reads them |
| `sweep.json` | what the screener's last pass found |

It carries no credential: the page has never held one, and the export is checked
against every secret in `.env` before it is written.

**Check it without trusting us.** The tool that reads the live page reads this too:

```
make account-claims DIR=docs/account-evidence
```

No network, no key, no broker. It asks whether the trading matches what the rest of
`docs/` says: every order a structure rather than a naked leg, every leg declaring
whether it opens or closes, one server behind every order, and no intent recorded
knowing its limits had not been read.

**One thing was cut, and here is exactly what.** The equity line begins at
2026-08-31T00:16:31Z, the minute the judged account was funded at 100,000. The
record keeps snapshots without naming the account they belong to, so the rows before
that minute belong to an account that was replaced, and a curve that ran them
together would be a curve of two accounts. 1,165 rows kept of 2,480. That the record
does not name the account per snapshot is a defect of ours; the fix is a column, and
until it lands the honest thing is to cut at the funding minute and say so.

## What the week shows

| Day | Open | Close | High |
|---|---|---|---|
| 31 August | 100,000 | 103,041 | 103,333 |
| 1 September | 103,041 | 105,077 | 105,461 |
| 2 September | 105,077 | 104,654 | 105,389 |
| 3 September | 104,654 | 91,231 | 104,654 |

Three days up, +5.5% at the peak, and one day that gave it back and more. The
result is what it is, and the interesting part is that the record says WHY rather
than leaving it to be guessed at.

**What the book held on the last day.** Three call credit spreads - SPY 772/773
expiring 4 September, SPY 772/773 expiring 8 September, IWM 300/302 - all short
calls, sold when the market was quiet. SPY rose about a per cent. Every one of them
went against us at the same time, because they were the same bet three times: that
is what `same_direction_max_loss` exists to bound, and the book sat close to that
bound rather than beyond it.

**What held.** Every position was defined-risk before it was opened: the worst case
of each was computed at entry, the broker held the collateral for it, and no naked
short option was ever opened. The loss is bounded by construction rather than by
anybody's attention, and the account was never at risk of the kind of loss that
ends an account. The daily fuse stopped new entries. Every order, every refusal and
every intent behind them is in `state.json`.

**What did not hold, and it is one thing.** The per-position ceiling is applied by
the ladder to a RESTING order, and the IWM structure filled instantly - a two-wide
spread sized as though it were one wide, 119 sets against a $9,298 ceiling at a true
worst case of $20,349. There was nothing left to cancel. Both halves of our own
arithmetic were right and neither was in the path of the order. The conclusion is in
`../write-up.md` and in `../algorithm.md`: a limit that lives with the caller is
advice however carefully it is computed, and the refusal has to belong to the thing
the order passes through.

That is the whole of it. A strategy whose thresholds come from 646 days of measured
history rather than from taste, a book bounded by construction, one identified
defect in where a ceiling is enforced, and a record complete enough that a stranger
can find all three without asking us.
