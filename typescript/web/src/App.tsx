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

import type { Everything, Limits, Money, Said, State, Sweep, ToolCall, Turn } from './api'
import { readEverything } from './api'
import { Equity } from './Equity'
import { ago, clock, dollars, percent, signed, took, trim } from './format'
import { Card, Empty, Figure, Section, Table, Unavailable } from './parts'

// Раз в пятнадцать секунд. Не поток: данные и меняются раз в минуту-две, а
// поток стоит сложности, которая на неделе не окупится.
const refreshEvery = 15_000

export function App() {
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
    <main className="mx-auto max-w-6xl px-6 pb-24 pt-10 text-neutral-900 dark:text-neutral-100">
      <header className="mb-10">
        <h1 className="text-xs font-semibold uppercase tracking-[0.16em] text-neutral-500 dark:text-neutral-400">
          Опционный агент
        </h1>
        <p className="mt-3 max-w-2xl text-lg leading-relaxed text-neutral-700 dark:text-neutral-300">
          Самостоятельный агент, торгующий опционами на бумажном счёте Alpaca. Когда ему
          работать, решает расписание; что торговать — решает он сам, и прежде чем отправить
          заявку, говорит, что собирается сделать.
        </p>
        <p className="mt-3 text-xs text-neutral-500 dark:text-neutral-400">
          {readAt ? `прочитано в ${clock(readAt.toISOString())}` : 'читаю…'}
          {failed.length > 0 ? ` · не ответило: ${failed.join('; ')}` : ''}
        </p>
      </header>

      {all ? <Page all={all} /> : <Empty says="читаю…" />}
    </main>
  )
}

function Page({ all }: { all: Everything }) {
  const state = all.state.ok ? all.state.value : undefined
  const money = all.money.ok ? all.money.value : undefined

  return (
    <>
      <Section title="Счёт">
        {all.money.ok ? <Account money={all.money.value} /> : <Unavailable why={all.money.why} />}
        <div className="mt-4">
          {all.equity.ok ? <Equity line={all.equity.value} /> : <Unavailable why={all.equity.why} />}
        </div>
      </Section>

      {state ? (
        <Section title="Одним взглядом" explains="Неделя в числах: как часто агент просыпался, что отправил, что держит.">
          <Counters state={state} money={money} />
        </Section>
      ) : null}

      <Section
        title="Пределы, которые он обнаружил"
        explains="Ничего из этого не вписано в инструкции агента. Он спрашивает свои пределы во время работы, и здесь тот же ответ, который получает он."
      >
        {all.limits.ok ? <LimitsCard limits={all.limits.value} /> : <Unavailable why={all.limits.why} />}
      </Section>

      <Section
        title="Что предлагает рынок"
        explains="Скринер оценивает весь разрешённый список бумаг снова и снова. Это его последний проход."
      >
        {all.sweep.ok ? <SweepCard sweep={all.sweep.value} /> : <Unavailable why={all.sweep.why} />}
      </Section>

      <Section title="Открытые позиции" explains="Что агент держит прямо сейчас, в оценке брокера.">
        {money ? <Positions money={money} /> : <Empty says="счёт недоступен" />}
      </Section>

      <Section
        title="Ходы"
        explains="Один запуск агента: когда, чем разбужен, сколько занял — и к чему он пришёл, его собственными словами."
      >
        {state ? <Turns state={state} /> : <Empty says="запись недоступна" />}
      </Section>
    </>
  )
}

function Account({ money }: { money: Money }) {
  const change = money.account.equity - money.account.last_equity
  const fraction = money.account.last_equity === 0 ? 0 : change / money.account.last_equity

  return (
    <dl className="m-0 flex flex-wrap gap-x-10 gap-y-6 rounded-xl border border-neutral-200 bg-white px-6 py-5 dark:border-neutral-800 dark:bg-neutral-900">
      {/* Число, ради которого страницу открывают. Оно и должно быть крупнейшим. */}
      <div className="flex flex-col gap-1">
        <dt className="text-[0.68rem] font-medium uppercase tracking-[0.1em] text-neutral-500 dark:text-neutral-400">
          капитал
        </dt>
        <dd className="text-4xl font-semibold leading-none tracking-tight">
          {dollars(money.account.equity)}
        </dd>
      </div>
      {/* Стрелка не украшение: цвет один не читается теми, кто его не
          различает, и второй признак это чинит. */}
      <Figure
        name="со вчерашнего закрытия"
        value={`${signed(change)} (${percent(fraction)})`}
        tone={change > 0 ? 'gain' : change < 0 ? 'loss' : undefined}
        icon={change > 0 ? ArrowUpRight : change < 0 ? ArrowDownRight : Minus}
      />
      <Figure name="наличные" value={dollars(money.account.cash)} />
      <Figure name="покупательная способность" value={dollars(money.account.buying_power)} />
    </dl>
  )
}

