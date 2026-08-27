---
name: playbook-earnings-crush
description: Sell the premium a company's report has inflated, on the expiration that survives the report - and only when the measurement says the market is paying more for the move than the company has historically made. Use when a task names a report by date.
requires: [crush_worst_case_share, crush_implied_over_realized]
---

# Earnings crush

Before a report nobody knows the number, so options are dear. A minute after it,
the unknown is gone and they are cheap again - even when the stock itself barely
moved. This playbook sells that difference.

**This is the one place where selling premium into an event is allowed, and it is
allowed only on the measurement below.** Everywhere else the rule is the opposite,
and for a good reason: selling into an event is a fat left tail. What makes this
different is not the situation, it is that the price is checked against history
before anything is sent.

## Before anything

`read_envelope(tool="place_option_order")` in this turn. Then the account, the
positions, and the clock.

**Cancel what is resting first.** A working order holds buying power at the full
width of its structure, with no credit netted against it - measured on this account.
Three forgotten orders can leave a bet unable to size. Cancel the ones this playbook
did not place, or say plainly that you could not.

## The measurement that decides the side

Do this before choosing anything, and say the numbers out loud.

1. **What the market is paying for the move.** Take the at-the-money straddle on the
   expiration that comes AFTER the report - the one that expires before it is dead
   the moment the report lands, and carries none of the event. Its price divided by
   the underlying's price is the implied move.
2. **What the company has actually done.** Daily bars go back years (`get_stock_bars`).
   Earnings dates show as the quarterly gaps - but **take them by the grid, not by
   size**: they sit sixty to seventy trading days apart, and a day that does not
   land on that grid is thrown out however large it was. This is not a detail. A
   name like this has huge days that have nothing to do with its report - a sector
   shock, a tariff headline - and counting those raises the median of what the
   company "usually" does, which pushes the ratio down and sends the whole session
   the wrong way. Take the last eight to twelve grid days, and the median of the
   absolute move the day after each.
3. **Divide.** Implied over median realized.

Then:

- **Ratio at or above `crush_implied_over_realized`** - the market is paying more
  than this company usually delivers. Sell the crush.
- **Ratio well below it** - the market is paying LESS than the company usually
  delivers, and selling would be picking up the wrong side of the same coin. Then
  buy the move instead, with a backspread on the side the position is leaning, and
  say that you flipped and why.
- **Between** - take nothing and say what you measured. A trade you cannot justify
  by the number is the thing we already refused as a candidate.

Fewer than eight past reports, or bars that will not come: **no trade.** An unmeasured
event is not a small edge, it is a guess.

## The structure

Iron condor on the expiration AFTER the report. Four legs, one order - four is
also the most this broker takes in one multi-leg order.

- Short legs outside the implied move worked out above, near 0.15-0.20 delta.
- Long legs one or two strikes further out. They are what makes the loss known.
- Both sides. The point is that the price falls whichever way the stock goes; taking
  one side is a directional bet wearing this playbook's clothes.

Check the cost of the round trip on all four legs before deciding, as a share of the
structure's width. Four legs cost more to enter and leave than two, and on a name
that has just reported the book is wide at the open - that cost is paid out of the
same money the crush is meant to earn.

## Size

Worst case no more than `crush_worst_case_share` of equity, and inside whatever the
envelope allows.

This is deliberately small. The right tail here is capped by the long legs, so the
trade cannot win big - it can only not lose. A profile like that does not win a
race, and sizing it as though it could is how a capped upside becomes an uncapped
regret. The size that can win is spent on the bet whose upside is not capped.

## Getting out - and the flip has its own

**If the measurement sent you to the flip - a backspread bought instead of a condor
sold - it comes out on the same morning, in the same half hour, by the same rule.**
Say plainly in the intent that what you hold is the flip, so the session that closes
it knows what to look for. This matters because everywhere else in this project a
backspread is the convexity layer, which is held for days; this one is not. It was
bought for one report, the report has happened, and the volatility it was bought on
falls all Thursday. Held past the morning it is not a position, it is a leak.

## Getting out

Close the morning after the report, in the first half hour, and do not wait for the
last cent of the premium to decay. The crush happens at the open; what comes after
is drift, and drift is the risk this structure is not paid to hold.

If the stock went through a short strike, close anyway and say what it cost. The
structure did its job by bounding the loss - holding it for a recovery is a
different trade nobody decided to make.

## Before the order

`record_intent`, and in the thesis: the implied move, the median realized, their
ratio, which way it sent you, the worst case in dollars, and the ruleset version you
read the limits from. Someone reading this back in a week must be able to see the
decision, not just the position.
