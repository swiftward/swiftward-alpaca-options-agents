import { ArrowRight, Eye, FileCode, Ruler } from 'lucide-react'
import { Link } from 'react-router'

import { Card, Chip, Eyebrow, Figure, Figures, Section, Table } from './parts'

// The landing page: what this is, how it differs, and where to go and look.
//
// Ordered for a reader who arrives and leaves, not for one who is walked through
// in order. What the session DOES comes first, because a reader who cannot see
// the model working will read everything after it as a backtest with a chat
// window attached. The limits come second and the measurements third: they are
// what the deciding is held to and what its numbers rest on, and neither is the
// thing being sold.
export function Landing() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-20">
      <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>

      <h1 className="mt-6 max-w-[17ch] text-[56px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[72px]">
        A model decides every trade. A file decides when it is asked.
      </h1>

      <p className="mt-7 max-w-[52ch] text-[22px] font-medium leading-[1.25] tracking-[-0.01em] text-secondary">
        Thirteen windows, written in plain words, say when a session wakes and what question it is
        put. The session reaches for the playbooks it needs, reads 284 names, works out what the
        book will actually pay, and either trades or says why it will not.
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

      {/* Four numbers, in the order the page argues them: what the session does,
          then what holds it, then what its numbers rest on. The zero is worth
          stopping on - risk limits written into a prompt are a request, and this
          session is given none of them to read or rewrite. */}
      <div className="mt-14">
        <Figures>
          <Figure name="windows the file declares" value="13" />
          <Figure name="names read on every pass" value="284" />
          <Figure name="risk limits in the prompt" value="0" />
          <Figure name="published numbers that recompute" value="25 / 25" />
        </Figures>
      </div>

      <div className="mt-16 mb-16 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={FileCode}
          title="The agent is written, not coded"
          says="A declaration says when a session wakes and what it is asked; five playbooks say how each technique is run. All of it is prose the session reads at the moment it acts, so changing what the agent does is editing a file. We edited one while the market was open and the next session read the new rule."
        />
        <Claim
          icon={Ruler}
          title="Limits it cannot reach"
          says="Nothing about risk is written into its instructions. An order is refused before it reaches the broker, and there is nothing in the session's reach to edit. It asks what it may do while it works — and is sometimes told only that a rule is there, without the number, so there is nothing to route around."
        />
        <Claim
          icon={Eye}
          title="Reasoning you can read"
          says="Each run shows what woke it, what it asked the broker, and what it concluded — in its own words. Including the runs where it priced six candidates, refused all six, and said what each one paid."
        />
      </div>

      <Section
        title="The file is the agent"
        explains="A window says when a session starts and what question it is being asked. It never says what to trade — that is the session's own decision, which is what makes this an agent rather than a script with a model in it. One engine runs several accounts from several files, and the difference between the files is the whole experiment."
      >
        <Card>
          <pre className="overflow-x-auto font-mono text-[13px] leading-relaxed text-secondary">
            {SCHEDULE}
          </pre>
          <p className="mt-5 text-[15px] leading-relaxed text-secondary">
            Thirteen of these run the day: three entry windows, a defence every fifteen minutes, a
            news watch on a cheaper model, and a flatten before the bell. The session sets its own
            wake-ups on top of them — on a clock, or on a price it wants to hear about.
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
          <p className="mt-4 text-[15px] leading-relaxed text-primary">
            A stop-loss is a hope: it fills where the market lets it. A bought leg is a contract.
          </p>
        </Card>
      </Section>

      <Section
        title="We deleted our own defence rule"
        explains="It closed a spread the moment the price touched the sold strike. It felt prudent for four months. Then it was measured against 672 trades, and the history said it loses to doing nothing."
      >
        <Card>
          <Table
            head={['exit rule', 'a trade']}
            rows={EXITS.map(([rule, value, note]) => [
              <span className={note ? 'text-primary' : ''}>
                {rule}
                {note ? <span className="ml-3 font-mono text-[13px] text-loss">{note}</span> : null}
              </span>,
              <span className="font-mono tabular-nums">{value}</span>,
            ])}
            empty="nothing to compare"
          />
          <p className="mt-5 text-[15px] leading-relaxed text-secondary">
            The price passed the sold strike in 42.7% of trades and only 26.6% ended breached. The
            rule was paying for 108 crossings that bought nothing. It is gone: a spread is now left
            alone until the price passes the leg that caps the loss.
          </p>
          <p className="mt-4 text-[15px] leading-relaxed text-primary">
            A measurement that only ever agrees with you is not a measurement.
          </p>
        </Card>
      </Section>

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
          <p className="mt-4 text-[15px] leading-relaxed text-secondary">
            Beside them run twelve trials that attack the agent rather than confirm it: the limits
            service answers with a broken payload — does the agent stop, or invent a number? One of
            the twelve failed, and the rule it broke is the one deleted above.
          </p>
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

// The exit rules, side by side, on the 672 trades the history holds. The middle
// one is what we shipped for four months and then removed.
const EXITS: [string, string, string?][] = [
  ['hold to expiry', '$2.94'],
  ['close when the sold strike is touched', '$2.32', '← removed'],
  ['close a full width past the sold strike', '$3.46'],
]

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
