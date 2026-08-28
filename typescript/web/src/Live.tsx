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
import { useEffect, useState } from 'react'
import { Link } from 'react-router'

import type { Everything, Limits, Money, Said, State, Sweep, ToolCall, Turn } from './api'
import { readEverything } from './api'
import { Equity } from './Equity'
import { ago, clock, dollars, percent, signed, took, trim } from './format'
import { Card, Chip, Empty, Eyebrow, Figure, Figures, Section, Table, Unavailable } from './parts'

// Раз в пятнадцать секунд. Не поток: данные и меняются раз в минуту-две, а
// поток стоит сложности, которая на неделе не окупится.
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
        <div className="flex flex-wrap items-center justify-between gap-4">
          <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>
          <Link
            to="/"
            className="font-mono text-xs uppercase tracking-[0.04em] text-muted hover:text-primary"
          >
            ← about
          </Link>
        </div>

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
        {all.money.ok ? <Account money={all.money.value} /> : <Unavailable why={all.money.why} />}
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
        explains="None of this is written into the agent\u0027s instructions. It asks what it may do while it works, and this is the same answer it gets — down to the rule that admits it exists and withholds its number."
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

function Account({ money }: { money: Money }) {
  const change = money.account.equity - money.account.last_equity
  const fraction = money.account.last_equity === 0 ? 0 : change / money.account.last_equity

  return (
    <Figures>
      {/* Число, ради которого страницу открывают. Одно на всю страницу. */}
      <Figure name="equity" value={dollars(money.account.equity)} hero />
      {/* Стрелка не украшение: цвет один не читается теми, кто его не
          различает, и второй признак это чинит. */}
      <Figure
        name="since yesterday"
        value={`${signed(change)} (${percent(fraction)})`}
        tone={change > 0 ? 'gain' : change < 0 ? 'loss' : undefined}
        icon={change > 0 ? ArrowUpRight : change < 0 ? ArrowDownRight : Minus}
      />
      <Figure name="cash" value={dollars(money.account.cash)} />
      <Figure name="buying power" value={dollars(money.account.buying_power)} />
    </Figures>
  )
}

function Counters({ state, money }: { state: State; money?: Money }) {
  const refused = state.turns.filter((turn) => turn.failure).length
  const sent = money?.orders.length ?? 0
  const filled = money?.orders.filter((order) => order.status === 'filled').length ?? 0

  return (
    <Figures>
      <Figure name="runs" value={String(state.turns.length)} />
      <Figure name="failed" value={String(refused)} tone={refused > 0 ? 'loss' : undefined} />
      <Figure name="orders sent" value={String(sent)} />
      <Figure name="filled" value={String(filled)} tone={filled > 0 ? 'gain' : undefined} />
      <Figure name="intents" value={String(state.intents.length)} />
      <Figure name="positions" value={String(money?.positions.length ?? 0)} />
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
        {limits.constraints.map((rule) => (
          <li key={rule.rule} className="flex items-baseline gap-2 text-sm">
            {/* Открытый глаз - число названо; перечёркнутый - правило сообщает,
                что существует, и не выдаёт числа. Это и есть механика, которую
                мы показываем, и она должна читаться с одного взгляда. */}
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <Eye aria-label="number disclosed" className="mt-0.5 size-3.5 shrink-0 text-muted" />
            ) : (
              <EyeOff aria-label="number withheld" className="mt-0.5 size-3.5 shrink-0 text-muted" />
            )}
            <span>
            <span className="text-muted">{rule.rule}: </span>
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <span>
                {shorten(JSON.stringify(rule.value))}
                {rule.unit ? ` ${rule.unit}` : ''}
              </span>
            ) : (
              // Правило, которое сообщает, что СУЩЕСТВУЕТ, и не выдаёт числа.
              // Это не пробел в данных, а степень раскрытия, и её видно.
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
  if (sweep.candidates.length === 0) return <Empty says="no sweep yet, or it found nothing" />

  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 text-xs text-muted">
        <span className="font-medium text-primary">
          {sweep.candidates.length} structures
        </span>
        <Chip>swept {ago(sweep.taken_at)}</Chip>
      </div>
      <ul className="mt-3 space-y-1.5 text-sm">
        {sweep.candidates.slice(0, 6).map((one) => (
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
      rows={money.positions.map((position) => [
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
  if (state.turns.length === 0) return <Empty says="no runs yet: nothing has woken it" />

  const saidByTurn = new Map<string, Said[]>()
  for (const line of state.said ?? []) {
    saidByTurn.set(line.turn_ref, [...(saidByTurn.get(line.turn_ref) ?? []), line])
  }

  const callsByTurn = new Map<string, ToolCall[]>()
  for (const call of state.calls ?? []) {
    callsByTurn.set(call.turn_ref, [...(callsByTurn.get(call.turn_ref) ?? []), call])
  }

  return (
    <ol className="m-0 flex list-none flex-col gap-3 p-0">
      {state.turns.map((turn) => (
        <li key={turn.ref}>
          <TurnCard
            turn={turn}
            said={saidByTurn.get(turn.ref) ?? []}
            calls={callsByTurn.get(turn.ref) ?? []}
          />
        </li>
      ))}
    </ol>
  )
}

function TurnCard({ turn, said, calls }: { turn: Turn; said: Said[]; calls: ToolCall[] }) {
  const state = turn.failure
    ? { text: turn.failure, colour: 'text-loss', Icon: CircleAlert }
    : turn.finished_at
      ? { text: took(turn.started_at, turn.finished_at), colour: '', Icon: CircleCheck }
      : { text: 'running', colour: 'text-gain', Icon: LoaderCircle }

  const refused = calls.filter((call) => call.status !== 'completed').length

  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs text-muted">
        <span>{clock(turn.started_at)}</span>
        <span className="font-medium text-primary">{turn.woken_by}</span>
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

      <p className="mt-2 text-sm text-secondary">{turn.cause}</p>

      {/* Его собственные слова. Отбиты слева, потому что это граница между тем,
          что записала система, и тем, что сказал агент. Ради этой полосы
          страница и открывается: кривую покажет любой, решение словами — мало кто. */}
      {said.map((line, index) => (
        <p
          key={index}
          className="mt-3 whitespace-pre-wrap border-l-2 border-accent-ink pl-3.5 text-[15px] leading-relaxed text-primary"
        >
          {line.text}
        </p>
      ))}
    </Card>
  )
}

// Список из двухсот бумаг занимает экран и ничего не сообщает. Начало списка
// сообщает всё: что он есть и какого рода.
function shorten(value: string): string {
  return value.length > 90 ? `${value.slice(0, 90)}…` : value
}
