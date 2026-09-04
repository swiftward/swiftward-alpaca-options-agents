import { ArrowDown, CircleSlash, Eye, EyeOff, FileCode, Layers } from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { Link } from 'react-router'

import { Boundary } from './Boundary'
import { Card, Chip, Eyebrow, Figure, Figures, inline, Panel } from './parts'
import { ACCOUNT, ACTIVITY, BENCHMARK, MEASUREMENTS, OPENED, type Measurement } from './snapshot'

// The landing page: what this is, how it differs, and where to go and look.
//
// Ordered for a reader who arrives and leaves, not for one who is walked through.
// What the session DOES comes first, because a reader who cannot see the model
// working reads everything after it as a backtest with a chat window attached.
// The limits come second and the measurements third.
//
// Every section carries a picture and about forty words. The version before this
// one was six paragraphs of a hundred words each and read as a report: the eye
// had nowhere to stop and nothing to look at. A payoff, three bars and a day on a
// line say at a glance what those paragraphs spent a screen on.
export function Landing() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-14">
      {/* THE FIRST SCREEN CARRIES THE CLAIM AND THE EVIDENCE SIDE BY SIDE.
          Before this the headline promised profit and the first number appeared
          seven screens down, in section 07 - a reader who left early left having
          been told, not shown. The settled figure now sits under the sentence it
          proves, and the four steps of a window sit beside it, so the screen
          argues in two directions at once instead of scrolling in one. */}
      <div className="grid items-start gap-x-12 gap-y-12 lg:grid-cols-12">
        <div className="lg:col-span-7">
          {/* The bar above already carries the name and the live dot. Repeating
              them here said the same thing twice and told a reader nothing; this
              line now says what the thing IS, which is what a reader arriving
              from a link actually needs first. */}
          <Eyebrow>[ Autonomous options agent · Alpaca paper account ]</Eyebrow>

          <h1 className="mt-6 max-w-[17ch] text-[46px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[58px]">
            It trades options for profit, and it is held to what it may lose.
          </h1>

          <p className="mt-6 max-w-[46ch] text-[20px] leading-[1.35] tracking-[-0.01em] text-secondary">
            A harness that puts intent, a model and policy on one line — so that what the agent
            means to do is written down, what it decides is its own, and{' '}
            <Mark>what it may lose is not its to change</Mark>.
          </p>

          <Actions />
        </div>

        <div className="lg:col-span-5">
          <Flow />
        </div>
      </div>

      {/* THE PLAQUE IS THE ACCOUNT. It used to hold four facts about the system -
          windows, names, limits, checks - which are all argued at length further
          down; on the first screen they asked the reader to take an interest
          before being given a reason to. What the account is worth, what it made,
          and how much deciding produced it is the reason.

          Equity and the profit come from the same array section 07 prints, and
          the profit is worked out from the opening balance, so the two places
          cannot drift apart. */}
      <div className="mt-14">
        <Figures spread>
          <Figure name="equity" value={money(SETTLED.equity as number)} />
          <Figure
            name={`P&L since the $${OPENED.toLocaleString('en-US')} start`}
            value={`${change((SETTLED.equity as number) - OPENED)} · ${(
              (((SETTLED.equity as number) - OPENED) / OPENED) *
              100
            ).toFixed(2)}%`}
            tone="gain"
          />
          {/* The market goes NEXT TO the profit, not in a footnote, because the
              two are one claim and reading either alone gets it wrong. */}
          <Figure
            name={`${BENCHMARK.name} over the same window`}
            value={`+${BENCHMARK.percent.toFixed(2)}%`}
          />
          {ACTIVITY.map(([name, value]) => (
            <Figure key={name} name={name} value={value} />
          ))}
        </Figures>

        {/* ONE CAPTION, not two loose blocks. What stood here was a full-width
            line of uppercase mono and a four-line paragraph under it, and neither
            belonged to anything: a reader met them after the numbers had already
            been read and had no reason to start.

            The account's id earns its place - it is what lets a judge open the
            account instead of trusting the figures - and so does the sample-size
            caveat, which is better said by us than raised by a judge. The claim
            about numbers recomputing came out: it is made in full, with the
            command that proves it, on the page for judges. */}
        <p className="mt-4 max-w-[86ch] text-[13px] leading-relaxed text-muted">
          <span className="font-mono text-secondary">{ACCOUNT}</span> · Alpaca paper ·{' '}
          {BENCHMARK.sessions} trading days, {BENCHMARK.window}. Four sessions cannot separate skill
          from a good draw; the evidence for the strategy is the 646 trading days of option prices
          committed to the repository.
        </p>
      </div>

      {/* THE THREE THINGS THIS PROJECT HAS THAT THE FIELD DOES NOT.
          What stood here was longer and softer - three paragraphs a reader skims
          and remembers none of. Each is now one fact with a number behind it,
          because a card is read in about four seconds and what is not read is not
          an argument. The long version of each is in the sections below and in
          the repository, which is where a reader who wants it goes.

          The limits are NOT one of the three, and that is deliberate: the page
          makes that claim four times before this block - in the headline, in the
          sentence under it, on the accented step of the diagram, and for the whole
          of section 02. What the page did not say anywhere was that a session
          reads and decides at all. In this field that is the rare half: the usual
          "agent" is deterministic gates around a single model call. */}
      <div className="mt-16 mb-20 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={FileCode}
          title="A session, not a pipeline"
          says="The screener prices the permitted field every few minutes. A session then reads its five playbooks and chooses: structure, strikes, size — or no trade at all."
        />
        <Claim
          icon={Layers}
          title="The test moves the real book"
          says="No replay, no simulation. Every price comes from the live broker with one number displaced. At zero displacement it matches the market to the cent."
        />
        <Claim
          icon={CircleSlash}
          title="The rule our data killed"
          says="Our defence closed a position when the strike was touched. Over 672 trades it lost to doing nothing. We published that and changed the rule."
        />
      </div>

      {/* SECTIONS 01 TO 04 ARE THE FOUR STEPS OF THE DIAGRAM ABOVE, in the order
          the diagram draws them. The first screen promises a shape; these prove it.
          Before this the sections were about the harness, the limits, the
          instrument and the research, which is a different list from the one the
          picture at the top had just made a reader memorise. */}

      <Block
        label="01 · intent"
        title="It says what it means to do, before it may do it."
        explains="A window wakes a session with a CAUSE and a task, never an answer. Nothing in the file says what to trade. What the session then writes down is filed before the order exists, and the order refers back to it."
      >
        <Card>
          <Day />
          <div className="mt-8 border-t border-line pt-6">
            <Yaml source={SCHEDULE} />
          </div>
          {/* WHAT THE FILE BUYS YOU, said under the file itself, and how the thing
              learns - which is the same sentence, because here they are the same
              mechanism.

              The word `self-improving` is deliberately absent. It promises a
              system that rewrites itself, and ours does not: a person edits the
              file. That is the ADVANTAGE, not the shortfall - a system that
              improves itself in a way nobody can point at cannot be taken apart
              after a bad day. Saying `learns` keeps the claim true. */}
          <Pull>Every change to how it trades is a diff. Every number in it can be recomputed.</Pull>
          <p className="mt-4 text-[15px] leading-relaxed text-secondary">
            Everywhere else that is a deploy. Here it is an edit — and that is also how it
            learns: whole rules are added, rewritten and deleted, not only numbers tuned, and the
            file records what each change was made on. The defence rule was deleted because 672
            trades said it lost to doing nothing.
          </p>
        </Card>
      </Block>

      <Block
        label="02 · the model"
        title="Six hundred priced by code. Six judged by the model."
        explains={
          <>
            The line is drawn on one question: <Mark>can the answer be wrong in an interesting
            way?</Mark> Applying one formula six hundred times cannot — that is arithmetic, and it
            is code. Whether today's news makes a rich structure a trap can, and that is the
            session's.
          </>
        }
      >
        <Card>
          <Funnel />
          <Pull>Give a model everything and it is a script with a random number generator in it.</Pull>
        </Card>

        <div className="mt-4">
          <Card>
            <Payoff />
            <p className="mt-7 text-[15px] leading-relaxed text-secondary">
              What it picks is always bounded: it sold the right to buy SPY at $772 and bought the
              right to buy at $773. That gap is the whole risk — $0.20 a spread comes in, and no
              move in the world costs more than the gap less the credit.
            </p>
            <Pull>
              A stop-loss is a hope: it fills where the market lets it. A bought leg is a contract.
            </Pull>
          </Card>
        </div>
      </Block>

      <Block
        label="03 · policy"
        title="It knows the wall is there. It does not know where."
        explains="Every rule carries how much of itself it discloses. Tell a session the size cap and it splits one order into four; tell it only that a rule exists, and there is nothing to route around. The service the order passes through is what refuses it, and the refusal names the rule."
      >
        <Card>
          <Envelope />
          <Pull>A prompt is a request. A service that holds the order is a wall.</Pull>
        </Card>
      </Block>

      <Block
        label="04 · the order"
        title="The price is walked. Every concession is re-priced first."
        explains="The session names the structure, the size and the worst price it will accept. From there code takes over: it walks the limit toward the book a cent at a time, and before every concession it re-computes what the structure would pay at the new price — refusing the concession if it falls below the entry threshold."
      >
        <Card>
          <Ladder />
          {/* The gap is NAMED here rather than left for a judge to find. A page that
              lists only the guards that worked is describing a different system. */}
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            Two of the three ceilings are re-checked here after the session has done its
            arithmetic. The third, the one that bounds everything betting the same way, is
            disclosed to the session and nothing in this code re-checks it — which is the gap
            3 September ran into, and it is written down rather than left to be found.
          </p>
          <Pull>
            A guard that can only cancel a resting order is an observation, not a limit.
          </Pull>
        </Card>
      </Block>

      <Block
        label="05 · the measurements"
        title="We deleted our own defence rule."
        explains={
          <>
            It closed a spread the moment price touched the sold strike, and it felt prudent for
            four months. Measured across 672 trades, <Mark>it loses to doing nothing</Mark>.
          </>
        }
      >
        <Card>
          <Exits />
          <p className="mt-7 text-[15px] leading-relaxed text-secondary">
            Price passed the sold strike in 42.7% of trades and only 26.6% ended breached. The rule
            was paying for 108 crossings that bought nothing.
          </p>
          <Pull>A measurement that only ever agrees with you is not a measurement.</Pull>
        </Card>

      </Block>

      <Block
        label="06 · the result"
        title="Two judges, two clocks, and both are named here."
        explains="One account, opened at $100,000 on the kickoff day and never reset. The result is taken twice by two measurements that do not agree on when the week ends, so both cut-offs are printed with the rule each of them uses."
      >
        <Card>
          <ul className="m-0 list-none space-y-4 p-0">
            {MEASUREMENTS.map((m) => (
              <Measured key={m.by} of={m} />
            ))}
          </ul>
        </Card>

        {/* The page ends by handing the reader off. Both figures above are frozen -
            they have to be, a poster cannot depend on a broker answering - and the
            honest next sentence is "the moving one is over here". */}
        <div className="mt-8 flex flex-wrap items-center gap-x-5 gap-y-3">
          <LiveLink />
          <span className="text-[15px] text-secondary">
            Both figures above are frozen. The account itself is not.
          </span>
        </div>
      </Block>
    </main>
  )
}

