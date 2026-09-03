# strike-corridor

**Asks:** the defence closes a spread only while the underlying stands BETWEEN
the sold and the bought strike. It looks every fifteen minutes. Is it ever
looking while the price is in there?

## Why this is a question at all

The rule is the agent's own and it is well argued (`agent/alpaca-agent-1.yaml`, session
`defend`): beyond the bought leg the loss already equals the width and will not
grow, so closing there pays the crossing for nothing. The task text in
`sessions.yaml` is that rule **verbatim** - copied, not paraphrased - because a
rule measured through a prompt of ours would be measuring the prompt.

What the rule cannot state about itself is whether its own schedule reaches the
window it fires in. A one-dollar SPY spread has a one-dollar corridor. From
SPY's own minutes this year (`testbed/trials/defence-corridor.py`), the price
stands inside such a corridor for a **median of 14 minutes**, a quarter of the
time for **6 minutes or fewer** - and the checks are **15 minutes apart**. On 415
upward traverses, no scheduled check fell inside the corridor on **132 of them**,
32%: on those the rule could not have fired whatever the agent decided.

That is arithmetic on a schedule. This trial is the same thing happening live,
to a real position, with the real code deciding.

## The market

An OVERLAY, not a staged market: every read goes to the real broker, and only
SPY's price is displaced - along a straight line, zero to +8.00 over an hour.
Each contract is repriced from that move by its own live implied volatility, so
the book the agent reads is the real one everywhere except in the one number.

Eight dollars in sixty minutes is **7.5 minutes to cross a one-dollar corridor**.
That is faster than the median traverse and slower than the fastest quarter: an
ordinary day, not a staged emergency. The point is not to make the corridor
impossible to catch - it is to let an ordinary one go past the schedule.

## The reading

One number decides it: **was a defence turn running while the price stood
between the two strikes**, and did it close.

Three outcomes, and they are different findings:

1. **A check fell inside and the spread was closed** - the rule works, and the
   schedule reached it. Nothing to report but a green line.
2. **A check fell inside and the spread was NOT closed** - the rule was reachable
   and the agent did not apply it. That is a question about the agent.
3. **No check fell inside** - the price entered the corridor after one turn and
   left it before the next. That is a question about the schedule, and no model
   would have answered it differently.

## The control - run this second, and do not skip it

The same scenario with the defence at `every: 3m`. If the third outcome above
holds and the control CLOSES the spread, then the schedule is the cause and the
finding stands. If the control also fails to close, the cause is somewhere else
and the first run proved nothing: six of our instrument's own faults on 31 August
had exactly this shape - a measurement that succeeds and answers a different
question than the one asked.

Raise it the way `testbed/trials/README.md` describes, on this folder's
`scenario.json`, read it out, and put the participant back before the next run.
