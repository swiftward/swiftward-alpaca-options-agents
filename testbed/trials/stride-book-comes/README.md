# Trial pair: `tick` against `arrive`, with only the tactic changed

_Two bundles, `stride-book-stays` and `stride-book-comes`. Same order, same
limit, same floor, same contracts. The only thing that differs between two runs
of one bundle is `EXECUTION_STRIDE`._

## Why the bench and not the accounts

On the night of 1 September the team put BOTH live accounts on `arrive`. That is
a defensible bet, and it also means the comparison no longer exists anywhere in
their production: two accounts running the same tactic measure the same thing
twice. Their earlier plan - one tactic per account - would not have settled it
either, because those two accounts differ in more than the tactic: different
entries, different structures, a different book in the minute each order was
placed.

A staged book has none of that. Hold the market still, place the same order, and
change one environment variable.

## The claim under test, in their own words

_Translated from the original; this repository is written in English._

> The arriving walk cannot end at a worse price than a cent a step: both stop in
> the same place - at the floor... Its true price is narrower than "can be
> worse": a slow order sometimes waits for the book to come to it and fills
> better than the floor, while a fast one has already conceded by then.

Both halves are testable, and each bundle tests one.

## `stride-book-stays` — the benefit

The order asks 1.20 credit. The book shows 0.90, improves once to 1.05 at the
sixth minute, and then stands. The floor is 0.85, so the target is the book.

```
tick     one cent a step: 15 cents of distance is more than patience allows -> cancelled unfilled
arrive   remaining over steps left: converges on the moving target inside patience -> filled
```

If that is what happens, their measured 37% and 44% of orders dying unfilled has
a mechanical explanation, and `arrive` is the answer to it.

## `stride-book-comes` — the cost they call bounded

Same order, but the book walks all the way to 1.20 by the fourth minute.

```
tick     has conceded ~5 cents by then, and is taken at about 1.15
arrive   has conceded ~15 cents by then, and is taken at about 1.05
```

The difference is what the faster tactic pays for arriving early, on a trade that
would have filled either way. Their claim is that this is bounded by the distance
from the floor to where the book came. The bench puts a number on it instead of a
bound.

## `stride-book-runs` — the only shape where the fast tactic can lose money

The first two bundles both hold a book that stands still or improves, and on those
two `arrive` came out strictly not worse: same fill price, more arrivals. That is
not the whole question, and the team named the missing half themselves -
translated: "the price is going against us: the gap grows, and the step grows
with it, we follow".

Here the book runs away and then comes back further than it started:

```
 0m   executable credit 0.90     the order asks 1.20, floor 0.50
 3m   executable credit 0.60     the book has walked AWAY
 9m   executable credit 1.15     it comes back, better than it began
```

The floor is set low on purpose — 0.50, well under where the book runs to — so
that the session's own bound is not what saves the tactic. What is being measured
is the tactic, not the reservation.

```
tick     concedes a cent per pass, is nowhere near 0.60 when the book runs,
         and is still resting at about 1.12 when the book returns -> takes 1.15
arrive   remaining over steps left, and the remaining GROWS as the book runs:
         its steps grow with it, it reaches 0.60 and fills there
```

If that is what happens, the cost of chasing is the whole distance between 0.60
and 1.15 — and it is not bounded by anything in the rule. Whether a book that
whipsaws like this is common is a question about markets, not about the ladder;
the trial only says what the two tactics do when it happens.

## Timing is part of the design, and the first attempt got it wrong

In `stride-book-comes` the book was first set to arrive four minutes in. The order
is placed by an agent in a setup window that opens at two minutes, so it lands
around the third - and the book arrived before the ladder had taken a single step.
Measured 1 September: `tick` filled at -1.20, its own limit, after zero walks.
Both tactics would have done exactly that, and the trial measured nothing.

The book now arrives at the EIGHTH minute: late enough that a cent a step has
conceded something real, early enough to be inside the eight minutes of patience.
A trial whose two arms cannot differ is not a trial, and it looks like a result.

## How to run one comparison

```
EXECUTION_STRIDE=tick   run-trial.sh stride-book-stays 5
  ... read out, then ...
EXECUTION_STRIDE=arrive run-trial.sh stride-book-stays 5
```

`EXECUTION_STRIDE` has to reach the participant's process; put it in the run
script's environment beside `EXECUTION_EVERY`, not in a shared env file, so that
the two runs of a pair cannot silently share one value.

Read out with `execution_steps` - the price before and after every move - and the
book's `filled_avg`.

## What this cannot show

The arena fills a resting order the moment the book crosses its limit, at that
limit. A real venue can fill better than the limit, and does not promise to fill
a spread at all merely because the quote crossed. So this measures the TACTIC -
how far and how fast the price is conceded, and whether it arrives before
patience - and not what a real book would have done with it.

Both scenarios are anchored to now (`"anchor": "now"`): they stage a book and
leave the clock alone, because the ladder measures ages against broker
timestamps.