// An AMOUNT and a CHANGE are written differently, and confusing them is how a
// balance ends up wearing a plus sign it did not earn. What the account holds
// takes no sign; what it made since the kickoff takes one.
function money(amount: number) {
  return `$${amount.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`
}

function change(amount: number) {
  return `${amount < 0 ? '-' : '+'}${money(Math.abs(amount))}`
}

// ============================================================
// The pictures.
//
// Hand-authored rather than the chart library. ../CLAUDE.md sends charts to
// recharts, and the reason it gives is axes, ticks and hitting the nearest point
// with a mouse - a data chart the reader interrogates. None of these is that:
// they are fixed diagrams of numbers that do not move, with nothing to hover and
// no scale to parse. Each takes its colour from the page's roles, so none of them
// carries an opinion about which theme it is in.
// ============================================================

// The four things that stand between a window opening and an order existing, and
// what each of them answers. Drawn rather than listed because the argument is the
// ORDER of them: the model decides in the middle, with something written down
// before it and something it cannot reach after it. A list of four bullets says
// the same words and loses the sequence, which is the whole claim.
function Flow() {
  // Two of the four rows carry a mark, and they answer the two questions a reader
  // actually arrives with. `decides` is dark because that step is the model and
  // nothing else - the page is read by people asking where the AI is. `holds`
  // carries the accent because it is the step nobody else has: the order can be
  // refused after the model has chosen it, and the session cannot argue.
  const steps: [string, string, string, 'strong' | 'accent' | undefined][] = [
    ['intent', 'what it means to do, and why', 'written first', undefined],
    ['the model', 'reads the book, picks the structure, sizes it', 'decides', 'strong'],
    ['policy', 'refuses what breaks the envelope', 'holds', 'accent'],
    ['the order', 'walked to the book, or cancelled on patience', 'sent', undefined],
  ]

  return (
    <figure className="m-0">
      <div className="rounded-xl border border-line bg-surface-raised p-5">
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          one window, from waking to an order
        </p>
        <ul className="m-0 mt-4 list-none p-0">
          {steps.map(([name, does, state, tone], index) => (
            <li key={name} className={index === 0 ? 'py-3' : 'border-t border-line py-3'}>
              <div className="flex items-center justify-between gap-3">
                <span className="flex items-baseline gap-2.5">
                  <span className="font-mono text-[12px] text-muted tabular-nums">
                    0{index + 1}
                  </span>
                  <span className="text-[16px] font-medium text-primary">{name}</span>
                </span>
                <Chip tone={tone}>{state}</Chip>
              </div>
              <p className="m-0 mt-1 pl-[30px] text-[14px] leading-snug text-secondary">{does}</p>
            </li>
          ))}
        </ul>
      </div>
      <div className="flex justify-center py-2.5 text-muted" aria-hidden>
        <ArrowDown className="size-5" />
      </div>
      <div className="rounded-xl border border-line bg-surface-raised px-5 py-4">
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-gain">
          what the account keeps
        </p>
        <p className="mt-1.5 text-[17px] font-medium leading-snug text-primary">
          The credit, less what the loss was allowed to be
        </p>
      </div>
    </figure>
  )
}

