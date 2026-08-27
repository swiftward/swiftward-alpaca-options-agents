// The demo page. It reads three things and decides nothing: /money is the
// account as the broker values it now, /equity is the line it drew over the
// week, /api/state is what the agent did and why.

type Account = {
  number: string
  status: string
  equity: number
  equity_yesterday: number
  cash: number
  buying_power: number
  options_buying_power: number
  position_market_value: number
}

type Position = {
  symbol: string
  asset_class: string
  side: string
  quantity: number
  average_entry_price: number
  current_price: number
  market_value: number
  unrealized_pl: number
  unrealized_pl_fraction: number
}

type Order = {
  id: string
  symbol: string
  side: string
  type: string
  class: string
  status: string
  position_intent: string
  quantity: number
  notional: number
  filled_quantity: number
  limit_price: number
  filled_price: number
  submitted_at?: string
  filled_at?: string
  canceled_at?: string
  legs?: Order[]
}

type Money = { account: Account; positions: Position[]; orders: Order[] }

type Snapshot = { recorded_at: string; equity: number }

type Turn = {
  ref: string
  started_at: string
  finished_at?: string
  woken_by: string
  cause: string
  model?: string
  failure?: string
}

type Intent = {
  at: string
  turn_ref: string
  session: string
  thesis: string
  structure: string
  max_loss: string
}

type ToolCall = {
  ref: string
  turn_ref: string
  server: string
  tool: string
  arguments?: unknown
  started_at: string
  finished_at?: string
  status: string
  failure?: string
  answer?: string
}

type State = { turns: Turn[]; calls: ToolCall[]; intents: Intent[] }

// Same origin by default: the page is served by the read side that answers these
// routes. A deployment that puts the page elsewhere sets this to that address.
//
// The routes below are RELATIVE - `api/money`, not `/api/money` - so the page
// works wherever it is mounted. The public address has to share a port with
// something else: a Tailscale funnel is allowed on three ports and all three are
// taken, so the dashboard hangs off a path. Measured 27 August: the mount prefix
// is stripped before the request reaches us, in both target forms, so a relative
// route arrives as the route this server serves and an absolute one would leave
// the mount entirely.
const readSide = import.meta.env.VITE_STATE_URL ?? ''

const refreshEvery = 15_000

async function render(): Promise<void> {
  const [money, equity, state] = await Promise.all([
    read<Money>('api/money'),
    read<Snapshot[]>('api/equity'),
    read<State>('api/state'),
  ])

  if (money.ok) {
    showAccount(money.value.account)
    fillTable('#positions', positionsTable(money.value.positions))
    fillTable('#orders', ordersTable(money.value.orders))
  } else {
    showUnavailable('#account-section', money.why)
  }

  if (equity.ok) drawEquity(equity.value)

  if (state.ok) {
    fillTable('#calls', callsTable(state.value.calls))
    fill('#intents', state.value.intents, intentCard, 'no intents yet: the agent has not planned an order')
    fill('#turns', state.value.turns, turnCard, 'no turns yet: nothing has woken the agent')
  }

  const failed = [money, equity, state].filter((answer) => !answer.ok)
  say(
    failed.length === 0
      ? `read at ${clock(new Date().toISOString())}`
      : `read at ${clock(new Date().toISOString())} — ${failed.map((answer) => answer.why).join('; ')}`,
  )
}

type Answer<T> = { ok: true; value: T } | { ok: false; why: string }

async function read<T>(route: string): Promise<Answer<T>> {
  try {
    const response = await fetch(`${readSide}${route}`)
    if (!response.ok) return { ok: false, why: `${route}: ${(await response.text()).trim() || response.status}` }
    return { ok: true, value: (await response.json()) as T }
  } catch (reason) {
    return { ok: false, why: `${route}: ${reason instanceof Error ? reason.message : String(reason)}` }
  }
}

function showAccount(account: Account): void {
  const day = account.equity - account.equity_yesterday
  const dayFraction = account.equity_yesterday === 0 ? 0 : day / account.equity_yesterday

  const list = document.querySelector('#account')!
  list.replaceChildren(
    figure('equity', dollars(account.equity)),
    figure('today', `${signed(day)} (${percent(dayFraction)})`, day === 0 ? '' : day > 0 ? 'up' : 'down'),
    figure('cash', dollars(account.cash)),
    figure('positions', dollars(account.position_market_value)),
    figure('options buying power', dollars(account.options_buying_power)),
    figure('account', `${account.number} · ${account.status.toLowerCase()}`),
  )
}

