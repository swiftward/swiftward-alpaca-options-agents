import {
  ArrowDownRight,
  ArrowUpRight,
  CircleAlert,
  CircleCheck,
  LoaderCircle,
  Minus,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import type {
  Everything,
  Limits,
  Money,
  Said,
  Snapshot,
  State,
  Sweep,
  ToolCall,
  Turn,
  TurnCause,
} from './api'
import { readEverything } from './api'
import { Equity } from './Equity'
import { ago, clock, dollars, marketOpen, percent, signed, took, trim } from './format'
import {
  Card,
  Chip,
  Empty,
  Eyebrow,
  Figure,
  Figures,
  inline,
  Mark,
  Section,
  Table,
  Unavailable,
  Yaml,
} from './parts'

// Every fifteen seconds. Not a stream: the data changes once a minute or two,
// and a stream costs complexity that would not pay for itself in a week.
const refreshEvery = 15_000

export function Live() {
  const [all, setAll] = useState<Everything | null>(null)
  const [readAt, setReadAt] = useState<Date | null>(null)

  useEffect(() => {
    let alive = true
    const pull = async () => {
      const answer = await readEverything()
      if (!alive) return
      setAll(answer)
      setReadAt(new Date())
    }

    void pull()
    const timer = setInterval(() => void pull(), refreshEvery)

    return () => {
      alive = false
      clearInterval(timer)
    }
  }, [])

  const failed = all
    ? Object.values(all).filter((answer) => !answer.ok).map((answer) => (answer as { why: string }).why)
    : []

  return (
    <main className="mx-auto max-w-6xl px-6 pb-24 pt-10 text-primary">
      <header className="mb-14">
        <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>

        <h1 className="mt-6 max-w-[20ch] text-[40px] font-medium leading-[1.05] tracking-[-0.024em] text-primary">
          What the agent is doing, right now.
        </h1>
        <p className="mt-4 max-w-[52ch] text-[20px] font-medium leading-[1.25] tracking-[-0.01em] text-secondary">
          Every run it makes, every limit it was handed, and every conclusion it reached — in
          its own words, refusals included.
        </p>

        <p className="mt-6 flex flex-wrap items-center gap-2">
          {/* WHICH ACCOUNT. The landing names it and this page did not, though this
              is the one a judge opens to check the figures against the broker. It
              comes from the broker's own answer, not from a constant. */}
          {all?.money.ok ? <Chip>{all.money.value.account.number}</Chip> : null}
          {/* The dot is the SIGNAL that this page moves, so it takes the accent.
              It was green, which on every other page here means money. */}
          <Chip tone={failed.length > 0 ? 'loss' : undefined}>
            <span
              className={`size-1.5 rounded-full ${
                failed.length > 0 ? 'bg-loss' : 'animate-pulse bg-accent'
              }`}
              aria-hidden
            />
            {readAt ? (
              `read ${clock(readAt.toISOString())} New York`
            ) : (
              <Bone className="my-[3px] h-2.5 w-32" />
            )}
          </Chip>
          {failed.map((why) => (
            <Chip key={why} tone="loss">
              {why}
            </Chip>
          ))}
        </p>
      </header>

      {all ? <Page all={all} /> : <Loading />}
    </main>
  )
}

// THE FIRST PAINT, before the five requests answer.
//
// It used to be the word "reading…", in the header chip and again in the body, on
// a page whose whole claim is that it shows rather than tells. The word said
// nothing the empty page had not already said, and it said it twice.
//
// The shapes stand where the real blocks will, so nothing jumps when the data
// lands - the account's two figures, the curve, and the four counters. Below that
// the page varies with what the broker returned, and a skeleton that guessed
// would be a promise the data may not keep.
//
// The pulse is on the container rather than on each block: separate animations
// drift apart within a second and read as a page that is broken rather than
// loading. `motion-safe` respects a reader who has asked for stillness, and the
// screen reader gets the word the screen no longer needs.
function Loading() {
  return (
    <div role="status" aria-live="polite" className="motion-safe:animate-pulse">
      <span className="sr-only">Reading the account.</span>

      <section className="mb-16">
        <Bone className="h-6 w-52" />
        <div className="mt-5 grid gap-4 sm:grid-cols-2">
          <Card>
            <Bone className="h-3 w-20" />
            <Bone className="mt-4 h-9 w-44 sm:h-11" />
            <Bone className="mt-5 h-3 w-36" />
          </Card>
          <Card>
            <Bone className="h-3 w-32" />
            <Bone className="mt-4 h-9 w-52 sm:h-11" />
            <Bone className="mt-5 h-3 w-40" />
          </Card>
        </div>
        <Bone className="mt-4 h-56 w-full rounded-xl" />
        <Bone className="mt-3 h-3 w-64" />
      </section>

      <section>
        <Bone className="h-6 w-40" />
        <Bone className="mt-4 h-3 w-full max-w-[560px]" />
        <div className="mt-5 grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Card key={i}>
              <Bone className="h-3 w-24" />
              <Bone className="mt-4 h-7 w-16" />
              <Bone className="mt-4 h-3 w-full" />
            </Card>
          ))}
        </div>
      </section>
    </div>
  )
}

