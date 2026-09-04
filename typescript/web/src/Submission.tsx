import { Link } from 'react-router'

import { Card, Chip, Eyebrow, LiveLink, Mark, Panel, Section } from './parts'
import { ACCOUNT, BENCHMARK, MEASUREMENTS, type Measurement, OPENED } from './snapshot'

// The page for the people scoring this, and the only one written for them.
//
// The landing argues why the thing is worth looking at; this one assumes that is
// settled and answers the next question, which is WHERE THE EVIDENCE IS. Every row
// ends in a link or a command, because a claim a judge cannot open is a claim they
// have to take on trust, and there is a week of work here that does not need to be.
const VIDEO = 'https://youtu.be/AWgiXKl8ysI'
const REPOSITORY = 'https://github.com/swiftward/swiftward-alpaca-options-agents'
// Each criterion below opens a DIFFERENT page. Four links to the same repository
// root is four ways of saying "go and read it", and a judge with a quarter of an
// hour reads none of them.
const DOCS = `${REPOSITORY}/blob/main/docs`
// Served by this page's own host, from typescript/web/public. The file is in the
// repository, so a reader of the code has it too, but the link a judge follows
// depends on nothing outside this deployment - no drive share to expire and no
// repository that has to be public yet.
const DECK = '/slides.pdf'

// Six of the twenty-five lines `make claims` prints, copied from a run rather than
// written: a judge who runs the command reads these back word for word.
const CLAIMS_OUTPUT = `PASS  the history covers 646 trading days                646    646
PASS  one day to expiry pays more than five             True   True
PASS  the expiry gradient is monotone from 2 to 5 days  True   True
PASS  closing on the touch is worse than holding        True   True
PASS  closing at 0.35 of the credit returns 6722        6722   6722
PASS  the take-profit measurement covers 597 trades     597    597

25 claims, 0 failed`

// The four this is scored on, each against the thing that answers it. Nothing here
// is a plea: every row is a number, a file or a command.
const SCORED: { on: string; says: string; where: { text: string; to: string }[] }[] = [
  {
    on: 'P&L Performance',
    says: 'Four sessions cannot separate skill from a good draw, so the evidence for the strategy is 646 committed trading days rather than the week alone. Vertical spreads and backspreads only — never a naked short option, so the worst case is known before the order goes.',
    where: [
      {
        text: "the broker's own answers, committed",
        to: `${REPOSITORY}/tree/main/docs/account-evidence`,
      },
    ],
  },
  {
    on: 'Technology Implementation',
    says: 'A model session decides every trade; the code sends what it chose and refuses what the limits forbid. Orders and market data go through the released Alpaca MCP server — a vertical is one call with order_class=mleg, not two legs raced against each other.',
    where: [
      { text: 'the architecture', to: `${DOCS}/architecture.md` },
      { text: 'what it can and cannot do', to: `${DOCS}/capabilities.md` },
    ],
  },
  {
    on: 'Creativity & Originality',
    says: 'Three of them: the limits are asked for at runtime rather than written into a prompt; the test stand displaces the REAL option book by one number instead of replaying history; and a measurement we published killed one of our own rules.',
    where: [
      { text: 'how it is tested, and what it killed', to: `${DOCS}/write-up.md#how-it-is-tested` },
      { text: 'how a trade is decided', to: `${DOCS}/algorithm.md` },
    ],
  },
  {
    on: 'Presentation & Execution',
    says: 'The page a judge opens reads the broker through the same process the agent does, so the figure on the screen and the figure on the account are one answer. Twenty-five published numbers recompute from one command.',
    where: [
      { text: 'the live page', to: '/live' },
      { text: 'the five routes it reads', to: `${DOCS}/api.md` },
    ],
  },
]