// The trading day and what the file puts in it. Three entries open the account;
// what keeps it repeats too often to draw and is said in a line instead.
type Window = {
  hour?: number
  name: string
  closes?: boolean
  rule: string
  said: { at: string; text: string }
  // The intent as the record holds it, and ONLY where one was actually filed.
  // Two of the five windows below have one, and they are exactly the two where an
  // order was sent - which is the section's whole claim, shown rather than said:
  // no intent, no order.
  filed?: { structure: string; worst: string }
}

// One trading day, and what each window is FOR - taken from the declaration's own
// `cause` field, word for word - beside what the session actually said in it,
// taken from the record on the stand, unedited.
//
// The marks are buttons rather than a hover: a tooltip does not exist on a phone,
// and half the readers of this page are on one. Choosing a window is also how a
// keyboard gets at the same thing.
//
// THE LINES ARE CHOSEN FOR WHAT THEY SHOW, NOT FOR FALLING ON ONE DAY. The axis is
// a day as the file declares it; each line carries its own date, and they come
// from three different ones. An earlier set was four refusals in a row, which read
// as an agent that never does anything - and where a line had not been looked for
// at all it said "nothing in the record", which reads as a broken page. Three of
// the five below are the session ACTING: two orders sent and one position bought
// back.
//
// A line is attached only where its timestamp falls inside that window. Putting a
// line under the wrong session would be inventing a quotation.
// The four that sit on the axis: each happens once, at a declared hour.
type Timed = Window & { hour: number }

