---
name: playbook-premium-harvest
description: The rule for opening a premium-harvest position - sell a vertical credit spread and let time decay pay for it. Use in any entry window, whenever a task asks for premium-harvest.
requires: [short_leg_delta, min_edge_points, min_edge_points_borrowed, daily_fuse_percent]
---

# Premium harvest

Sell a vertical credit spread and hold it while time passes. The credit is taken on the way in; what is left of it at expiry is the profit. The loss is bounded by the width of the spread, which is why it may be opened at all.

**Which SIDE is the task's to say, not this file's.** A put spread below the price and a call spread above it are the same technique mirrored, and the price cannot go both ways at once - so a second side on an underlying that already has one is the cheapest credit in the book. On 26 August this file said "put spread", a session read that as a prohibition and refused a call it had already selected. Anything here that reads as a restriction on WHAT to pick is a defect in this file: the mechanics live here, the choices live in the task.

The rule is here once. A task that asks for premium-harvest does not repeat it.

**The numbers below are this playbook's usual ones, not its law.** Where your task names a different delta, a different credit-to-risk threshold or a different ceiling on the cost of the round trip, that number replaces the one here and the mechanics stay as written. Two accounts run this playbook side by side precisely so that such numbers can differ - a session that quietly keeps the number from this file has cancelled the experiment it was opened for. Say in one line which number you took and where it came from.

Four of them are not this file's to hold at all, and an agent that is not given them refuses to start. They stand at the top of every turn you are given - whatever woke you, a window, your own wake-up or a person - under "Numbers this agent runs on":

- `short_leg_delta` - how far out the sold leg goes.
- `min_edge_points` - the least `edge_points` a structure may show to be taken.
- `min_edge_points_borrowed` - the same, where the volatility behind the measure was borrowed from another expiration.
- `daily_fuse_percent` - how far below yesterday's close the account may fall before today is over.

If they are not in front of you, something is wrong with how you were woken: say so and open nothing. Do not supply them from this file, from your notes, or from what a session did last week.

The limits are the other way round: what the envelope hands you is never replaced by anything. The full order is in `AGENTS.md`.

## Before anything

`read_envelope(tool="place_option_order")`, in this turn - not once earlier in the conversation. Which underlyings, which expirations, how large a position and how much of the account may be at risk at once: all of it comes from there, none of it from this file. If the envelope is silent, do not trade and say so. The skill `read-my-envelope` says the rest.

Then check the clock, the account and what is already open.

**The daily fuse.** Read the account. If equity is `daily_fuse_percent` or more below yesterday's close, open nothing today and say so in one line. The number is the task's, never this file's: it stood here as 2% while the task said 3%, and on 27 August one account refused an entry at 14:01 and took one at 14:18 on the same two figures.

The fuse is not enforced by anything but you. Nothing refuses the order if you skip this check, so skipping it is not caught, it is simply a day the account keeps losing.

## Choosing

- **Underlying.** From the envelope's list, taken in turn. Start with those you have not looked at today. Single stocks have wider strikes, so one contract carries more risk than an ETF's - count contracts from the risk, not from habit.
- **Expiration.** From the envelope's range, and from nowhere else - if the envelope permits today, today is permitted. The nearer expiration decays faster, the further one passes the thresholds more often; take whichever measures better.
  On expiry day the broker computes no greeks and no volatility at all, so the screener borrows the volatility from the nearest other expiration of the same underlying and says so in `edge_from`. A borrowed number errs the dangerous way - the very short end usually sits above the days behind it, so it understates how often the strike is reached - which is why the TASK asks more of a borrowed measure than of a delta one. Follow the task.
- **The short leg.** How far out is the TASK's to say, not this file's: the two accounts sell at deliberately different distances, and that difference is the experiment they exist for. The long leg is one strike further out.
- **Width.** Prefer THREE to FIVE strikes, and this is measured, not stylistic. Over 646 days on SPY and QQQ, per unit of risk: one strike returned +0.8%, two +4.2%, three +12.0%, five +23.4%. The crossing is paid once whatever the width, so a wider structure carries the same cost over more credit. Take a narrow one only when nothing wider clears the threshold, and say so. Both legs need a two-sided quote. Risk is the width less the credit.
- **Whether the list may be used at all.** `read_candidates` answers `fresh`, and
  that is the whole test - take it as given. Do NOT work freshness out from
  `seconds_old` yourself, and above all do not read a rising age across two reads
  as the screener having stopped: sweeps come at an interval, so inside one cycle
  the age rises by design. That exact misreading cost an entry on 27 August - the
  window saw 280 seconds become 309, called the screener dead and sent nothing,
  while the screener was sweeping every five minutes without a miss.
  `fresh` false is no list at all; `fresh` true means use it - and still re-read
  the legs before ordering, because prices move inside a cycle even when the list
  is doing its job.