// One block of the skeleton. The colour is the sunk surface rather than a grey of
// its own, so it stays a shade of the card it sits in whichever theme is on.
function Bone({ className }: { className: string }) {
  return <span className={`block rounded bg-surface-sunk ${className}`} aria-hidden />
}

function Page({ all }: { all: Everything }) {
  const state = all.state.ok ? all.state.value : undefined
  const money = all.money.ok ? all.money.value : undefined

  return (
    <>
      <Section title="The account">
        {all.money.ok ? (
          <Account money={all.money.value} line={all.equity.ok ? all.equity.value : undefined} />
        ) : (
          // The broker is silent - but the profit is computed from OUR history,
          // and that arrived. Showing a failure over data we do have hides from
          // the reader the one thing they opened the page for.
          <>
            {all.equity.ok ? <FromHistory line={all.equity.value} /> : null}
            <div className="mt-4">
              <Unavailable why={all.money.why} />
            </div>
          </>
        )}
        <div className="mt-4">
          {all.equity.ok ? <Equity line={all.equity.value} /> : <Unavailable why={all.equity.why} />}
        </div>
      </Section>

      {state ? (
        <Section title="At a glance"
        explains="The week in numbers: what it sent, what filled, what it filed, and what it is holding now.">
          <Counters state={state} money={money} />
        </Section>
      ) : null}

      <Section
        title="Limits it discovered"
        explains={
          <>
            <Mark>None of this is written into the agent’s instructions.</Mark> It asks what it may
            do while it works, and this is the same answer it gets — down to the rule that admits
            it exists and withholds its number.
          </>
        }
      >
        {all.limits.ok ? <LimitsCard limits={all.limits.value} /> : <Unavailable why={all.limits.why} />}
      </Section>

      <Section
        title="What the market offers"
        explains="The screener prices every permitted underlying, over and over. This is its last pass and how long ago it ran."
      >
        {all.sweep.ok ? <SweepCard sweep={all.sweep.value} /> : <Unavailable why={all.sweep.why} />}
      </Section>

      <Section title="Open positions" explains="What it is holding right now, valued by the broker.">
        {money ? <Positions money={money} /> : <Empty says="the account is unavailable" />}
      </Section>

      <Section
        title="Every run"
        explains="When it ran, what woke it, what it asked — and what it concluded. The runs where it looked and took nothing are here too, and they say why. A waking that recorded no words at all is not: it woke, found nothing to think about, and finished."
      >
        {state ? <Turns state={state} /> : <Empty says="the record is unavailable" />}
      </Section>
    </>
  )
}

