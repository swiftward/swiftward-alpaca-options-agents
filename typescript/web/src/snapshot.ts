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

// THE RESULT IS MEASURED TWICE, by two judges whose clocks differ, and both are
// named here because a page that showed one number would be answering a question
// nobody asked. Neither cancels the other.
//
// The rules are quoted rather than paraphrased. Alpaca's is from the organiser's
// written FAQ; LabLab's is their admin's answer in Discord on 26 August, and both
// are kept in full in the team's own `docs/rules.md`.
export type Measurement = {
  by: string
  when: string
  rule: string
  // null until the cut-off passes. A number typed in ahead of it would be a guess
  // wearing the clothes of a result.
  equity: number | null
}

export const MEASUREMENTS: Measurement[] = [
  {
    by: 'Alpaca',
    when: 'end of Thursday, 3 September',
    rule: 'Total account equity, not cash balance. Open positions enter it at their mark.',
    // The account stood at 102,335.60 at Wednesday's close and this page carried
    // that figure until the Thursday one was in.
    equity: 102_061.24,
  },
  {
    by: 'LabLab',
    when: 'Friday, 4 September · 11:00 New York',
    rule: 'P&L as of the moment submissions close. Trading after it does not count at all.',
    // One line to fill in when the cut-off passes. The event bet the agent bought
    // on Thursday evening is sold at 09:35 that morning, so what this measurement
    // sees is settled before it is taken.
    equity: null,
  },
]

// The account this page is about, published so a judge can open it rather than
// take the figures on trust. It is what the README submits and what `/live` reads.
export const ACCOUNT = 'PA3BXFR0ZVYC'

// THE MARKET OVER THE SAME WINDOW. A return means nothing until the reader knows
// what the market did while it was earned: "+2.06%" is a number, "+2.06% against
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

// The week as it ran on the account above, counted from the BROKER's own order
// list rather than from our record - that is the list a judge can open beside this
// page, and the two have to agree. Sessions and intents are not here for the same
// reason: this account's agent keeps its record where it runs, and a number we
// cannot point at does not go on a poster.
export const COUNTS: [string, string][] = [
  ['orders sent', '25'],
  ['orders filled', '4'],
  ['days it traded', '3'],
  ['positions still open', '4'],
]

// Six lines the agent wrote, in its own words, taken from the record unedited.
//
// Three of the six are it deciding not to trade. That is not a selection made to
// look modest: it is the proportion the week had, and a page showing only the
// entries would be describing a different agent from the one that ran.
export const SAID: { at: string; text: string }[] = [
  {
    at: '3 Sep · 14:22',
    text: 'I took the task’s overrides with envelope `2026-08-31.1`; all six fresh candidates were below +2 edge, so no intent or order was filed.',
  },
  {
    at: '3 Sep · 10:22',
    text: 'Used the task’s 13% credit/risk and +2 fresh-edge floors: no trade — SPY paid 26.58%/−1.16 (calls) and 5.26%/−9.14 (puts), QQQ 1.01%/−4.86 (calls) and 5.26%/−3.97 (puts), and BRK.B 10.50% with no measurable edge.',
  },
  {
    at: '2 Sep · 15:52',
    text: 'The layer is flat, and the 3 September horizon leaves no expiry compatible with the playbook’s required 2–5 trading days, so no structure and no order.',
  },
  {
    at: '2 Sep · 10:25',
    text: 'Submitted 21 SMH 552.5/557.5 call spreads at 0.79 credit, worst 0.76; accepted loss $8,841 against $101,714 equity.',
  },
  {
    at: '1 Sep · 10:22',
    text: 'The gateway refused the intent because this continuation exposes cause `execution`, not `entry-morning`. I’m correcting only that cause field; the price, size and risk are unchanged.',
  },
  {
    at: '31 Aug · 11:09',
    text: 'Closing a structure that has given back its credit: buy-back at 0.10 against 0.36 taken, which is 27.8% of the credit given up against the 35% the numbers allow.',
  },
]