const WINDOWS: Timed[] = [
  {
    hour: 10.33,
    name: 'entry',
    rule: 'morning entry window, taken only when premium is dear',
    said: {
      at: '1 September, 10:20',
      text: 'Used task thresholds (13% credit-to-risk, +3 fresh edge, one underlying) under envelope `2026-08-31.1`. Submitted 123 QQQ Sep 4 717/718 call spreads at −0.29 credit, worst −0.26.',
    },
    filed: {
      structure:
        'Sell 123 QQQ260904C00717000 (717 calls) and buy 123 QQQ260904C00718000 (718 calls), same-expiry call credit spread; seek 0.29 credit and accept no worse than 0.26 credit.',
      worst:
        '$9,102 at a 0.26 credit (74 points risk x 123 contracts x $100), 8.95% of 101719.3 equity; sized to nine-tenths of the envelope’s 10% position maximum under ruleset `2026-08-31.1`.',
    },
  },
  {
    hour: 12.5,
    name: 'entry',
    rule: 'midday entry window: a position taken here lives half a session',
    said: {
      at: '1 September, 12:30',
      text: 'The screener is fresh. The top three candidates by listed edge — therefore the only three I will re-price this turn — are SPY Sep 8 774/775 calls (+3.07), QQQ Sep 2 714/715 calls (+2.33) and IWM Sep 4 295/296 calls (−0.07).',
    },
  },
  {
    hour: 14.33,
    name: 'entry',
    rule: 'the main entry window of the day',
    said: {
      at: '31 August, 14:20',
      text: 'I took the task’s 0.30 delta cap, +3 fresh-edge rule, borrowed-edge >0 rule, and 2% fuse; the envelope governed. After QQQ and SPY failed fresh tests, I submitted 40 META 570/567.5 put spreads at 0.37 credit, worst 0.29; accepted loss $8,840.',
    },
    filed: {
      structure:
        'Sell 40 META260831P00570000 (570 puts) and buy 40 META260831P00567500 (567.5 puts), same-day vertical credit spread; seek 0.37 credit, accept no worse than 0.29 credit.',
      worst:
        '$8,840 at a 0.29 credit (calculated: $221 x 40 contracts; 2.5-point width less $0.29 credit), 8.79% of 100652.95 equity; 90% sizing ceiling is 9% and envelope position maximum is 10% under ruleset `2026-08-31.1`.',
    },
  },
  {
    hour: 15.67,
    name: 'flatten',
    closes: true,
    rule: 'close what must not be held overnight',
    said: {
      at: '2 September, 15:40',
      text: 'The chain spacing is 2.5 points, so the half-gap threshold is 1.25; SMH `p=549.67` is 5.33 below the 555 short strike. It is not near assignment, so I’m leaving it to expire.',
    },
  },
]

// EVERYTHING THAT IS NOT A TIME OF DAY. The file declares thirteen windows; four
// of them sit on the axis above because they happen once at a fixed hour, and
// these five do not - three run on a cadence all day, two only on their own day of
// the week. They were missing entirely, and with them the picture was claiming a
// trading day is three entries and a close.
//
// A row of pills rather than dashed bands stacked under the axis: five bands would
// be five lines of furniture, and a pill is a control a reader already knows how
// to read.
const OTHERS: Window[] = [
  {
    name: 'news · every half hour',
    rule: 'news check on the underlyings we hold',
    said: {
      at: '1 September, 09:35',
      text: 'Relevant news: oil/U.S.–Iran risk, higher yields, and rising September Fed-hike expectations affected SPY/QQQ; no IWM-specific item. SPY alone is held; at 762.305 it remains 19.305 from the 743 short strike versus the 7.905 trigger threshold. No wake-up or order.',
    },
  },
  {
    name: 'defence · every half hour',
    rule: 'the defence rules',
    said: {
      at: '31 August, 11:09',
      text: 'Closing a structure that has given back its credit: buy-back at 0.10 against 0.36 taken, which is 27.8% of the credit given up against the 35% the numbers allow.',
    },
  },
  {
    name: 'convexity · every two hours',
    rule: 'convexity layer: a cheap bet on a large move, bounded and known',
    said: {
      at: '31 August, 13:51',
      text: 'I took the task’s 1.5/2.5-sigma placement rules, 2% loss caps and 10% width round-trip cap; the envelope governed. Submitted 2 SPY Sep 2 743/734 put-backspread sets at 0.04 debit, worst 0.05; accepted loss $1,810.',
    },
  },
  {
    name: 'earnings · Wednesday',
    rule: 'the earnings window: if a report lands after the close tonight, sell what the unknown has inflated',
    said: {
      at: '2 September, 15:20',
      text: 'The measurement is nearly neutral: fresh AVGO 4 September 370-strike mids imply about 8.09%, against the task’s 8.0% median realized move. The resulting ratio is between the task’s 0.8 buy and 1.3 sell thresholds.',
    },
  },
  {
    name: 'event bet · Thursday',
    rule: 'the employment number lands in the morning: buy the gap tonight',
    said: {
      at: '3 September, 15:30',
      text: 'SPY was 773.435; account equity $102,335.60 and positions were empty. Submitted and accepted: 8 Sep-4 773 straddles at $4.00 debit, max loss $3,200; and 174 Sep-4 762P/785C strangles at $0.11 debit, max loss $1,914. Total worst case $5,114, or 4.999% of equity.',
    },
  },
]