// The build-in-public posts, in the order they went out. No author is named: this
// repository names nobody on the team, and what a post is about is what a judge
// came to read.
const POSTS: { on: string; at: string; about: string; to: string }[] = [
  {
    on: 'X',
    at: '28 August',
    about: 'The agent invented a 45-minute rule for itself, and then obeyed it.',
    to: 'https://x.com/KTrunin/status/2093447602549243988',
  },
  {
    on: 'LinkedIn',
    at: '29 August',
    about:
      'That invented rule in full: seven mentions counted in one day, five agreeing with permission and two refusing — one of which cost an order.',
    to: 'https://www.linkedin.com/feed/update/urn:li:share:7499563354768842752/',
  },
  {
    on: 'X',
    at: '29 August',
    about:
      'A 10,000-set order with a worst case of $2,020,000, and the agent arguing its own cage into existence.',
    to: 'https://x.com/KTrunin/status/2093806824432496900',
  },
  {
    on: 'LinkedIn',
    at: '30 August',
    about:
      'Limits are fetched, not written into the prompt: two ceilings raised mid-day with one edit and no restart.',
    to: 'https://www.linkedin.com/feed/update/urn:li:share:7499918634673217536/',
  },
  {
    on: 'X',
    at: '30 August',
    about: 'A price alert cut into a turn, and the agent closed a spread mid-thought.',
    to: 'https://x.com/KTrunin/status/2094218911520555093',
  },
  {
    on: 'X',
    at: '4 September',
    about:
      'The week closed: four trading days, one paper account, nobody trading it by hand, and the repository opened.',
    to: 'https://x.com/KTrunin/status/2095856013874171965',
  },
]

export function Submission() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-16">
      <Eyebrow>[ for judges ]</Eyebrow>

      <h1 className="mt-6 max-w-[20ch] text-[40px] font-medium leading-[1.05] tracking-[-0.024em] text-primary">
        Everything, and where to check it.
      </h1>

      <p className="mt-5 max-w-[62ch] text-[19px] leading-[1.35] text-secondary">
        An autonomous options agent that asks what it is allowed to risk before it risks it.{' '}
        <Mark>Every limit is held outside the agent</Mark>, read at the start of each turn rather
        than written into a prompt, and an order past one is refused before it reaches Alpaca.
      </p>

      {/* THE FOUR THINGS A JUDGE OPENS, at the top where they are looked for. */}
      <div className="mt-9 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Opens to="/live" name="The account, live" says="It moves while you watch it." inside />
        <Opens to={VIDEO} name="The video" says="Three minutes, and it runs." />
        <Opens to={REPOSITORY} name="The source" says="The whole history, MIT." />
        <Opens to={DECK} name="The deck" says="Thirteen pages: what it trades, what each control stops, and the account it is judged on." />
      </div>

      <div className="mt-16">
        <Section
          title="The account"
          explains="Named so it can be opened rather than taken on trust, and measured twice: the two judges' clocks disagree on when the week ends, so both cut-offs are here with the rule each uses."
        >
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <Card>
              <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
                alpaca paper account
              </p>
              <p className="mt-2 font-mono text-[24px] font-medium leading-none text-primary">
                {ACCOUNT}
              </p>
              <p className="mt-4 text-[15px] leading-relaxed text-secondary">
                Simulated funds, real market data, no real money. Opened at $
                {OPENED.toLocaleString('en-US')} on the kickoff day and never reset; its first order
                went out on the Monday, when the measurement window opens.
              </p>
            </Card>

            {MEASUREMENTS.map((m) => (
              <Result key={m.by} of={m} />
            ))}
          </div>

          <div className="mt-6 flex flex-wrap items-center gap-4">
            <LiveLink />
            <span className="text-[15px] text-secondary">
              The figures above are frozen. The account itself is not.
            </span>
          </div>
        </Section>

        <Section
          title="Scored on four things"
          explains="Each with what answers it, and a way to open that rather than a description of it."
        >
          <div className="grid gap-4 sm:grid-cols-2">
            {SCORED.map((one) => (
              <Card key={one.on} marked>
                <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
                  {one.on}
                </p>
                <p className="mt-3 text-[15px] leading-relaxed text-secondary">{one.says}</p>
                <p className="mt-4 flex flex-wrap gap-x-4 gap-y-2">
                  {one.where.map((link) =>
                    link.to.startsWith('/') ? (
                      <Link
                        key={link.text}
                        to={link.to}
                        className="text-[14px] text-accent underline underline-offset-4"
                      >
                        {link.text}
                      </Link>
                    ) : (
                      <a
                        key={link.text}
                        href={link.to}
                        target="_blank"
                        rel="noreferrer"
                        className="text-[14px] text-accent underline underline-offset-4"
                      >
                        {link.text}
                      </a>
                    ),
                  )}
                </p>
              </Card>
            ))}
          </div>
        </Section>

        <Section
          title="One command. No credentials. No network."
          explains="Every figure this project publishes about its own measurements recomputes from data committed to the repository. Beside them run thirteen trials that attack the agent rather than confirm it — one failed, and took a defence rule with it."
        >
          <Card>
            <Panel title="a clone of this repository, and nothing else">
              <p className="font-mono text-[13px] text-code-punct">
                $ <span className="text-code-fg">make claims</span>
              </p>
              <pre className="m-0 mt-4 font-mono text-[13px] leading-relaxed text-code-fg">
                {CLAIMS_OUTPUT}
              </pre>
            </Panel>
            <p className="mt-6 text-[15px] leading-relaxed text-secondary">
              Twenty-five claims, none failing. Six are shown; the rest cover the expiry gradient,
              the per-underlying returns and the cost of crossing the book.
            </p>
          </Card>
        </Section>

        <Section
          title="Built in public"
          explains="Six posts, published as the week ran rather than written up after it. Each tells a different story."
        >
          {/* A TIMELINE RATHER THAN A GRID, because the order is the point: these
              went out as the week ran, and a grid of cards says only how many
              there were. The dates sit on the line, the posts alternate
              across it; below the small breakpoint it folds to one column with
              the line down the left, where there is no room for two. */}
          <div className="relative mt-2">
            <div className="absolute inset-y-0 left-[7px] w-px bg-line sm:left-1/2" aria-hidden />

            <ol className="m-0 grid list-none gap-6 p-0 sm:gap-4">
              {POSTS.map((post, i) => (
                <li
                  key={post.to}
                  className="relative pl-9 sm:grid sm:grid-cols-2 sm:items-center sm:gap-x-28 sm:pl-0"
                >
                  <span
                    className="absolute left-[3px] top-[9px] size-[9px] rounded-full bg-accent sm:hidden"
                    aria-hidden
                  />

                  <span className="mb-3 inline-flex rounded-full bg-accent px-3 py-1 font-mono text-[11px] uppercase tracking-[0.04em] text-on-accent sm:absolute sm:left-1/2 sm:top-1/2 sm:z-10 sm:mb-0 sm:-translate-x-1/2 sm:-translate-y-1/2">
                    {post.at}
                  </span>

                  <div className={i % 2 === 1 ? 'sm:col-start-2' : 'sm:col-start-1 sm:row-start-1'}>
                    <a
                      href={post.to}
                      target="_blank"
                      rel="noreferrer"
                      className="block transition-opacity hover:opacity-80"
                    >
                      <Card>
                        <Chip>{post.on}</Chip>
                        <p className="mt-3 text-[15px] leading-relaxed text-secondary">
                          {post.about}
                        </p>
                      </Card>
                    </a>
                  </div>
                </li>
              ))}
            </ol>
          </div>
        </Section>
      </div>
    </main>
  )
}