function Account({ money, line }: { money: Money; line?: Snapshot[] }) {
  // P&L from the START, not from yesterday's close. It is the first thing the
  // work is judged by, and until now the page did not carry it at all: it showed
  // the change over a day, which has nothing to do with the week's result.
  //
  // It is measured from the first recorded measurement, not from an imagined
  // hundred thousand: an account can be opened with a different sum, and a guessed
  // number on a page read for its numbers is worse than a missing one. The caption
  // says what it was measured from.
  const opened = (line ?? [])[0]?.equity
  const total = opened === undefined ? undefined : money.account.equity - opened
  const totalShare = opened ? (total ?? 0) / opened : 0

  return (
    // TWO NUMBERS, ONE TO A CARD. Five stood in one row and the two that matter
    // had to be found among them.
    //
    // `today` and `buying power` are gone. The day's change is a different measure
    // from the one this account is judged by - a week - and on a page read once it
    // invites the wrong comparison; buying power is a broker's artefact, four times
    // the cash at this options level, and says nothing about the work. Nothing is
    // hidden by their absence: every move including the drawdowns is in the curve
    // below, and the open positions carry their own losses.
    <div className="grid gap-4 sm:grid-cols-2">
      <Card>
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">equity</p>
        <p className="mt-2 text-[32px] font-medium leading-none tracking-[-0.024em] text-primary tabular-nums sm:text-[44px]">
          {dollars(money.account.equity)}
        </p>
        <p className="mt-4 font-mono text-[12px] text-muted">
          {dollars(money.account.cash)} of it in cash
        </p>
      </Card>

      <Card>
        <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">
          profit since the start
        </p>
        {total === undefined ? (
          <p className="mt-2 text-[44px] font-medium leading-none text-muted">&mdash;</p>
        ) : (
          <>
            <p
              className={`mt-2 flex flex-wrap items-baseline gap-x-2 gap-y-1 text-[32px] font-medium leading-none tracking-[-0.024em] tabular-nums sm:text-[44px] ${
                total > 0 ? 'text-gain' : total < 0 ? 'text-loss' : 'text-primary'
              }`}
            >
              {/* The arrow is a second cue beside the colour, for readers who do
                  not distinguish it. */}
              {total > 0 ? (
                <ArrowUpRight className="size-6 shrink-0 self-center sm:size-8" aria-hidden />
              ) : total < 0 ? (
                <ArrowDownRight className="size-6 shrink-0 self-center sm:size-8" aria-hidden />
              ) : (
                <Minus className="size-6 shrink-0 self-center sm:size-8" aria-hidden />
              )}
              {signed(total)}
              {/* The share belongs BESIDE the sum, not in the caption under it:
                  they are one claim, and a reader weighing the result should not
                  have to travel to find half of it. Smaller, same colour. */}
              <span className="text-[18px] tracking-[-0.01em] sm:text-[22px]">{percent(totalShare)}</span>
            </p>
            <p className="mt-4 font-mono text-[12px] text-muted">
              from {dollars(opened as number)} at the first reading
            </p>
          </>
        )}
      </Card>
    </div>
  )
}

// The account from our own record, when the broker does not answer. The numbers
// are the same and come from the same series as the curve; the only difference is
// that "now" here is the last recorded measurement rather than a live answer from
// the broker, and the caption says so.
function FromHistory({ line }: { line: Snapshot[] }) {
  const points = line ?? []
  if (points.length === 0) return null

  const opened = points[0].equity
  const now = points[points.length - 1].equity
  const total = now - opened
  const share = opened === 0 ? 0 : total / opened

  return (
    <Figures>
      <Figure name="equity (last reading)" value={dollars(now)} hero />
      <Figure
        name="profit since the start"
        value={`${signed(total)} (${percent(share)})`}
        tone={total > 0 ? 'gain' : total < 0 ? 'loss' : undefined}
        icon={total > 0 ? ArrowUpRight : total < 0 ? ArrowDownRight : Minus}
        hero
      />
    </Figures>
  )
}

