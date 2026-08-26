---
name: playbook-premium-harvest
description: The rule for opening a premium-harvest position - sell a put credit spread and let time decay pay for it. Use in any entry window, whenever a task asks for premium-harvest.
---

# Premium harvest

Sell a vertical put spread and hold it while time passes. The credit is taken on the way in; what is left of it at expiry is the profit. The loss is bounded by the width of the spread, which is why it may be opened at all.

The rule is here once. A task that asks for premium-harvest may add to it or ask for less; it does not repeat it.

## Before anything

`read_envelope(tool="place_option_order")`. Which underlyings, which expirations, how large a position and how much of the account may be at risk at once - all of it comes from there, none of it from this file. If the envelope is silent, do not trade and say so. The skill `read-my-envelope` says the rest.

Then check the clock, the account and what is already open.

**The daily fuse.** Read the account. If equity is 2% or more below yesterday's close, open nothing today and say so in one line.

## Choosing

- **Underlying.** From the envelope's list, taken in turn. Start with those you have not looked at today. Single stocks have wider strikes, so one contract carries more risk than an ETF's - count contracts from the risk, not from habit.
- **Expiration.** From the envelope's range. Never the one expiring today: on its expiry day the broker computes no greeks at all, so there is no delta to choose a strike by, and by midday it pays almost nothing. The nearer expiration decays faster, the further one passes the thresholds more often - take whichever passes.
- **The short leg.** Delta about −0.15. The long leg is one strike below it.
- **Width.** The narrowest where both legs have a two-sided quote and the credit is at least a tenth of the risk. Risk is the width less the credit.
- **The cost of the round trip, worked out BEFORE the decision.** Add the bid-ask spread of both legs. If that is more than a third of the credit, the structure is no good however handsome its credit-to-risk looks. Measured 25 August: DIA showed 20.5% credit-to-risk while the round trip cost 0.13 against a credit of 0.085 - a loss dressed as a good ratio.

## Sizing

Work out the largest loss the structure can produce: the width times a hundred times the contracts, less the credit. Take the contract count as the largest whole number that stays inside the envelope's `position_max_loss`, computed against equity read from the broker.

Then check it fits: add up what every open position can lose and keep the total inside the envelope's `portfolio_max_loss`. The limit is held by risk, not by a count - ten small positions are an underfilled book, not a full one.

## How often

Not more than four entries into one underlying a day, and not more than twelve in a day altogether. Do not return to an underlying whose position the defence closed today because price crossed the short strike; a position closed in profit does not block a return - going back in is the point of taking profit.

How often you may open is also governed by a rule you cannot see the inside of. If an order is refused on it, that refusal is final: change what you are doing rather than sending it again.

## Before the order

Ask `read_volatility_history` for this underlying and put the rank you get into the thesis. Fewer than fifty readings: say the history is not there yet. The rank forbids nothing today - we are collecting it so that in a week we can ask, on our own trades, whether entries at a high rank end differently from entries at a low one.

Record the intent (`record_intent`) before sending anything: the thesis, the structure, the largest loss it can produce. Name in it the limit you sized against and the `ruleset_version` you read it from.

## Sending it

One order, all legs together, limit at the middle. Put the worst price you will accept into `client_order_id` as `worst=-0.11` - negative because a credit is negative - then a semicolon and something unrepeatable, because the broker refuses a name it has seen (`client_order_id must be unique`). For example `worst=-0.11;QQQ703-702-1226`.

The harness walks the price toward the book from there, waits at each price long enough to be taken, never asks for worse than your number, and cancels what the book will not take. Do not watch the order afterwards.

The worst price has to satisfy the same rule as the entry: credit-to-risk **at that price**, not at the one you asked for, at least a tenth. Otherwise you are agreeing to a fill you already called unfit. Beyond that, give up no more than a third of the credit; of the two bounds take the one that gives up less. Example: a spread five dollars wide, credit 0.67 at the middle. A tenth of the risk gives a worst price of 0.46 (at a credit of 0.46 the risk is 4.54); a third of the credit would give 0.45. The first gives up less, so `worst=-0.46`.

## Afterwards

Say what you did and what the broker answered. In the same line name two numbers from the account: equity now, and how much credit the open positions are holding. Do not spend a separate turn reporting that - it costs more than it tells.
