import {
  ArrowDownRight,
  ArrowUpRight,
  CircleAlert,
  CircleCheck,
  Eye,
  EyeOff,
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
import { ago, clock, dollars, percent, signed, took, trim } from './format'
import {
  Card,
  Chip,
  Empty,
  Eyebrow,
  Figure,
  Figures,
  inline,
  Section,
  Table,
  Unavailable,
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
          <Chip tone={failed.length > 0 ? 'loss' : 'gain'}>
            <span
              className={`size-1.5 rounded-full ${failed.length > 0 ? 'bg-loss' : 'animate-pulse bg-gain'}`}
              aria-hidden
            />
            {readAt ? `read ${clock(readAt.toISOString())}` : 'reading…'}
          </Chip>
          {failed.map((why) => (
            <Chip key={why} tone="loss">
              {why}
            </Chip>
          ))}
        </p>
      </header>

      {all ? <Page all={all} /> : <Empty says="reading…" />}
    </main>
  )
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
        explains="The week in numbers: how often it woke, what it sent, what it holds.">
          <Counters state={state} money={money} />
        </Section>
      ) : null}

      <Section
        title="Limits it discovered"
        explains="None of this is written into the agent’s instructions. It asks what it may do while it works, and this is the same answer it gets — down to the rule that admits it exists and withholds its number."
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
        explains="When it ran, what woke it, what it asked — and what it concluded. The runs where it looked and did nothing are here too, and they say why."
      >
        {state ? <Turns state={state} /> : <Empty says="the record is unavailable" />}
      </Section>
    </>
  )
}

function Account({ money, line }: { money: Money; line?: Snapshot[] }) {
  const yesterday = money.account.equity_yesterday
  const day = yesterday === undefined ? undefined : money.account.equity - yesterday
  const dayShare = yesterday ? (day ?? 0) / yesterday : 0

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
    <Figures>
      {/* The number the page is opened for. One on the whole page. */}
      <Figure name="equity" value={dollars(money.account.equity)} hero />
      {total === undefined ? null : (
        <Figure
          name="profit since the start"
          value={`${signed(total)} (${percent(totalShare)})`}
          tone={total > 0 ? 'gain' : total < 0 ? 'loss' : undefined}
          icon={total > 0 ? ArrowUpRight : total < 0 ? ArrowDownRight : Minus}
          hero
        />
      )}
      {/* The arrow is not decoration: colour alone is unreadable to those who do
          not distinguish it, and a second cue fixes that. */}
      {day === undefined ? null : (
        <Figure
          name="today"
          value={`${signed(day)} (${percent(dayShare)})`}
          tone={day > 0 ? 'gain' : day < 0 ? 'loss' : undefined}
          icon={day > 0 ? ArrowUpRight : day < 0 ? ArrowDownRight : Minus}
        />
      )}
      <Figure name="cash" value={dollars(money.account.cash)} />
      <Figure name="buying power" value={dollars(money.account.buying_power)} />
    </Figures>
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

function Counters({ state, money }: { state: State; money?: Money }) {
  const refused = (state.turns ?? []).filter((turn) => turn.failure).length
  const sent = money?.orders?.length ?? 0
  const filled = money?.orders?.filter((order) => order.status === 'filled').length ?? 0

  return (
    <Figures>
      <Figure name="runs" value={String((state.turns ?? []).length)} />
      <Figure name="failed" value={String(refused)} tone={refused > 0 ? 'loss' : undefined} />
      <Figure name="orders sent" value={String(sent)} />
      <Figure name="filled" value={String(filled)} tone={filled > 0 ? 'gain' : undefined} />
      <Figure name="intents" value={String((state.intents ?? []).length)} />
      <Figure name="positions" value={String(money?.positions?.length ?? 0)} />
    </Figures>
  )
}

function LimitsCard({ limits }: { limits: Limits }) {
  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs text-muted">
        <span className="font-medium text-primary">{limits.identity}</span>
        <Chip>{limits.tool}</Chip>
        <Chip>ruleset {limits.ruleset_version}</Chip>
        <Chip tone={limits.governed ? 'gain' : 'loss'}>
          {limits.governed ? 'governed' : 'ungoverned'}
        </Chip>
      </div>
      <ul className="mt-3 space-y-1.5">
        {(limits.constraints ?? []).map((rule) => (
          <li key={rule.rule} className="flex items-baseline gap-2 text-sm">
            {/* An open eye means the number is named; a struck-through one means
                the rule reports that it exists and discloses no number. That is
                the mechanism we are showing, and it must read at a glance. */}
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <Eye aria-label="number disclosed" className="mt-0.5 size-3.5 shrink-0 text-muted" />
            ) : (
              <EyeOff aria-label="number withheld" className="mt-0.5 size-3.5 shrink-0 text-muted" />
            )}
            <span className="min-w-0 break-words">
            <span className="text-muted">{rule.rule}: </span>
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <span>
                {shorten(JSON.stringify(rule.value))}
                {rule.unit ? ` ${rule.unit}` : ''}
              </span>
            ) : (
              // A rule that reports it EXISTS and discloses no number. This is
              // not a gap in the data but a degree of disclosure, and it shows.
              <span className="italic text-muted">
                {rule.disclosure === 'existence' ? 'exists · number withheld' : 'withheld'}
              </span>
            )}
            </span>
          </li>
        ))}
      </ul>
    </Card>
  )
}

function SweepCard({ sweep }: { sweep: Sweep }) {
  const candidates = sweep.candidates ?? []
  if (candidates.length === 0) return <Empty says="no sweep yet, or it found nothing" />

  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 text-xs text-muted">
        <span className="font-medium text-primary">
          {candidates.length} structures
        </span>
        <Chip>swept {ago(sweep.taken_at)}</Chip>
      </div>
      <ul className="mt-3 space-y-1.5 text-sm">
        {candidates.slice(0, 6).map((one) => (
          <li key={`${one.underlying}${one.type}${one.short_strike}${one.long_strike}`}>
            <span className="font-medium">{one.underlying}</span>{' '}
            <span className="text-muted">
              {one.type} {one.short_strike}/{one.long_strike}
            </span>{' '}
            — credit {one.credit.toFixed(2)} against risk {one.risk.toFixed(2)}
            {one.edge_points === undefined ? '' : `, edge ${one.edge_points.toFixed(1)}`}
          </li>
        ))}
      </ul>
    </Card>
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
        <span className="font-medium text-primary">{causes[0]?.woken_by ?? 'unknown'}</span>
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
function shorten(value: string): string {
  return value.length > 90 ? `${value.slice(0, 90)}…` : value
}
