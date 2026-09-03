import { ArrowRight, Eye, EyeOff, FileCode, Ruler } from 'lucide-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'

import { Boundary } from './Boundary'
import { Card, Chip, Eyebrow, Figure, Figures } from './parts'

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
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-20">
      <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>

      <h1 className="mt-6 max-w-[17ch] text-[56px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[72px]">
        A model decides every trade. A file decides when it is asked.
      </h1>

      <p className="mt-7 max-w-[52ch] text-[22px] font-medium leading-[1.25] tracking-[-0.01em] text-secondary">
        Thirteen windows, written in plain words, say when a session wakes and what it is asked. It
        reaches for the playbooks it needs, reads 284 names, and either trades or says why it will
        not.
      </p>

      <div className="mt-10 flex flex-wrap items-center gap-4">
        <Link
          to="/live"
          className="inline-flex items-center gap-2 rounded-md bg-accent-ink px-5 py-3 font-mono text-sm uppercase tracking-[0.02em] text-inverse transition-opacity hover:opacity-90"
        >
          Watch it trade
          <ArrowRight className="size-4" aria-hidden />
        </Link>
        <span className="font-mono text-xs uppercase tracking-[0.04em] text-muted">
          updates every 15 seconds
        </span>
      </div>

      {/* Four numbers in the order the page argues them: what the session does,
          what holds it, what its numbers rest on. */}
      <div className="mt-14">
        <Figures>
          <Figure name="windows the file declares" value="13" />
          <Figure name="names read on every pass" value="284" />
          <Figure name="risk limits in the prompt" value="0" />
          <Figure name="published numbers that recompute" value="25 / 25" />
        </Figures>
      </div>

      <div className="mt-16 mb-20 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={FileCode}
          title="Written, not coded"
          says="A declaration and five playbooks, all of it prose the session reads as it acts. Changing what the agent does is editing a file."
        />
        <Claim
          icon={Ruler}
          title="Limits it cannot reach"
          says="Nothing about risk is in its instructions. An order is refused before it reaches the broker, and there is nothing in the session's reach to edit."
        />
        <Claim
          icon={Eye}
          title="Reasoning you can read"
          says="What woke it, what it asked the broker, what it concluded — in its own words. Including the runs that priced six candidates and took none."
        />
      </div>

      <Block
        label="01 · the harness"
        title="The file is the agent."
        explains="A window says when a session starts and what it is asked. It never says what to trade. We edited one while the market was open and the next session read the new rule — no restart, no deploy."
      >
        <Card>
          <Day />
          <pre className="mt-8 overflow-x-auto border-t border-line pt-6 font-mono text-[13px] leading-relaxed text-secondary">
            {SCHEDULE}
          </pre>
        </Card>
      </Block>

      <Block
        label="02 · the limits"
        title="It knows the wall is there. It does not know where."
        explains="Every rule carries how much of itself it discloses. Tell a session the size cap and it splits one order into four; tell it only that a rule exists, and there is nothing to route around."
      >
        <Card>
          <Envelope />
          <Pull>A prompt is a request. A service that holds the order is a wall.</Pull>
        </Card>
      </Block>

      <Block
        label="03 · what it trades"
        title="The worst case is arithmetic, and it is known before the order goes out."
        explains="It sold the right to buy SPY at $772 and bought the right to buy at $773. That gap is the whole risk: $0.20 a spread comes in, and no move in the world costs more than the gap less the credit."
      >
        <Card>
          <Payoff />
          <Pull>
            A stop-loss is a hope: it fills where the market lets it. A bought leg is a contract.
          </Pull>
        </Card>
      </Block>

      <Block
        label="04 · the measurements"
        title="We deleted our own defence rule."
        explains="It closed a spread the moment price touched the sold strike, and it felt prudent for four months. Measured across 672 trades, it loses to doing nothing."
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
        label="05 · check it yourself"
        title="One command. No credentials. No network."
        explains="Every figure this project publishes about its own measurements is recomputed from data committed to the repository. Beside them run twelve trials that attack the agent rather than confirm it — one failed, and took the rule above with it."
      >
        <Card>
          <div className="rounded-lg border border-line bg-surface-sunk p-5">
            <p className="font-mono text-[13px] text-muted">$ make claims</p>
            <pre className="mt-4 overflow-x-auto font-mono text-[13px] leading-relaxed text-secondary">
              {CLAIMS_OUTPUT}
            </pre>
          </div>
          <p className="mt-6 text-[15px] leading-relaxed text-secondary">
            Twenty-five claims, none failing. Six are shown; the rest cover the expiry gradient, the
            per-underlying returns and the cost of crossing the book.
          </p>
        </Card>
      </Block>

      <Block label="06 · the brief" title="What the hackathon asked for.">
        <div className="grid gap-4 sm:grid-cols-3">
          {REQUIRED.map(([asked, met]) => (
            <Card key={asked}>
              <Chip>{asked}</Chip>
              <p className="mt-4 text-[15px] leading-relaxed text-secondary">{met}</p>
            </Card>
          ))}
        </div>
      </Block>
    </main>
  )
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

