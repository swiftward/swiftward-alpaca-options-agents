// What the account and the agent looked like, frozen.
//
// The landing does NOT call the API. `/live` does, and it is the page to open to
// see the account move; this one is a poster and has to render the same on a
// judge's screen whether or not the stand is up, whether or not the broker
// answers, and after the week is over. A landing that shows "the broker did not
// answer" where its own results should be has argued against itself.
//
// Every figure here was taken from the running account and the record behind it,
// not composed. Refreshed before submission - the date below says when, and a
// reader who wants the moving version has the link.
export const TAKEN_AT = '3 September 2026'

export const ACCOUNT = {
  opened: 100_000,
  equity: 102_335.6,
  // Kept apart from equity rather than worked out in the component: what the
  // account is worth and what it earned are two claims, and one of them is the
  // one being made.
  profit: 2_335.6,
  percent: 2.34,
}

// The week as it ran. Counted from the record the agent writes as it goes, which
// is the same record `/live` reads.
export const COUNTS: [string, string][] = [
  ['sessions run', '140'],
  ['intents filed', '13'],
  ['orders sent', '47'],
  ['orders filled', '9'],
]

// Six lines the agent wrote, in its own words, taken from the record unedited.
//
// Four of the six are refusals. That is not a selection to look modest: it is the
// proportion the week actually had, and a page that showed only the entries would
// be describing a different agent from the one that ran.
export const SAID: { at: string; text: string }[] = [
  {
    at: '3 Sep · 12:31',
    text: 'I took the task’s overrides with envelope `2026-08-31.1`; all six fresh candidates fell below the required +2 edge, so no trade.',
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
