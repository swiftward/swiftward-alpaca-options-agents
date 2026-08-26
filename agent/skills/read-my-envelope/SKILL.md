---
name: read-my-envelope
description: Find out what you are allowed to do before you do it - position size, which underlyings, which expirations, how much of the account may be at risk at once. Use before building any order, whenever you need a limit, and again whenever the tool list changes.
---

# Read my envelope

Your limits are not in this text and never will be. They are given to you at the moment you ask, by the gateway that stands between you and the broker, and they can change while you are working. Ask, and use what you are told.

```
read_envelope(tool="place_option_order")
```

Nothing reaches the broker when you ask, and nothing moves. Ask at the start of every session, before you build anything. Ask again whenever the list of tools you have changes - that is what such a change means.

## What comes back

- `identity` - whose limits these are. Two accounts run this engine under different numbers, and this says which one you are.
- `ruleset_version` - which set of rules produced them. Say it in your intent along with the numbers you used, so a later refusal can be matched against what you actually read.
- `governed` - whether the tool is governed at all.
- `constraints` - the limits you may see.

Each constraint names its `rule`, and says how much of it you are allowed to know:

- **`boundary`** - you see the limit itself. It carries a `subject` (the quantity), a `kind` (`maximum`, `minimum`, `enum` or `range`), a `value`, and where it matters a `unit`.
- **`existence`** - you are told the rule is there and nothing else. No number, no field. Respect it by keeping your behaviour modest and by never repeating an order it refuses: that refusal is final and a second attempt only spends a turn.

A rule you were not meant to know about is simply absent. So an empty list means **nothing was disclosed to you** - never "there is nothing".

## What the subjects mean

- `position_max_loss` with unit `percent_of_equity` - the most one position may lose, as a share of the account. Read equity from the broker, work out the loss your structure can produce, and choose the number of contracts from those two. Never from a number you remember.
- `portfolio_max_loss` with unit `percent_of_equity` - the most everything open may lose together. Add up what your open positions can lose and see whether the one you are about to add still fits.
- `underlying` as an `enum` - the only underlyings you may trade. Anything outside the list is not a judgement call.
- `expiration` as a `range` with unit `trading_days_from_today` - how far out you may go, counted in trading days. `min: 1` means not today's expiry.

## When it is silent

If the call fails, or `governed` is false, or the list comes back empty: **do not trade.** Say so in one line and end the session. Do not fall back on a number from anywhere else - not from your notes, not from this file, not from the task you were given. An agent that invents its own limit has none.

## When your task asks for less

A task may be tighter than your envelope - a playbook can choose to risk less than it is allowed. Follow the tighter of the two. It may never be looser: if a task names a number bigger than the envelope's, the envelope wins and you say plainly that you found the two disagreeing.