function figure(name: string, value: string, tone = ''): HTMLElement {
  const block = document.createElement('div')
  const term = document.createElement('dt')
  term.textContent = name
  const detail = document.createElement('dd')
  detail.textContent = value
  if (tone) detail.className = tone
  block.append(term, detail)
  return block
}

function drawEquity(line: Snapshot[]): void {
  const svg = document.querySelector('#equity')! as SVGElement
  const caption = document.querySelector('#equity-caption')!
  if (line.length < 2) {
    // An empty frame reads as a chart that failed to draw. Until there are two
    // readings there is no line, and the caption says why.
    svg.replaceChildren()
    svg.setAttribute('hidden', '')
    caption.textContent =
      line.length === 0
        ? 'no account history recorded yet'
        : 'one reading recorded so far: a line needs two'
    return
  }

  const values = line.map((point) => point.equity)
  const lowest = Math.min(...values)
  const highest = Math.max(...values)
  const span = highest - lowest
  const width = 720
  const height = 160

  const points = line.map((point, index) => {
    const x = (index / (line.length - 1)) * width
    // An account that has not moved draws through the middle. Scaled to a span of
    // zero it would lie along the floor and read as a loss of everything.
    const y = span === 0 ? height / 2 : height - ((point.equity - lowest) / span) * (height - 8) - 4
    return `${x.toFixed(1)},${y.toFixed(1)}`
  })

  svg.removeAttribute('hidden')
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'polyline')
  path.setAttribute('points', points.join(' '))
  path.setAttribute('class', values[values.length - 1] >= values[0] ? 'up' : 'down')
  svg.replaceChildren(path)

  caption.textContent =
    `${line.length} readings, ${clock(line[0].recorded_at)} to ${clock(line[line.length - 1].recorded_at)}` +
    (span === 0 ? ' · unchanged so far' : ` · low ${dollars(lowest)} · high ${dollars(highest)}`)
}

function positionsTable(positions: Position[]): { head: string[]; rows: string[][]; empty: string } {
  return {
    head: ['symbol', 'side', 'quantity', 'entry', 'now', 'value', 'open profit'],
    rows: positions.map((position) => [
      position.symbol,
      position.side,
      trim(position.quantity),
      dollars(position.average_entry_price),
      dollars(position.current_price),
      dollars(position.market_value),
      `${signed(position.unrealized_pl)} (${percent(position.unrealized_pl_fraction)})`,
    ]),
    empty: 'nothing held right now',
  }
}

function ordersTable(orders: Order[]): { head: string[]; rows: string[][]; empty: string } {
  const rows: string[][] = []
  for (const order of orders) {
    rows.push([
      when(order),
      order.legs?.length ? `${order.class} · ${order.legs.length} legs` : order.symbol,
      order.side,
      amount(order),
      order.limit_price === 0 ? order.type : price(order),
      order.status,
    ])
    for (const leg of order.legs ?? []) {
      rows.push(['', `↳ ${leg.symbol}`, leg.position_intent || leg.side, trim(leg.quantity), '', leg.status])
    }
  }

  return { head: ['sent', 'what', 'side', 'quantity', 'price', 'status'], rows, empty: 'no orders sent yet' }
}

// A crypto order names the money to spend instead of the amount to buy, and
// showing its absent quantity as zero would read as an order for nothing.
function amount(order: Order): string {
  return order.quantity > 0 ? trim(order.quantity) : dollars(order.notional)
}

// A refusal travels inside the answer, so the answer is what this column shows;
// the status alone would read as success on a rejected order.
function answered(call: ToolCall): string {
  if (call.failure) return `${call.status}: ${call.failure}`
  if (call.answer?.startsWith('refused: ')) return call.answer
  return call.status
}

function callsTable(calls: ToolCall[]): { head: string[]; rows: string[][]; empty: string } {
  return {
    head: ['started', 'tool', 'asked', 'took', 'answered'],
    rows: calls.map((call) => [
      clock(call.started_at),
      call.server && call.server !== 'shell' ? `${call.server}.${call.tool}` : call.tool,
      call.arguments === undefined || call.arguments === null ? '' : JSON.stringify(call.arguments),
      call.finished_at ? took(call.started_at, call.finished_at) : '',
      answered(call),
    ]),
    empty: 'no tool calls recorded yet',
  }
}

// A negative limit price on a spread is a credit: the broker pays us to open it.
function price(order: Order): string {
  if (order.filled_price > 0) return `filled ${dollars(order.filled_price)}`
  return order.limit_price < 0
    ? `credit ${dollars(Math.abs(order.limit_price))}`
    : `limit ${dollars(order.limit_price)}`
}

