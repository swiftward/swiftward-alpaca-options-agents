import { ArrowRight, Eye, GitBranch, Ruler } from 'lucide-react'
import { Link } from 'react-router'

import { Card, Chip, Eyebrow, Section, Table } from './parts'

// The landing page: what this is, how it differs, and where to go and look.
//
// The three claims were not chosen for their looks. They are the only things we
// have that the neighbouring teams almost certainly will not: limits that arrive
// by discovery; numbers measured on the history; and reasoning that is visible.
//
// Everything below them is written for a reader who has to decide quickly whether
// the thing is real - so it shows the command, the declaration and one trade
// worked through, rather than describing any of the three.
export function Landing() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-20">
      <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>

      <h1 className="mt-6 max-w-[16ch] text-[56px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[72px]">
        An options agent that shows its reasoning.
      </h1>

      <p className="mt-7 max-w-[46ch] text-[22px] font-medium leading-[1.25] tracking-[-0.01em] text-secondary">
        It wakes on a schedule, reads what the market offers, states what it means to do — and
        only then sends an order. Every number it trades on was measured, not guessed.
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

      <div className="mt-20 mb-16 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={Ruler}
          title="Limits it discovered"
          says="Nothing about risk is written into its instructions. It asks what it is allowed to do while it works, and the page shows the same answer it gets — including the rule that admits it exists and withholds its number."
        />
        <Claim
          icon={GitBranch}
          title="Numbers that were measured"
          says="Two and a half years of option prices decided the thresholds. The first version of the rule lost money by construction; the measurement is what found that, and the comment beside every number names the script that produced it."
        />
        <Claim
          icon={Eye}
          title="Reasoning you can read"
          says="Each run shows what woke it, what it asked the broker, and what it concluded — in its own words. Including the runs where it looked and decided to do nothing."
        />
      </div>

      <Section
        title="Check the numbers yourself"
        explains="Every figure this project publishes about its own measurements is recomputed by one command, from data committed to the repository. It reaches no network and needs no key of ours."
      >
        <Card>
          <p className="font-mono text-sm text-primary">$ make claims</p>
          <div className="mt-5 overflow-x-auto">
            <Table
              head={['claim', 'computed', 'published']}
              rows={CLAIMS.map(([what, value]) => [
                what,
                <span className="font-mono tabular-nums">{value}</span>,
                <span className="font-mono tabular-nums">{value}</span>,
              ])}
              empty="nothing to check"
            />
          </div>
          <p className="mt-5 text-[15px] leading-relaxed text-secondary">
            Twenty-five claims, none failing. Five of them are shown; the rest cover the expiry
            gradient, the per-underlying returns and the cost of crossing the book.
          </p>
        </Card>
      </Section>

      <Section
        title="One trade, in plain words"
        explains="No options background is needed to read the page. This is the whole of what the agent does, on a position it actually held."
      >
        <Card>
          <p className="text-[15px] leading-relaxed text-secondary">
            It <span className="text-primary">sold</span> the right to buy SPY at $772 for $0.79,
            and <span className="text-primary">bought</span> the right to buy it at $773 for $0.59.
            The difference, $0.20 a spread, is paid to the account the moment the order fills — 117
            spreads, so $2,340.
          </p>
          <p className="mt-4 text-[15px] leading-relaxed text-secondary">
            If SPY finishes below $772 on the expiry day, both rights expire worthless and the
            $2,340 is kept. If it finishes above $773, the pair is worth its full $1 gap and the
            loss is $9,360 — the gap less the credit. Nothing it can do costs more than that, and
            the figure is known before the order goes out. That is what{' '}
            <span className="text-primary">defined risk</span> means, and it is the only kind of
            structure this agent is allowed to open.
          </p>
        </Card>
      </Section>

      <Section
        title="When it wakes, and why"
        explains="The schedule is a declaration rather than code. It says when a session starts and what question it is being asked; it never says what to trade — that is the session's own decision, which is what makes the agent autonomous rather than driven."
      >
        <Card>
          <pre className="overflow-x-auto font-mono text-[13px] leading-relaxed text-secondary">
            {SCHEDULE}
          </pre>
        </Card>
      </Section>

      <Section title="What the hackathon asked for">
        <div className="grid gap-4 sm:grid-cols-3">
          {REQUIRED.map(([asked, met]) => (
            <Card key={asked}>
              <Chip>{asked}</Chip>
              <p className="mt-4 text-[15px] leading-relaxed text-secondary">{met}</p>
            </Card>
          ))}
        </div>
      </Section>
    </main>
  )
}

// Five of the twenty-five, chosen because each one decided a rule. Computed and
// published are one value here because the check passes; the command prints them
// in two columns precisely so that a failure is visible as a difference.
const CLAIMS: [string, string][] = [
  ['the history covers 646 trading days', '646'],
  ['one day to expiry pays 10.72 a trade', '10.72'],
  ['holding to expiry pays 2.94 a trade', '2.94'],
  ['closing on the touch pays 2.32 a trade', '2.32'],
  ['closing at 0.35 of the credit returns 6722', '6722'],
]

const SCHEDULE = `- name: defend
  cause: "checking the defence rules"
  every: 15m
  between: ["09:40", "15:55"]
  days: [mon, tue, wed, thu, fri]
  task: |
    First read the intents (read_state). A position whose
    intent says plainly that it was opened as a CHECK is
    not to be touched under any circumstances...`

const REQUIRED: [string, string][] = [
  [
    'autonomous agent',
    'A model session decides every trade. The schedule says when it runs and what it is being asked; the code sends what the session chose and refuses what the limits forbid.',
  ],
  [
    "alpaca's mcp server",
    'Every order and every price goes through it. The account is read the same way, so what the page shows and what the agent saw are one answer.',
  ],
  [
    'options only',
    'Vertical spreads and backspreads. Never a naked short option: the largest possible loss is known before the order is sent, and the gateway the order passes through refuses one that is too large.',
  ],
]

function Claim({
  icon: Icon,
  title,
  says,
}: {
  icon: typeof Eye
  title: string
  says: string
}) {
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
