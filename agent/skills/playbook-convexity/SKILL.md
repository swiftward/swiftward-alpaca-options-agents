---
name: playbook-convexity
description: Buy a backspread for near nothing, so the day the market moves hard is not a day this account only loses. Manage what the layer already holds before buying more. Use for the convexity layer.
requires: [convexity_short_leg_distance, convexity_valley_distance, convexity_worst_case_share, convexity_layer_share, convexity_daily_debit, convexity_roundtrip_share, convexity_take, convexity_horizon]
---

# Convexity layer

This layer exists for the day the market moves hard - the day the premium sold
elsewhere loses all at once. So here we do not sell the move, we buy it, and we
buy it for as close to nothing as the chain allows.

## Before anything

`read_envelope(tool="place_option_order")` in this turn, then the account and the
positions.

## Manage what is open first, then buy

Do this before considering anything new. Otherwise convexity gets bought and never
sold: the move comes intraday and gives itself back, and profit not taken is zero.

- **Worth `convexity_take` of what was paid** - close it with an opposing mleg order
  and say what you took. This is the day the layer was bought for.
- **A day or less to expiry and the move never came** - close while something is
  still bid. On the last day it is worth almost nothing and falls fastest.
- **It outlives `convexity_horizon`** - close it. That parameter is the last day
  whose result is counted; a structure expiring after it cannot pay into the
  answer this account is judged on, whatever it does afterwards.

  Read the horizon, do not assume it. "The end of the week" is the wrong rule and
  it cost a round trip on 27 August: the layer was opened on a Thursday for an
  expiry six days out, and an hour later the same rule closed it - and by that
  rule the layer could never open on any Thursday at all, because every
  expiration the envelope permits outlives the Friday. The horizon is a date the
  declaration knows and this file does not.
- **None of these** - say in one line what you hold and why, and go on to a new one.

## The structure

A backspread: sell ONE option nearer the money and buy TWO of the same type and the
same expiration further out. One mleg order for all three legs.

- **Side.** Put backspread if it is our sold put spreads that need covering; call
  backspread if we are mostly in call spreads. Look at which we hold more of and
  cover that.
- **Underlying.** SPY or QQQ - the narrowest bid-ask.
- **Expiration.** Two to five trading days, so the move has time.
- The sold leg must be covered by the bought ones in count: sell one, buy two.
  Anything else is naked risk.

## Where the legs go - measured, not judged by eye

This is the part that decides whether the structure is worth opening at all, and
it is not a matter of taste.

First work out **sigma**: the move this underlying is expected to make by the
expiration you chose. Read the volatility (`read_volatility_history`), multiply by
the square root of trading days remaining over 252, multiply by the price. Say the
number out loud - every rule below is in it.

- **The sold leg no closer than `convexity_short_leg_distance`.**
- **The bought strike - the valley - no closer than `convexity_valley_distance`.**

The valley is where the worst case sits: at the bought strike, at expiration. Put
it where the market arrives on an ordinary move and it eats everything the
structure earns the rest of the time. Measured over 466 windows: every placement
with the valley inside two sigma has a NEGATIVE expectation, and on 28 August a
structure sold at 0.57 sigma with the valley at 1.25 lost money by construction
before the market did anything.

**If the chain offers no strikes that satisfy both, open nothing and say why.** A
backspread placed closer than this is not a cheap bet on a large move; it is a
bet that the market moves a lot or not at all, and pays for the space between.

## Size - by the WORST CASE, never by the entry price

Entry here is near zero, so a limit worked out from what was paid constrains
nothing: at two cents a set, "how much fits under the ceiling" means hundreds of
sets. That is not a hypothetical - it is how this layer went wrong before.

The worst case of a backspread sits in the valley: at the bought strike, at
expiration. It is the width between the legs times a hundred times the number of
sets, less the credit received.

- One structure: worst case no more than `convexity_worst_case_share` of equity.
- The whole layer, counting what is already open: no more than
  `convexity_layer_share`. Read what the layer holds (`read_state`, intents marked
  as convexity) and add it up. Out of room - say so in one line and finish.
- Number of sets comes from the worst-case limit, never from the price. One
  contract pays nothing, but "how many they will hand you" is not a size either.

## Price is the SECOND filter, not the first

Near zero or a credit. Nobody overpays for the ticket, but a cheap ticket is not a
reason to buy an unsized one.

- Debit across the layer, per day: no more than `convexity_daily_debit`.
- **Work out the round trip before deciding, on all three legs.** Three legs cost
  more to enter and leave than a vertical, and that cost is what turns "entry near
  zero" into a daily expense. Measure it as a share of the structure's WIDTH, not
  of the credit: the credit here is about zero, and dividing by it forbids
  everything. Dearer than `convexity_roundtrip_share` - do not take it.
- Already holding one on the same underlying and the same expiration - do not
  double it. Take another underlying or say there is no room.

## Say what it does not cover

**The valley does not insure against a moderate move, it doubles the loss.** If the
market slides a percent or two into expiration, the sold spreads lose and so does
this - its worst point is exactly there. It pays on a large move. Say that in the
thesis rather than presenting it as insurance against everything.

## Before the order

`record_intent`, and say in the thesis that this is the convexity layer and what it
covers. Then one line: what you took, for how much, the largest loss, and the equity
it is measured against.
