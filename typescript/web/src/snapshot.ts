// What the account did, frozen.
//
// The landing does NOT call the API. `/live` does, and it is the page to open to
// watch the account move; this one is a poster and has to render the same on a
// judge's screen whether or not the stand is up, whether or not the broker
// answers, and after the week is over. A landing that shows "the broker did not
// answer" where its own results should be has argued against itself.
//
// Every figure here was read off the running account, not composed.
export const OPENED = 100_000

// THE RESULT, and the rule that defines it.
//
// The organiser reads total equity at the close of Thursday 3 September, and the
// broker's own record of that close is what this carries. The rule is quoted rather
// than paraphrased, and it is kept in full in the team's own `docs/rules.md`.
//
// The platform states a second rule - P&L as of the submission deadline, with
// trading after it not counting - and that rule is quoted where the result is
// stated. It has no row of its own here: nobody reads the account at that minute,
// and a row headed by a measurement that is never taken promises a number this page
// would then have to invent.
export type Measurement = {
  by: string
  when: string
  rule: string
  equity: number | null
}

export const MEASUREMENTS: Measurement[] = [
  {
    by: 'Alpaca',
    when: 'end of Thursday, 3 September',
    rule: 'Total account equity, not cash balance. Open positions enter it at their mark.',
    // The broker's own record of that close, its `last_equity` field, which its API
    // reference defines as "Equity as of previous trading day at 16:00:00 ET".
    //
    // This page carried 102,061.24 until that record landed - what the account read
    // at 23:57 UTC that evening, while options still marked. The video was cut from
    // the earlier figure and says so.
    equity: 102_588.74,
  },
]

// The account this page is about, published so a judge can open it rather than
// take the figures on trust. It is what the README submits and what `/live` reads.
export const ACCOUNT = 'PA3BXFR0ZVYC'

// THE MARKET OVER THE SAME WINDOW. A return means nothing until the reader knows
// what the market did while it was earned: "+2.59%" is a number, "+2.59% against
// +0.76%" is a result. Taken from the same market data the agent reads, on the
// same feed, so the comparison can be checked from where it came from.
export const BENCHMARK = {
  name: 'SPY',
  percent: 0.76,
  window: 'open of 31 August to close of 3 September',
  sessions: 4,
}

// What the sessions themselves did, counted in the stand's own record over the
// JUDGED TRADING WEEK - the window the organiser measures, Monday to Friday, and
// the same window the profit beside it is earned in. The record holds a handful
// of rows from before that window; they are not counted here, because a number
// standing beside this week's profit has to describe this week.
//
// The pair is chosen for the RATIO between them, which is the whole argument: the
// agent woke 152 times and committed 16. A page showing only the trades describes
// an agent that always trades.
export const ACTIVITY: [string, string][] = [
  ['sessions it ran', '152'],
  ['intents it filed', '16'],
]
