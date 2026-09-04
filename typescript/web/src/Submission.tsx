import { Card, Chip, Eyebrow, Figure, Figures, inline, Panel } from './parts'
import { COUNTS, SAID } from './snapshot'

// What the hackathon asked for, and what answers each requirement.
//
// This lived on the landing as a numbered section, where it was the one block
// addressed to somebody other than the reader in front of it: everything else
// there argues why the thing is worth looking at, and this checks boxes. It reads
// better on the page named for the people who need it checked.
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

const CLAIMS_OUTPUT = `PASS  646 trading days covered          646
PASS  one day to expiry pays          10.72
PASS  0.30 delta beats 0.45            True
PASS  holding pays a trade             2.94
PASS  closing on the touch             2.32
PASS  take-profit at 0.35 returns      6722

25 claims, 0 failed`

export function Submission() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-16">
      <Eyebrow>[ for judges ]</Eyebrow>

      <h1 className="mt-6 max-w-[20ch] text-[40px] font-medium leading-[1.05] tracking-[-0.024em] text-primary">
        Everything a judge needs, in one place.
      </h1>

      <p className="mt-5 max-w-[58ch] text-[19px] leading-[1.35] text-secondary">
        The account, the rules it was held to, and the commands that recompute every number this
        project publishes.
      </p>

      <section className="mt-16">
        <h2 className="text-[28px] font-medium leading-tight tracking-[-0.015em] text-primary">
          What the hackathon asked for.
        </h2>

        <div className="mt-7 grid gap-4 sm:grid-cols-3">
          {REQUIRED.map(([asked, met]) => (
            <Card key={asked}>
              <Chip>{asked}</Chip>
              <p className="mt-4 text-[15px] leading-relaxed text-secondary">{met}</p>
            </Card>
          ))}
        </div>
      </section>

      {/* MOVED HERE FROM THE LANDING, all three of them. The landing argues why
          the thing is worth looking at; these three answer a reader who has
          decided to check. A command to re-run, the counts a judge can hold
          against the broker's own list, and the agent in its own words. */}
      <section className="mt-16">
        <h2 className="text-[28px] font-medium leading-tight tracking-[-0.015em] text-primary">
          One command. No credentials. No network.
        </h2>
        <p className="mt-4 max-w-[68ch] text-[17px] leading-[1.5] text-secondary">
          Every figure this project publishes about its own measurements is recomputed from data
          committed to the repository. Beside them run thirteen trials that attack the agent rather
          than confirm it — one failed, and took a defence rule with it.
        </p>

        <div className="mt-7">
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
        </div>
      </section>

      <section className="mt-16">
        <h2 className="text-[28px] font-medium leading-tight tracking-[-0.015em] text-primary">
          The week, as the broker lists it.
        </h2>
        <p className="mt-4 max-w-[68ch] text-[17px] leading-[1.5] text-secondary">
          Counted from the broker&apos;s own order list rather than from our record — that is the
          list that can be opened beside this page, and the two have to agree.
        </p>

        <div className="mt-7">
          <Figures spread>
            {COUNTS.map(([name, value]) => (
              <Figure key={name} name={name} value={value} />
            ))}
          </Figures>
        </div>
      </section>

      <section className="mt-16">
        <h2 className="text-[28px] font-medium leading-tight tracking-[-0.015em] text-primary">
          What it said, unedited.
        </h2>
        <p className="mt-4 max-w-[68ch] text-[17px] leading-[1.5] text-secondary">
          Six lines the agent wrote, in its own words. Three of the six are the session deciding not
          to trade — that is the proportion the week actually had, and a page showing only the
          entries would describe a different agent.
        </p>

        <ul className="m-0 mt-7 list-none space-y-2 p-0">
          {SAID.map((line) => (
            <li key={line.at}>
              <Card>
                <p className="font-mono text-[11px] text-muted">{line.at}</p>
                <p className="mt-1.5 text-[15px] leading-relaxed text-secondary">{inline(line.text)}</p>
              </Card>
            </li>
          ))}
        </ul>
      </section>

      <p className="mt-14 rounded-xl border border-dashed border-line px-6 py-5 text-[15px] text-muted">
        The account moves on{' '}
        <a className="text-accent underline underline-offset-4" href="/live">
          Live
        </a>
        , and the two settled figures are at the foot of the{' '}
        <a className="text-accent underline underline-offset-4" href="/">
          overview
        </a>
        .
      </p>

    </main>
  )
}