function Day() {
  // The morning entry opens selected because it is the one window in the day with
  // a filled trade behind it, and an empty state here would waste the picture.
  const [chosen, setChosen] = useState<Window>(WINDOWS[0])
  const at = (hour: number) => ((hour - 9.5) / 6.5) * 100

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        a trading day as the file declares it — choose a window
      </figcaption>

      {/* The day scrolls in its own box on a narrow screen rather than pushing the
          card sideways: at 330px the last label sits past the right edge, and a
          page that scrolls horizontally as a whole is worse than a strip that
          does. */}
      <div className="overflow-x-auto pb-1">
        <div className="min-w-[420px]">
          <div className="relative mt-9 h-px bg-line-strong">
            {WINDOWS.map((window) => {
              const on = window === chosen
              return (
                <button
                  key={window.hour}
                  type="button"
                  onClick={() => setChosen(window)}
                  aria-pressed={on}
                  className="absolute top-0 -translate-x-1/2 cursor-pointer"
                  style={{ left: `${at(window.hour)}%` }}
                >
                  <span
                    className={`mx-auto block rounded-full transition-all ${
                      on ? 'size-3.5 ring-4' : 'size-2.5'
                    } ${
                      window.closes
                        ? 'bg-accent-ink ring-accent-ink/15'
                        : 'bg-accent ring-accent/20'
                    }`}
                    style={{ marginTop: on ? '-7px' : '-5px' }}
                  />
                  <span
                    className={`mt-3 block whitespace-nowrap font-mono text-[11px] transition-colors ${
                      on ? 'text-primary' : 'text-muted hover:text-secondary'
                    }`}
                  >
                    {window.name}
                  </span>
                </button>
              )
            })}
          </div>

          <div className="mt-11 flex justify-between font-mono text-[11px] text-muted">
            <span>09:30</span>
            <span>16:00</span>
          </div>

          {/* Pills, not bare text on a line. As plain text the one that used to be
              here was indistinguishable from the axis captions around it and
              nobody would guess it could be chosen. */}
          <div className="mt-8 border-t border-line pt-5">
            <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              and underneath, or on their own day
            </p>
            <div className="mt-3 flex flex-wrap gap-2">
              {OTHERS.map((window) => {
                const on = window === chosen
                return (
                  <button
                    key={window.name}
                    type="button"
                    onClick={() => setChosen(window)}
                    aria-pressed={on}
                    className={`cursor-pointer whitespace-nowrap rounded-full border px-3 py-1.5 font-mono text-[11px] transition-colors ${
                      on
                        ? 'border-accent bg-accent text-on-accent'
                        : 'border-line bg-surface-raised text-muted hover:border-line-strong hover:text-primary'
                    }`}
                  >
                    {window.name}
                  </button>
                )
              })}
            </div>
          </div>

          <p className="mt-5 text-[13px] leading-relaxed text-muted">
            Thirteen windows in one file. The session sets its own wake-ups on top of them — on a
            clock, or on a price it wants to hear about.
          </p>
        </div>
      </div>

      {/* What the chosen window is for, and what it once did. The box keeps a
          floor under its height so choosing another window does not make the page
          jump under the reader's hand. */}
      <div
        className={`mt-7 min-h-[164px] rounded-lg border border-l-[3px] border-line px-5 py-4 ${
          chosen.closes ? 'border-l-accent-ink' : 'border-l-accent'
        }`}
      >
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          what the file asks for
        </p>
        <p className="mt-2 text-[17px] leading-snug text-primary">{chosen.rule}</p>

        <p className="mt-5 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          what it said · {chosen.said.at}
        </p>
        <p className="mt-2 text-[15px] leading-relaxed text-secondary">
          {inline(chosen.said.text)}
        </p>

        {chosen.filed ? (
          <div className="mt-6 border-t border-line pt-5">
            <p className="flex flex-wrap items-center justify-between gap-3 font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
              and this is what it filed before the order existed
              <Chip tone="accent">envelope read</Chip>
            </p>
            <dl className="m-0 mt-4 space-y-3">
              <div className="flex flex-col gap-1 sm:flex-row sm:gap-5">
                <dt className="shrink-0 font-mono text-[12px] text-muted sm:w-[86px]">structure</dt>
                <dd className="m-0 text-[14px] leading-relaxed text-primary">
                  {chosen.filed.structure}
                </dd>
              </div>
              <div className="flex flex-col gap-1 sm:flex-row sm:gap-5">
                <dt className="shrink-0 font-mono text-[12px] text-muted sm:w-[86px]">worst case</dt>
                <dd className="m-0 text-[14px] leading-relaxed text-primary">
                  {inline(chosen.filed.worst)}
                </dd>
              </div>
            </dl>
          </div>
        ) : null}
      </div>

    </figure>
  )
}