- **Ranking and the threshold are two different things.** `edge_points` orders the
  list; the threshold is what your task names, and it applies to the number you
  work out from FRESH quotes after re-reading the legs. Never treat the list's own
  best value as the bar: the list has aged, the fresh number is almost always
  lower, and "at least the best in the list" is a bar nothing can clear. That
  happened on 27 August - 6.37 in the list, 2.2 on fresh quotes, refused, while
  2.2 was the best thing seen all day.
- **A candidate that dies on the fresh quote does not end the turn - take the
  next one.** The list is ranked, and the one at the top is the one whose edge
  had furthest to fall. Re-read the legs of the best; if it no longer measures
  up, go to the second, then the third, and stop only when the list runs out or
  one holds. Refusing the best and going home is how a whole day produces no
  trades while the list was full.
  Measured 27 August on the first account: 19 entry windows ran to completion and
  filled TWO orders. Every refusal read the same - screened +8.03 became -2.41 on
  the fresh quote, +3.51 became -2.25, +2.87 became +1.40 - and most sessions
  stopped at the first name instead of asking the second what it paid now.
- **What to rank on.** `edge_points` from `read_candidates`: how many percentage points the structure pays above what it has to survive. Both halves at once - a delta ceiling keeps what is far and throws away what pays, a credit threshold keeps what pays and ignores how often it loses.
  Crossing the book is already taken out of it, so there is no separate rule about what the round trip costs. `credit_after_cost` beside `credit` shows how much a structure gives up getting in. A structure quoted wide shows a worse `edge_points` on its own; one measured on the displayed midpoint scored seven points better than the cheaper structure that actually earns.

## Never sell premium into a report

Do not sell a spread on an underlying whose earnings fall before the structure
expires. The measure that ranks the list will disagree with you, and it is wrong
here on purpose: premium is dear before a report, so the structure looks like the
best thing on the screen. The measure works out how often price reaches the strike
from delta, and before an event the distribution has two humps - delta understates
exactly the tail the premium is being paid for.

Selling premium into an event is a fat LEFT tail, which is the one thing this
account is told to avoid. Seen on 26 August: the screener put NVDA 207.5/205 puts
at the top with an edge of +6.38 on the day NVDA reported, and the order was taken
away by hand. Worse, it would have cancelled our own bet on that same event - a
backspread pays when price falls hard, and that spread loses when it does.

The rule fires on a CONFIRMED date before expiry, not on silence. Learn the date
from the news (`get_news`) or ask the session that watches it. Nothing found: say
so in one line and take the trade. Silence is not evidence - on 26 August a veto
by silence took DELL and AMD away, and neither had a report in the window at all,
so the rule was costing trades where the tail it exists for was absent.

A tail needs an APPOINTED event; missing information does not create one. We bet on
events by BUYING convexity, not by selling premium.

## Sizing

Work out the largest loss the structure can produce: the width times a hundred times the contracts, less the credit. Take the contract count as the largest whole number that stays inside the envelope's `position_max_loss`, computed against equity read from the broker.

Then check it fits: add up what every open position can lose and keep the total inside the envelope's `portfolio_max_loss`. The limit is held by risk, not by a count - ten small positions are an underfilled book, not a full one.

## How often

Not more than four entries into one underlying a day, and not more than twelve in a day altogether. Do not return to an underlying whose position the defence closed today because price crossed the short strike; a position closed in profit does not block a return - going back in is the point of taking profit.

How often you may open is also governed by a rule you cannot see the inside of. If an order is refused on it, that refusal is final: change what you are doing rather than sending it again.

## Before the order

Ask `read_volatility_history` for this underlying and put the rank you get into the thesis. Fewer than fifty readings: say the history is not there yet. The rank forbids nothing today - we are collecting it so that in a week we can ask, on our own trades, whether entries at a high rank end differently from entries at a low one.

Record the intent (`record_intent`) before sending anything: the thesis, the structure, the largest loss it can produce, and the underlying's price as you just read it. Name in it the limit you sized against and the `ruleset_version` you read it from. The price is refused if empty: the windows that watch this position measure how far the underlying has travelled since, and only the starting point cannot be recovered afterwards.

## Sending it

One order, all legs together, limit at the middle. Put the worst price you will accept into `client_order_id` as `worst=-0.11` - negative because a credit is negative - then a semicolon and something unrepeatable, because the broker refuses a name it has seen (`client_order_id must be unique`). For example `worst=-0.11;QQQ703-702-1226`.

The harness walks the price toward the book from there, waits at each price long enough to be taken, never asks for worse than your number, and cancels what the book will not take. Do not watch the order afterwards.

The worst price has to satisfy the same rule as the entry: work `edge_points` out again **at that price** - the chance of surviving, less risk over credit plus risk - and it must still clear the threshold you entered on. Otherwise you are agreeing to a fill you already called unfit. Beyond that, give up no more than a third of the credit; of the two bounds take the one that gives up less.

## Afterwards

Say what you did and what the broker answered. In the same line name two numbers from the account: equity now, and how much credit the open positions are holding. Do not spend a separate turn reporting that - it costs more than it tells.