function Result({ of }: { of: Measurement }) {
  const taken = of.equity !== null
  const profit = taken ? (of.equity as number) - OPENED : 0

  return (
    <Card>
      <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
        {of.by} · {of.when}
      </p>

      {taken ? (
        <p className="mt-2 flex flex-wrap items-baseline gap-x-3 text-[24px] font-medium leading-none tabular-nums text-gain">
          +$
          {profit.toLocaleString('en-US', {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
          })}
          <span className="text-[17px]">+{((profit / OPENED) * 100).toFixed(2)}%</span>
        </p>
      ) : (
        <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
          <span className="text-[24px] font-medium leading-none text-muted">&mdash;</span>
          <Chip>not yet taken</Chip>
        </p>
      )}

      <p className="mt-4 text-[15px] leading-relaxed text-secondary">
        {of.rule}
        {taken ? (
          <>
            {' '}
            Against {BENCHMARK.name} at +{BENCHMARK.percent.toFixed(2)}% over the same{' '}
            {BENCHMARK.sessions} sessions.
          </>
        ) : null}
      </p>
    </Card>
  )
}

// One thing to open. The deck has no address yet, and a card with an empty href is
// a link that looks broken; without one it reads as plainly waiting.
function Opens({
  to,
  name,
  says,
  inside,
}: {
  to: string
  name: string
  says: string
  inside?: boolean
}) {
  const body = (
    <span className="block h-full rounded-xl border border-line bg-surface-raised p-5 shadow-[0_1px_2px_rgba(16,18,22,0.04),0_12px_32px_-16px_rgba(16,18,22,0.14)]">
      <span className="block text-[17px] font-medium text-primary">{name}</span>
      <span className="mt-1.5 block text-[14px] leading-snug text-secondary">{says}</span>
    </span>
  )

  if (!to) return <span className="block opacity-55">{body}</span>

  return inside ? (
    <Link to={to} className="block transition-opacity hover:opacity-80">
      {body}
    </Link>
  ) : (
    <a
      href={to}
      target="_blank"
      rel="noreferrer"
      className="block transition-opacity hover:opacity-80"
    >
      {body}
    </a>
  )
}