function when(order: Order): string {
  return order.submitted_at ? clock(order.submitted_at) : ''
}

function fillTable(
  selector: string,
  table: { head: string[]; rows: string[][]; empty: string },
): void {
  const element = document.querySelector(selector)!
  if (table.rows.length === 0) {
    const caption = document.createElement('caption')
    caption.className = 'empty'
    caption.textContent = table.empty
    element.replaceChildren(caption)
    return
  }

  const head = document.createElement('thead')
  const headRow = document.createElement('tr')
  for (const name of table.head) {
    const cell = document.createElement('th')
    cell.textContent = name
    headRow.append(cell)
  }
  head.append(headRow)

  const body = document.createElement('tbody')
  for (const row of table.rows) {
    const line = document.createElement('tr')
    for (const value of row) {
      const cell = document.createElement('td')
      cell.textContent = value
      line.append(cell)
    }
    body.append(line)
  }

  element.replaceChildren(head, body)
}

function showUnavailable(selector: string, why: string): void {
  const section = document.querySelector(selector)!
  const said = section.querySelector('.unavailable') ?? document.createElement('p')
  said.className = 'unavailable'
  said.textContent = why
  section.append(said)
}

function say(text: string): void {
  document.querySelector('#freshness')!.textContent = text
}

function fill<T>(selector: string, rows: T[], card: (row: T) => HTMLElement, whenEmpty: string): void {
  const list = document.querySelector(selector)!
  if (rows.length === 0) {
    const empty = document.createElement('li')
    empty.className = 'empty'
    empty.textContent = whenEmpty
    list.replaceChildren(empty)
    return
  }
  list.replaceChildren(...rows.map(card))
}

function turnCard(turn: Turn): HTMLElement {
  const item = shell()
  const state = turn.failure
    ? { text: turn.failure, className: 'down' }
    : turn.finished_at
      ? { text: took(turn.started_at, turn.finished_at), className: '' }
      : { text: 'running', className: 'up' }

  item.append(
    head([
      { text: clock(turn.started_at) },
      { text: turn.woken_by, className: 'who' },
      { text: state.text, className: state.className },
      ...(turn.model ? [{ text: turn.model }] : []),
    ]),
    body(turn.cause),
  )
  return item
}

function intentCard(intent: Intent): HTMLElement {
  const item = shell()
  item.append(
    head([{ text: clock(intent.at) }, { text: intent.session, className: 'who' }]),
    body(intent.thesis),
    pairs([
      ['structure', intent.structure],
      ['max loss', intent.max_loss],
    ]),
  )
  return item
}

function shell(): HTMLElement {
  const item = document.createElement('li')
  item.className = 'card'
  return item
}

function head(parts: { text: string; className?: string }[]): HTMLElement {
  const line = document.createElement('div')
  line.className = 'head'
  for (const part of parts) {
    if (part.text === '') continue
    const span = document.createElement('span')
    if (part.className) span.className = part.className
    span.textContent = part.text
    line.append(span)
  }
  return line
}

function body(text: string): HTMLElement {
  const paragraph = document.createElement('div')
  paragraph.className = 'body'
  paragraph.textContent = text
  return paragraph
}

function pairs(rows: [string, string][]): HTMLElement {
  const list = document.createElement('dl')
  list.className = 'pairs'
  for (const [name, value] of rows) {
    const term = document.createElement('dt')
    term.textContent = name
    const detail = document.createElement('dd')
    detail.textContent = value
    list.append(term, detail)
  }
  return list
}

const currency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 2,
})

function dollars(value: number): string {
  return currency.format(value)
}

function signed(value: number): string {
  return `${value > 0 ? '+' : ''}${currency.format(value)}`
}

function percent(fraction: number): string {
  return `${fraction > 0 ? '+' : ''}${(fraction * 100).toFixed(2)}%`
}

// Quantities run from one contract to eight decimals of a coin; neither reads
// well under one rule, so trailing zeros go and the rest stays.
function trim(value: number): string {
  return Number(value.toFixed(8)).toString()
}

// Times are shown in the reader's own zone: a judge in another city should not
// have to convert New York's hours in their head.
function clock(at: string): string {
  return new Date(at).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function took(from: string, to: string): string {
  const seconds = Math.max(0, Math.round((Date.parse(to) - Date.parse(from)) / 1000))
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${seconds % 60}s`
}

void render()
setInterval(() => void render(), refreshEvery)