function Counters({ state, money }: { state: State; money?: Money }) {
  const refused = state.turns.filter((turn) => turn.failure).length
  const sent = money?.orders.length ?? 0
  const filled = money?.orders.filter((order) => order.status === 'filled').length ?? 0

  return (
    <dl className="m-0 flex flex-wrap gap-x-10 gap-y-6 rounded-xl border border-neutral-200 bg-white px-6 py-5 dark:border-neutral-800 dark:bg-neutral-900">
      <Figure name="ходов" value={String(state.turns.length)} />
      <Figure name="с отказом" value={String(refused)} tone={refused > 0 ? 'loss' : undefined} />
      <Figure name="заявок отправлено" value={String(sent)} />
      <Figure name="исполнено" value={String(filled)} tone={filled > 0 ? 'gain' : undefined} />
      <Figure name="намерений" value={String(state.intents.length)} />
      <Figure name="открыто позиций" value={String(money?.positions.length ?? 0)} />
    </dl>
  )
}

function LimitsCard({ limits }: { limits: Limits }) {
  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs text-neutral-500 dark:text-neutral-400">
        <span className="font-medium text-neutral-900 dark:text-neutral-100">{limits.identity}</span>
        <span>{limits.tool}</span>
        <span>правила {limits.ruleset_version}</span>
        <span className={limits.governed ? 'text-gain' : 'text-loss'}>
          {limits.governed ? 'под правилами' : 'без правил'}
        </span>
      </div>
      <ul className="mt-3 space-y-1.5">
        {limits.constraints.map((rule) => (
          <li key={rule.rule} className="flex items-baseline gap-2 text-sm">
            {/* Открытый глаз - число названо; перечёркнутый - правило сообщает,
                что существует, и не выдаёт числа. Это и есть механика, которую
                мы показываем, и она должна читаться с одного взгляда. */}
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <Eye aria-label="число раскрыто" className="mt-0.5 size-3.5 shrink-0 text-neutral-400" />
            ) : (
              <EyeOff aria-label="число не раскрыто" className="mt-0.5 size-3.5 shrink-0 text-neutral-400" />
            )}
            <span>
            <span className="text-neutral-500 dark:text-neutral-400">{rule.rule}: </span>
            {rule.disclosure === 'boundary' && rule.value !== undefined ? (
              <span>
                {shorten(JSON.stringify(rule.value))}
                {rule.unit ? ` ${rule.unit}` : ''}
              </span>
            ) : (
              // Правило, которое сообщает, что СУЩЕСТВУЕТ, и не выдаёт числа.
              // Это не пробел в данных, а степень раскрытия, и её видно.
              <span className="italic text-neutral-500 dark:text-neutral-400">
                {rule.disclosure === 'existence' ? 'существует, число не раскрыто' : 'не раскрыто'}
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
  if (sweep.candidates.length === 0) return <Empty says="прохода ещё не было или он ничего не нашёл" />

  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 text-xs text-neutral-500 dark:text-neutral-400">
        <span className="font-medium text-neutral-900 dark:text-neutral-100">
          {sweep.candidates.length} конструкций
        </span>
        <span>проход {ago(sweep.taken_at)}</span>
      </div>
      <ul className="mt-3 space-y-1.5 text-sm">
        {sweep.candidates.slice(0, 6).map((one) => (
          <li key={`${one.underlying}${one.type}${one.short_strike}${one.long_strike}`}>
            <span className="font-medium">{one.underlying}</span>{' '}
            <span className="text-neutral-500 dark:text-neutral-400">
              {one.type} {one.short_strike}/{one.long_strike}
            </span>{' '}
            — кредит {one.credit.toFixed(2)} против риска {one.risk.toFixed(2)}
            {one.edge_points === undefined ? '' : `, преимущество ${one.edge_points.toFixed(1)}`}
          </li>
        ))}
      </ul>
    </Card>
  )
}

function Positions({ money }: { money: Money }) {
  return (
    <Table
      head={['бумага', 'сторона', 'количество', 'вход', 'сейчас', 'стоимость', 'открытая прибыль']}
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
      empty="сейчас ничего не держит"
    />
  )
}

function Turns({ state }: { state: State }) {
  if (state.turns.length === 0) return <Empty says="ходов ещё не было: агента ничто не будило" />

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
      : { text: 'идёт', colour: 'text-gain', Icon: LoaderCircle }

  const refused = calls.filter((call) => call.status !== 'completed').length

  return (
    <Card>
      <div className="flex flex-wrap items-baseline gap-x-4 gap-y-1 text-xs text-neutral-500 dark:text-neutral-400">
        <span>{clock(turn.started_at)}</span>
        <span className="font-medium text-neutral-900 dark:text-neutral-100">{turn.woken_by}</span>
        <span className={`inline-flex items-center gap-1.5 ${state.colour}`}>
          <state.Icon className={`size-3.5 ${turn.finished_at || turn.failure ? '' : 'animate-spin'}`} />
          {state.text}
        </span>
        {calls.length > 0 ? (
          <span>
            вызовов {calls.length}
            {refused > 0 ? `, отказов ${refused}` : ''}
          </span>
        ) : null}
      </div>

      <p className="mt-2 text-sm text-neutral-700 dark:text-neutral-300">{turn.cause}</p>

      {/* Его собственные слова. Отбиты слева, потому что это граница между тем,
          что записала система, и тем, что сказал агент. Ради этой полосы
          страница и открывается: кривую покажет любой, решение словами — мало кто. */}
      {said.map((line, index) => (
        <p
          key={index}
          className="mt-3 whitespace-pre-wrap border-l-2 border-neutral-900 pl-3 text-[0.94rem] leading-relaxed dark:border-neutral-100"
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