// The trading day and what the file puts in it. Three entries open the account;
// what keeps it repeats too often to draw and is said in a line instead.
function Day() {
  // The three windows that may open the account, and the one that empties it.
  // `open-check` at 10:00 was here and is not: twenty minutes from the first
  // entry, its label ran into the next one and the two read as one word.
  const marks: [number, string][] = [
    [10.33, 'entry'],
    [12.5, 'entry'],
    [14.33, 'entry'],
    [15.67, 'flatten'],
  ]
  const at = (hour: number) => ((hour - 9.5) / 6.5) * 100

  return (
    <figure className="m-0">
      <figcaption className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        one trading day, as the file declares it
      </figcaption>
      {/* The day scrolls in its own box on a narrow screen rather than pushing the
          card sideways: at 330px the last label sits past the right edge, and a
          page that scrolls horizontally as a whole is worse than a strip that
          does. */}
      <div className="overflow-x-auto">
        <div className="relative mt-9 h-px min-w-[420px] bg-line-strong">
          {marks.map(([hour, name], index) => (
            <div
              key={index}
              className="absolute top-0 -translate-x-1/2"
              style={{ left: `${at(hour)}%` }}
            >
              <div className="mx-auto size-2 -translate-y-1/2 rounded-full bg-accent-ink" />
              <p className="mt-3 whitespace-nowrap font-mono text-[11px] text-secondary">{name}</p>
            </div>
          ))}
        </div>
        <div className="mt-12 flex min-w-[420px] justify-between font-mono text-[11px] text-muted">
          <span>09:30</span>
          <span>16:00</span>
        </div>
      </div>
      <p className="mt-6 text-[15px] leading-relaxed text-secondary">
        Under those, a defence every fifteen minutes and a news watch on a cheaper model. The
        session sets its own wake-ups on top — on a clock, or on a price it wants to hear about.
      </p>
    </figure>
  )
}

// What comes back when the session asks what it may do. The picture exists for
// its last row: a rule that admits it is there and withholds its number.
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
            className="flex flex-wrap items-center justify-between gap-x-6 gap-y-1 rounded-md bg-surface-sunk px-4 py-3"
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

const CLAIMS_OUTPUT = `PASS  646 trading days covered          646
PASS  one day to expiry pays          10.72
PASS  0.30 delta beats 0.45            True
PASS  holding pays a trade             2.94
PASS  closing on the touch             2.32
PASS  take-profit at 0.35 returns      6722

25 claims, 0 failed`

const SCHEDULE = `- name: defend
  cause: "checking the defence rules"
  every: 15m
  between: ["09:40", "15:55"]
  task: |
    First read the intents (read_state). A position whose
    intent says plainly that it was opened as a CHECK is
    not to be touched under any circumstances...`

const REQUIRED: [string, string][] = [
  [
    'autonomous agent',
    'A model session decides every trade. The schedule says when it runs and what it is asked; the code sends what the session chose and refuses what the limits forbid.',
  ],
  [
    "alpaca's mcp server",
    'Every order and every price goes through it. The account is read the same way, so what the page shows and what the agent saw are one answer.',
  ],
  [
    'options only',
    'Vertical spreads and backspreads. Never a naked short option: the largest possible loss is known before the order is sent.',
  ],
]

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
  explains?: string
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

// The line a section is remembered by, at the end of the card that earned it.
function Pull({ children }: { children: ReactNode }) {
  return (
    <p className="mt-7 border-l-2 border-line-strong pl-4 text-[19px] font-medium leading-[1.4] tracking-[-0.01em] text-primary">
      {children}
    </p>
  )
}

function Claim({ icon: Icon, title, says }: { icon: typeof Eye; title: string; says: string }) {
  return (
    <Card>
      <Icon className="size-5 text-muted" aria-hidden />
      <h2 className="mt-4 text-[22px] font-medium leading-tight tracking-[-0.01em] text-primary">
        {title}
      </h2>
      <p className="mt-2.5 text-[15px] leading-relaxed text-secondary">{says}</p>
    </Card>
  )
}