// `runs` and `failed` used to stand here and both were wrong: they counted a list
// the record returns with a ceiling on it, so a week of 152 runs showed as 50. The
// four that remain are whole - three come from the broker, and the intents are
// every one there has been.
//
// One to a card, because a row of bare numbers makes a reader guess what each is
// counting. `filled` used to be green: on every other page here green means money,
// and a count of orders is not money.
function Counters({ state, money }: { state: State; money?: Money }) {
  const counted: [string, string, string][] = [
    [
      'orders sent',
      String(money?.orders?.length ?? 0),
      'Everything the broker received, cancellations included.',
    ],
    [
      'filled',
      String(money?.orders?.filter((order) => order.status === 'filled').length ?? 0),
      'What the book actually took.',
    ],
    [
      'intents',
      String((state.intents ?? []).length),
      'Filed before an order could exist: no intent, no order.',
    ],
    [
      'positions',
      String(money?.positions?.length ?? 0),
      'Open right now, valued at the broker’s own mark.',
    ],
  ]

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
      {counted.map(([name, value, says]) => (
        <Card key={name}>
          <p className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">{name}</p>
          <p className="mt-2 text-[32px] font-medium leading-none tracking-[-0.02em] text-primary tabular-nums">
            {value}
          </p>
          <p className="mt-3 text-[14px] leading-snug text-secondary">{says}</p>
        </Card>
      ))}
    </div>
  )
}

// THE ANSWER AS IT ARRIVES, in the shape it arrives in.
//
// It used to be a list with an eye beside each rule, headed by the agent's own
// identity and the tool name - `alpaca-agent-1`, `PLACE_OPTION_ORDER` - neither of
// which means anything to a reader who has not read our code. What is left is the
// answer itself, and it is already technical: hyphenated rule names, a unit, a
// range with braces. Setting it as what it is says more than dressing it down.
//
// `disclosure` is the field this whole section is about, and in YAML it says
// itself: `existence` where a number is withheld, `boundary` where one is given -
// and the withheld rule visibly has no `value` line at all.
//
// Built from the live answer, not written here. The one thing shortened is the
// list of underlyings, and the comment above it says by how much.
function asYaml(limits: Limits): string {
  const lines = [`ruleset: "${limits.ruleset_version}"`, 'constraints:']

  for (const rule of limits.constraints ?? []) {
    lines.push(`  - rule: ${rule.rule}`)
    lines.push(`    disclosure: ${rule.disclosure}`)

    if (rule.value !== undefined) {
      if (Array.isArray(rule.value)) {
        lines.push(`    # ${rule.value.length} names, four shown`)
        lines.push(`    value: [${rule.value.slice(0, 4).join(', ')}]`)
      } else if (typeof rule.value === 'object') {
        lines.push(`    value: ${JSON.stringify(rule.value)}`)
      } else {
        lines.push(`    value: ${rule.value}`)
      }
    }
    if (rule.unit) lines.push(`    unit: ${rule.unit}`)
    if (rule.says) lines.push(`    says: "${rule.says}"`)
  }

  return lines.join('\n')
}

function LimitsCard({ limits }: { limits: Limits }) {
  return (
    <Yaml
      title="risk-engine.yaml"
      // WHETHER THE ENGINE IS ANSWERING AT ALL, in the title bar beside the file
      // it answers with. `ungoverned` is the state that matters - it means the
      // agent is running with nothing refusing it - and both take the panel's own
      // green and red rather than the page's: `--gain` is mixed for a white ground
      // and goes muddy on #0d1117.
      aside={
        <span
          className={`inline-flex items-center gap-2 font-mono text-[12px] ${
            limits.governed ? 'text-code-ok' : 'text-code-alarm'
          }`}
        >
          <span className="size-1.5 rounded-full bg-current" aria-hidden />
          {limits.governed ? 'governed' : 'ungoverned'}
        </span>
      }
      source={asYaml(limits)}
    />
  )
}