// What comes back when the session asks what it may do. The picture exists for
// its last row: a rule that admits it is there and withholds its number.
// A tiny YAML highlighter for the ONE sample this page quotes.
//
// Not a library. The same trade the markdown in the agent's words was weighed
// against and lost: a real highlighter is around a hundred kilobytes to colour
// four kinds of token in twelve lines, and it would carry grammars for languages
// this page will never show. This handles exactly the shapes the sample has - a
// list dash, a key, a quoted string, a bracketed list, the block scalar `|` and
// the indented prose under it - and anything it does not recognise stays the
// colour the text already was.
//
// There is no injection risk: React nodes are built, never a markup string.
function Yaml({ source }: { source: string }) {
  return (
    <Panel title="alpaca.yaml">
      <pre className="m-0 font-mono text-[13px] leading-relaxed text-code-fg">
        {source.split('\n').map((line, index) => (
          <span key={index} className="block">
            {colour(line)}
            {'\n'}
          </span>
        ))}
      </pre>
    </Panel>
  )
}

// One line at a time. Prose under `task: |` is indented four spaces or more and is
// left as prose - it is English, and colouring it as code would say it is not.
function colour(line: string) {
  const key = /^(\s*)(-\s)?([a-z_]+)(:)(.*)$/.exec(line)
  if (!key || line.startsWith('    ')) {
    return <span className="text-code-fg">{line}</span>
  }

  const [, indent, dash, name, colon, rest] = key

  return (
    <>
      {indent}
      {dash ? <span className="text-code-punct">{dash}</span> : null}
      <span className="text-code-key">{name}</span>
      <span className="text-code-punct">{colon}</span>
      {value(rest)}
    </>
  )
}

// What follows the colon: a quoted string, a bracketed list, a duration or clock
// time, the block marker, or nothing it knows.
function value(rest: string) {
  const string = /^(\s*)(".*")$/.exec(rest)
  if (string) {
    return (
      <>
        {string[1]}
        <span className="text-code-string">{string[2]}</span>
      </>
    )
  }

  const list = /^(\s*)(\[)(.*)(\])$/.exec(rest)
  if (list) {
    return (
      <>
        {list[1]}
        <span className="text-code-punct">{list[2]}</span>
        <span className="text-code-fg">{list[3]}</span>
        <span className="text-code-punct">{list[4]}</span>
      </>
    )
  }

  const number = /^(\s*)(\d+[a-z]?)$/.exec(rest)
  if (number) {
    return (
      <>
        {number[1]}
        <span className="text-code-number">{number[2]}</span>
      </>
    )
  }

  if (rest.trim() === '|') {
    return (
      <>
        {' '}
        <span className="text-code-punct">|</span>
      </>
    )
  }

  return <span className="text-code-fg">{rest}</span>
}

