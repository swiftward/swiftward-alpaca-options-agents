---
name: playbook-event-convexity
description: Buy the gap a scheduled number opens, on the expiration that dies the same day - entered the afternoon before, closed in the first hour after. Use when a task names a macro release by date.
requires: [event_bet_share, event_exit_by]
---

# Event convexity

A number that moves the whole market comes out before the opening bell. Nobody can
be positioned for it after it lands: the price is already there when trading starts.
So the position is taken the afternoon before and sold into the open.

This is the bet with an uncapped right tail, and it is the reason the rest of the
book is kept modest. It burns more often than it pays - and that is the shape we
chose on purpose, not a defect to be tuned away.

## What this is not

It is not a backspread. A backspread has a short leg, and on the day of expiry that
short leg puts the valley of the payoff exactly on the most likely outcome - a small
gap. Here we buy outright and own no short leg at all.

## Before anything

`read_envelope(tool="place_option_order")` in this turn, then the account and the
positions.

**Cancel what is resting.** A working order holds buying power at the full width of
its structure with no credit netted against it. This bet is the largest of the week
and it must not be sized down by three forgotten orders.

**Say what you already hold that this event will move.** Sold put spreads lose on the
same fall this bet wins on; that is the point of the pairing, and it belongs in the
thesis.

## The structure

On SPY, expiring the day the number lands.

- About two thirds into the at-the-money straddle: it starts paying at roughly one
  and a half times the move the market has priced.
- About one third into a strangle a percent and a half out on both sides: it pays
  nothing on a small move and multiples on a large one.

Both bought outright, no short legs. Two orders, or one for each side of each - name
what you sent.

## Size

`event_bet_share` of equity, and inside whatever the envelope allows. It is the
whole ticket price and it is expected to burn: money spent here is gone unless the
number surprises.

Work out and say the arithmetic before sending: the straddle price is close to the
overnight move the market has priced, so doubling needs a gap around half again as
large as that. Name how much money that is, name what a total loss costs the week,
and only then send.

## Getting out - by rule, not by judgement

Everything closed by `event_exit_by`. Two ways it goes:

- **The gap is smaller than what the position needs to break even**: close at once,
  in the first minutes. Decay on the day of expiry plus the volatility falling after
  the number eat what is left within the hour. Waiting for it to come back is how
  the whole ticket is lost instead of part of it.
- **The gap is there**: sell in pieces as it moves, and be finished by
  `event_exit_by`. Nothing is carried into the afternoon: this position was bought
  for one move and holding it afterwards is a different trade nobody decided on.

Closing is sending an order, and the book is wide in the first minutes after a
release. Check that it actually closed, and say the price you got rather than the
price you wanted.

## Before the order

`record_intent`: what the market has priced for the overnight move, what gap the
position needs to break even, the whole cost as a share of equity, what a total loss
does to the week, and the ruleset version. Write it as a bet with a stated price -
because that is what it is, and a week from now the record must show it was taken
with open eyes.
