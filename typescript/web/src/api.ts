// Что страница спрашивает у своей половины и в каком виде получает.
//
// Пять отдельных запросов, а не один общий, и это выбор. Общий ответ дал бы один
// отказ на всё: брокер за сутки падал трижды, и в такую минуту страница не
// показала бы НИЧЕГО - ни решений агента, ни пределов, ни кривой. Пять запросов
// дают пять независимых отказов, каждый со своей надписью. Браузер шлёт их
// параллельно, во времени это ничего не стоит.

const readSide = import.meta.env.VITE_STATE_URL ?? ''

export type Account = {
  equity: number
  last_equity: number
  cash: number
  buying_power: number
  status: string
}

export type Position = {
  symbol: string
  side: string
  quantity: number
  average_entry_price: number
  current_price: number
  market_value: number
  unrealized_pl: number
  unrealized_pl_fraction: number
}

export type Order = {
  id: string
  symbol: string
  side: string
  status: string
  quantity: number
  notional: number
  submitted_at: string
  filled_price?: number
}

export type Money = { account: Account; positions: Position[]; orders: Order[] }
export type Snapshot = { recorded_at: string; equity: number }

export type Turn = {
  ref: string
  started_at: string
  finished_at?: string
  woken_by: string
  cause: string
  model?: string
  failure?: string
}

export type Intent = {
  at: string
  session: string
  thesis: string
  underlying?: string
}

export type ToolCall = {
  turn_ref: string
  server: string
  tool: string
  status: string
  failure?: string
  answer?: string
  started_at: string
}

// Что агент СКАЗАЛ внутри хода. Вызовы показывают, что он делал; это - почему.
export type Said = { turn_ref: string; at: string; text: string }

export type State = { turns: Turn[]; calls: ToolCall[]; intents: Intent[]; said: Said[] }

export type Constraint = {
  rule: string
  disclosure: string
  value?: unknown
  unit?: string
}

export type Limits = {
  tool: string
  identity: string
  ruleset_version: string
  governed: boolean
  constraints: Constraint[]
}

export type Candidate = {
  underlying: string
  type: string
  short_strike: number
  long_strike: number
  credit: number
  risk: number
  edge_points?: number
  short_delta?: number
}

export type Sweep = { candidates: Candidate[]; taken_at: string }

export type Answer<T> = { ok: true; value: T } | { ok: false; why: string }

async function read<T>(route: string): Promise<Answer<T>> {
  try {
    const answer = await fetch(`${readSide}${route}`)
    if (!answer.ok) {
      // Текст отказа читается человеком, поэтому показывается он, а не код.
      return { ok: false, why: (await answer.text()).trim() || `${route}: ${answer.status}` }
    }

    return { ok: true, value: (await answer.json()) as T }
  } catch (trouble) {
    return { ok: false, why: `${route}: ${String(trouble)}` }
  }
}

export type Everything = {
  money: Answer<Money>
  equity: Answer<Snapshot[]>
  state: Answer<State>
  limits: Answer<Limits>
  sweep: Answer<Sweep>
}

export async function readEverything(): Promise<Everything> {
  const [money, equity, state, limits, sweep] = await Promise.all([
    read<Money>('api/money'),
    read<Snapshot[]>('api/equity'),
    read<State>('api/state'),
    read<Limits>('api/limits'),
    read<Sweep>('api/sweep'),
  ])

  return { money, equity, state, limits, sweep }
}