function Funnel() {
  const stages: [string, string, number, boolean][] = [
    ['priced by code, ranked by one measure', '~600', 100, false],
    ['put in front of the session', '6', 12, true],
    ['taken', '0 or 1', 3, true],
  ]

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        one pass, every few minutes
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-4 p-0">
        {stages.map(([name, count, width, judged]) => (
          <li key={name}>
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <span className="text-[15px] text-secondary">{name}</span>
              <span className="font-mono text-[15px] tabular-nums text-primary">{count}</span>
            </div>
            <div className="mt-2 h-2 w-full rounded-full bg-surface-sunk">
              <div
                className={`h-2 rounded-full ${judged ? 'bg-accent' : 'bg-accent-ink'}`}
                style={{ width: `${width}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </figure>
  )
}

// The limit walked toward the book, and the two things that stop it. The floor is
// the price the SESSION named; past it the order is cancelled rather than conceded,
// which is the difference between patience and desperation.
function Ladder() {
  const steps: [string, string][] = [
    ['0.79', 'placed at the price the session asked for'],
    ['0.78', 'one cent conceded, re-priced first'],
    ['0.77', 'one cent conceded, re-priced first'],
    ['0.76', 'the worst the session named'],
  ]

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        the limit walked toward the book
      </figcaption>
      <ol className="m-0 mt-5 list-none space-y-1 p-0">
        {steps.map(([price, what], index) => {
          const floor = index === steps.length - 1
          return (
            <li
              key={price}
              className={`flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1 rounded-lg border px-4 py-3 ${
                floor
                  ? 'border-accent bg-accent text-on-accent'
                  : 'border-line bg-surface-raised'
              }`}
            >
              <span className="flex items-baseline gap-3">
                <span
                  className={`font-mono text-[15px] tabular-nums ${
                    floor ? 'font-medium' : 'text-primary'
                  }`}
                >
                  {price}
                </span>
                <span className={`text-[14px] ${floor ? '' : 'text-secondary'}`}>{what}</span>
              </span>
              {floor ? (
                <span className="rounded-full border border-on-accent/40 px-2.5 py-0.5 font-mono text-[11px] uppercase tracking-[0.04em]">
                  floor
                </span>
              ) : null}
            </li>
          )
        })}
      </ol>
      <p className="mt-4 text-[15px] leading-relaxed text-secondary">
        Below the floor there is no next step: what the book will not take is{' '}
        <Mark>cancelled, not conceded</Mark>.
      </p>
    </figure>
  )
}

function Envelope() {
  const rules: [string, string, boolean][] = [
    ['per-position', '10 percent of equity', true],
    ['one side of the market', '35 percent of equity', true],
    ['whole portfolio', '80 percent of equity', true],
    ['how often it may open', 'exists · withheld', false],
  ]

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what read_envelope answers, in every turn that acts
      </figcaption>
      <ul className="m-0 mt-5 list-none space-y-1 p-0">
        {rules.map(([rule, value, disclosed]) => (
          <li
            key={rule}
            className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 rounded-lg border border-line bg-surface-raised px-4 py-3"
          >
            <span className="flex items-center gap-3 font-mono text-[13px] text-secondary">
              {disclosed ? (
                <Eye className="size-4 shrink-0 text-muted" aria-hidden />
              ) : (
                <EyeOff className="size-4 shrink-0 text-muted" aria-hidden />
              )}
              {rule}
            </span>
            <span
              className={`font-mono text-[13px] tabular-nums ${
                disclosed ? 'text-primary' : 'text-muted'
              }`}
            >
              {value}
            </span>
          </li>
        ))}
      </ul>
    </figure>
  )
}

// The payoff of the spread the section describes. Level at the credit below the
// sold strike, level at the loss above the bought one, and a slope between. A
// reader with no options background sees in one look that BOTH ends are flat -
// which is what defined risk means and what the prose took five sentences to say.
function Payoff() {
  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what the position is worth at expiry · 117 spreads
      </figcaption>
      <svg
        viewBox="0 0 640 200"
        className="mt-5 w-full text-line-strong"
        role="img"
        aria-label="The payoff of the spread: it keeps 2,340 dollars while SPY finishes below 772, falls between 772 and 773, and loses 9,360 dollars above 773."
      >
        <line x1="0" y1="60" x2="640" y2="60" stroke="currentColor" strokeDasharray="3 4" />
        <line x1="240" y1="16" x2="240" y2="184" stroke="currentColor" strokeWidth="1" />
        <line x1="400" y1="16" x2="400" y2="184" stroke="currentColor" strokeWidth="1" />
        <polyline
          points="0,34 240,34 400,172 640,172"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          className="text-primary"
        />
      </svg>
      <div className="mt-4 flex flex-wrap justify-between gap-x-6 gap-y-1 font-mono text-[11px] text-muted">
        <span className="text-gain">keeps $2,340</span>
        <span>772 · sold</span>
        <span>773 · bought</span>
        <span className="text-loss">loses $9,360</span>
      </div>
    </figure>
  )
}

// The three exits on the 672 trades the history holds. Bars rather than a table:
// the whole point is that the middle one is SHORTER than doing nothing, and a
// column of dollar signs makes the reader work that out for himself.
function Exits() {
  const most = 3.46

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        what a trade paid · 672 trades, january 2024 to august 2026
      </figcaption>
      <ul className="m-0 mt-6 list-none space-y-5 p-0">
        {EXITS.map(([rule, value, removed]) => (
          <li key={rule}>
            <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-1">
              <span className={`text-[15px] ${removed ? 'text-primary' : 'text-secondary'}`}>
                {rule}
                {removed ? (
                  <span className="ml-3 font-mono text-[13px] text-loss">← removed</span>
                ) : null}
              </span>
              <span className="font-mono text-[15px] tabular-nums text-primary">
                ${value.toFixed(2)}
              </span>
            </div>
            <div className="mt-2 h-2 w-full rounded-full bg-surface-sunk">
              <div
                className={`h-2 rounded-full ${removed ? 'bg-loss' : 'bg-accent-ink'}`}
                style={{ width: `${(value / most) * 100}%` }}
              />
            </div>
          </li>
        ))}
      </ul>
    </figure>
  )
}

const EXITS: [string, number, boolean][] = [
  ['hold to expiry', 2.94, false],
  ['close when the sold strike is touched', 2.32, true],
  ['close a full width past the sold strike', 3.46, false],
]


// A faithful excerpt of the submitted declaration, not a paraphrase of it. The
// sample here once quoted the DEFENCE window - a rule about counting legs, which
// is housekeeping - and it said `every: 15m` where the file says 30m, with a cause
// of its own invention. This is an ENTRY window instead, because it carries the
// section's claim in the file's own words: even the condition for taking a trade
// is a judgement to be made, not a number to be met. The `...` marks where the
// task is cut; nothing inside the quoted lines is altered.
const SCHEDULE = `- name: entry-morning
  cause: "morning entry window, taken only when premium is dear"
  at: "10:20"
  within: 45m
  days: [mon, tue, wed, thu]
  task: |
    ...
    A morning entry is not taken every day: a whole day of
    movement lies ahead, so the premium has to be dear by a
    measure visible in the chain itself.`


// ============================================================
// The page's own parts.
// ============================================================

// The landing's section, which is not the live page's section.
//
// `Section` in parts.tsx titles with an Eyebrow - monospace, 12px, muted - and
// that is right where a title labels a panel of numbers the reader is already
// looking at. On a page somebody is scanning, a 12px caption is not a heading:
// the eye finds nothing to stop at and the page reads as one grey column.
//
// This is deliberately NOT a variant of that part. The two are different things
// wearing the same word: a data panel takes a caption, an argument takes a
// heading. Giving one component both jobs is how it ends up with a property for
// each.
function Block({
  label,
  title,
  explains,
  children,
}: {
  label: string
  title: string
  explains?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="mb-20">
      <Eyebrow>{label}</Eyebrow>
      <h2 className="mt-3 max-w-[24ch] text-[32px] font-medium leading-[1.1] tracking-[-0.02em] text-primary sm:text-[38px]">
        {title}
      </h2>
      {explains ? (
        <p className="mt-4 max-w-[68ch] text-[17px] leading-[1.55] text-secondary">{explains}</p>
      ) : null}
      <div className="mt-7">
        <Boundary says="this section failed">{children}</Boundary>
      </div>
    </section>
  )
}

// One measurement: who takes it, when, by what rule, and the number if that
// moment has passed. A cut-off still ahead shows a dash rather than a figure -
// the alternative is a number the reader cannot tell from a settled one.
// The measurement that is IN. One place picks it, so the plaque on the first
// screen and the section at the foot cannot disagree, and when the second
// measurement lands nothing here needs editing.
const SETTLED = MEASUREMENTS.filter((m) => m.equity !== null).slice(-1)[0] ?? MEASUREMENTS[0]

// TWO buttons, not four. The bar above already carries GitHub, and a hero that
// repeats a link the reader can see six inches higher is spending its most
// valuable row on nothing. The deck is not written yet, and a button for a page
// that does not exist is worse than no button.
//
// The first is filled because it is the one thing a judge cannot get from any
// other page: the account moving while they watch. The second is outlined
// because it orients rather than persuades.
// Used at the top of the page and again at the foot of it, so it is written once.
function LiveLink() {
  return (
    <Link
      to="/live"
      className="inline-flex items-center gap-2 rounded-lg bg-accent px-5 py-2.5 text-[15px] font-medium text-on-accent transition-opacity hover:opacity-90"
    >
      <span
        className="inline-block size-1.5 rounded-full bg-on-accent motion-safe:animate-pulse"
        aria-hidden
      />
      See it live
    </Link>
  )
}

function Actions() {
  return (
    <div className="mt-9 flex flex-wrap items-center gap-3">
      <LiveLink />
      <Link
        to="/submission"
        className="inline-flex items-center rounded-lg border border-line-strong px-5 py-2.5 text-[15px] font-medium text-primary transition-colors hover:bg-surface-sunk"
      >
        For judges
      </Link>
    </div>
  )
}

function Measured({ of }: { of: Measurement }) {
  const settled = of.equity !== null
  const profit = settled ? (of.equity as number) - OPENED : 0

  return (
    <li className="rounded-lg border border-line bg-surface-raised px-5 py-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <span className="text-[17px] font-medium text-primary">{of.by}</span>
          <span className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
            {of.when}
          </span>
        </span>
        <Chip tone={settled ? 'gain' : undefined}>{settled ? 'settled' : 'not yet taken'}</Chip>
      </div>

      {settled ? (
        <p className="mt-3 flex flex-wrap items-baseline gap-x-4 gap-y-1">
          <span className="text-[32px] font-medium leading-none tracking-[-0.02em] text-primary tabular-nums">
            {money(of.equity as number)}
          </span>
          <span className="text-[17px] font-medium tabular-nums text-gain">
            {change(profit)} · {((profit / OPENED) * 100).toFixed(2)}%
          </span>
        </p>
      ) : (
        <p className="mt-3 text-[32px] font-medium leading-none text-muted">&mdash;</p>
      )}

      <p className="mt-3 text-[15px] leading-relaxed text-secondary">{of.rule}</p>
    </li>
  )
}

// The line a section is remembered by, at the end of the card that earned it.
// A marker pen, and the only thing on the page drawn in Alpaca's yellow. One
// meaning: THIS IS THE SENTENCE TO TAKE AWAY. It is a ground with dark ink on it
// and never text in the yellow itself - measured, black on it is 13.95:1 and the
// yellow on this page's own background is 1.41:1, which no reader can see.
//
// Four of these on the whole page. A fifth would make it a colour scheme rather
// than an emphasis, and nothing marked is emphasised.
function Mark({ children }: { children: ReactNode }) {
  return (
    <mark className="rounded-[3px] bg-mark px-1.5 py-0.5 text-mark-ink decoration-clone">
      {children}
    </mark>
  )
}

function Pull({ children }: { children: ReactNode }) {
  return (
    <p className="mt-7 border-l-[3px] border-mark pl-4 text-[19px] font-medium leading-[1.4] tracking-[-0.01em] text-primary">
      {children}
    </p>
  )
}

function Claim({ icon: Icon, title, says }: { icon: typeof Eye; title: string; says: string }) {
  return (
    <Card>
      <Icon className="size-5 text-accent" aria-hidden />
      <h2 className="mt-4 text-[22px] font-medium leading-tight tracking-[-0.01em] text-primary">
        {title}
      </h2>
      <p className="mt-2.5 text-[15px] leading-relaxed text-secondary">{says}</p>
    </Card>
  )
}