// A TABLE, because these are six rows of the same four measurements and a list
// made a reader parse each one out of a sentence. `edge` is the column that
// decides - the number the entry rule is read against - so it is last, where the
// eye lands, and a negative one is coloured.
function SweepCard({ sweep }: { sweep: Sweep }) {
  const candidates = sweep.candidates ?? []
  if (candidates.length === 0) return <Empty says="no sweep yet, or it found nothing" />

  const shown = candidates.slice(0, 6)

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center gap-x-3 gap-y-2 text-xs text-muted">
        <span className="font-medium text-primary">
          {candidates.length} priced
          {shown.length < candidates.length ? ` · the ${shown.length} best shown` : ''}
        </span>
        <Chip>swept {ago(sweep.taken_at)}</Chip>
        {marketOpen() ? null : <Chip>market closed — the screener runs while it is open</Chip>}
      </div>

      <Table
        head={['underlying', 'structure', 'credit', 'risk', 'edge']}
        rows={shown.map((one) => [
          <span className="font-medium">{one.underlying}</span>,
          <span className="text-secondary">
            {one.type} {one.short_strike}/{one.long_strike}
          </span>,
          <span className="tabular-nums">{one.credit.toFixed(2)}</span>,
          <span className="tabular-nums">{one.risk.toFixed(2)}</span>,
          one.edge_points === undefined ? (
            <span className="text-muted">—</span>
          ) : (
            <span
              className={`font-medium tabular-nums ${
                one.edge_points < 0 ? 'text-loss' : 'text-primary'
              }`}
            >
              {one.edge_points.toFixed(1)}
            </span>
          ),
        ])}
        empty="no sweep yet, or it found nothing"
      />
    </div>
  )
}

function Positions({ money }: { money: Money }) {
  return (
    <Table
      head={['symbol', 'side', 'quantity', 'entry', 'now', 'value', 'open profit']}
      rows={(money.positions ?? []).map((position) => [
        position.symbol,
        position.side,
        trim(position.quantity),
        dollars(position.average_entry_price),
        dollars(position.current_price),
        dollars(position.market_value),
        <span className={position.unrealized_pl >= 0 ? 'text-gain' : 'text-loss'}>
          {signed(position.unrealized_pl)} ({percent(position.unrealized_pl_fraction)})
        </span>,
      ])}
      empty="holding nothing right now"
    />
  )
}

function Turns({ state }: { state: State }) {
  const box = useRef<HTMLDivElement>(null)
  // Whether the reader is holding at the bottom edge. It decides whether the feed
  // scrolls on an update: somebody who went up to read the history must not be
  // yanked, while somebody watching the newest lines needs the scroll.
  const atBottom = useRef(true)

  useEffect(() => {
    const node = box.current
    if (node && atBottom.current) node.scrollTop = node.scrollHeight
  }, [state.turns?.length, state.said?.length])

  const turns = state.turns ?? []
  if (turns.length === 0) return <Empty says="no runs yet: nothing has woken it" />

  // The lines arrive newest first - that is how the last dozen are fetched. But
  // inside one turn they are a piece of reasoning, and it has to be read from the
  // start: otherwise the conclusion stands first and the premise last.
  const saidByTurn = new Map<string, Said[]>()
  for (const line of state.said ?? []) {
    saidByTurn.set(line.turn_ref, [...(saidByTurn.get(line.turn_ref) ?? []), line])
  }
  for (const lines of saidByTurn.values()) {
    lines.sort((a, b) => a.at.localeCompare(b.at))
  }

  const callsByTurn = new Map<string, ToolCall[]>()
  for (const call of state.calls ?? []) {
    callsByTurn.set(call.turn_ref, [...(callsByTurn.get(call.turn_ref) ?? []), call])
  }

  // A turn is woken once and then told more things while it runs, so what caused
  // it is a list. They arrive in order and stay in it.
  const causesByTurn = new Map<string, TurnCause[]>()
  for (const cause of state.causes ?? []) {
    causesByTurn.set(cause.turn_ref, [...(causesByTurn.get(cause.turn_ref) ?? []), cause])
  }

  // It reads like a conversation: old at the top, newest at the bottom. The
  // record arrives newest first, so it is reversed here.
  const inOrder = [...turns].reverse()

  return (
    <div
      ref={box}
      onScroll={(event) => {
        const node = event.currentTarget
        atBottom.current = node.scrollHeight - node.scrollTop - node.clientHeight < 80
      }}
      className="max-h-[70vh] overflow-y-auto rounded-xl border border-line bg-surface p-3"
    >
      <ol className="m-0 flex list-none flex-col gap-2.5 p-0">
        {inOrder.map((turn) => (
          <li key={turn.ref}>
            <TurnCard
              turn={turn}
              causes={causesByTurn.get(turn.ref) ?? []}
              said={saidByTurn.get(turn.ref) ?? []}
              calls={callsByTurn.get(turn.ref) ?? []}
            />
          </li>
        ))}
      </ol>
    </div>
  )
}

function TurnCard({
  turn,
  causes,
  said,
  calls,
}: {
  turn: Turn
  causes: TurnCause[]
  said: Said[]
  calls: ToolCall[]
}) {
  const state = turn.failure
    ? { text: turn.failure, colour: 'text-loss', Icon: CircleAlert }
    : turn.finished_at
      ? { text: took(turn.started_at, turn.finished_at), colour: '', Icon: CircleCheck }
      : { text: 'running', colour: 'text-gain', Icon: LoaderCircle }

  const refused = calls.filter((call) => call.status !== 'completed').length

  return (
    <div className="rounded-lg border border-line bg-surface-raised px-4 py-3">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 font-mono text-[11px] text-muted">
        <span>{clock(turn.started_at)}</span>
        {/* No name rather than a placeholder. `unknown` told a reader nothing and
            read as a fault; a turn whose cause did not arrive still has its time,
            its length and its words, and those say more than the word does. */}
        {causes[0]?.woken_by ? (
          <span className="font-medium text-primary">{causes[0].woken_by}</span>
        ) : null}
        {causes.slice(1).map((cause) => (
          <Chip key={cause.id}>
            {'+ '}
            {cause.woken_by}
          </Chip>
        ))}
        <Chip tone={turn.failure ? 'loss' : turn.finished_at ? undefined : 'gain'}>
          <state.Icon className={`size-3 ${turn.finished_at || turn.failure ? '' : 'animate-spin'}`} />
          {state.text}
        </Chip>
        {calls.length > 0 ? (
          <Chip tone={refused > 0 ? 'loss' : undefined}>
            {calls.length} calls
            {refused > 0 ? ` · ${refused} refused` : ''}
          </Chip>
        ) : null}
      </div>

      {causes.map((cause) => (
        <p key={cause.id} className="mt-2 text-sm text-secondary">
          {cause.cause}
        </p>
      ))}

      {/* Its own words. They are what the page is opened for: anyone can show a
          curve, few can show the decision in words. They are separated from the
          cause by text colour rather than a rule: the cause grey, the words
          black. */}
      {said.map((line, index) => (
        <div key={index} className="mt-3 text-[15px] leading-relaxed text-primary">
          <Spoken text={line.text} />
        </div>
      ))}
    </div>
  )
}

function Spoken({ text }: { text: string }) {
  // Lines are gathered into blocks: consecutive items into one list, everything
  // else into a paragraph keeping the line breaks the agent put there.
  const blocks: Array<{ list: boolean; lines: string[] }> = []
  for (const line of text.split('\n')) {
    const bullet = /^\s*[-*]\s+/.test(line)
    const last = blocks[blocks.length - 1]
    if (last && last.list === bullet) last.lines.push(line)
    else blocks.push({ list: bullet, lines: [line] })
  }

  return (
    <>
      {blocks.map((block, index) =>
        block.list ? (
          <ul key={index} className="my-1.5 list-disc space-y-1 pl-5">
            {block.lines.map((line, at) => (
              <li key={at}>{inline(line.replace(/^\s*[-*]\s+/, ''))}</li>
            ))}
          </ul>
        ) : (
          <p key={index} className="whitespace-pre-wrap">
            {inline(block.lines.join('\n'))}
          </p>
        ),
      )}
    </>
  )
}

// A list of two hundred underlyings fills the screen and says nothing. The start
// of the list says everything: that it exists and what kind of thing is in it.
