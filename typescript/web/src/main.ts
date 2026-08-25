// The demo page. It reads /state and shows the three things the record keeps:
// what the agent did, what it meant to do before it ordered, and where it was
// stopped. It decides nothing and never talks to the broker.

type Turn = {
  ref: string
  thread_ref: string
  started_at: string
  finished_at?: string
  woken_by: string
  cause: string
  model?: string
  failure?: string
}

type Intent = {
  at: string
  session: string
  thesis: string
  structure: string
  max_loss: string
}

type Refusal = { at: string; boundary: string; detail: string }

type State = { turns: Turn[]; intents: Intent[]; refusals: Refusal[] }

// Same origin by default: the page is served by the read side that answers
// /state. A deployment that puts the page elsewhere sets this to that address.
const readSide = import.meta.env.VITE_STATE_URL ?? ''

const refreshEvery = 15_000

async function render(): Promise<void> {
  let state: State
  try {
    const response = await fetch(`${readSide}/state`)
    if (!response.ok) throw new Error(`the read side answered ${response.status}`)
    state = await response.json()
  } catch (reason) {
    say(`the record is unreachable: ${reason instanceof Error ? reason.message : String(reason)}`)
    return
  }

  fill('#turns', state.turns, turnCard, 'no turns yet: nothing has woken the agent')
  fill('#intents', state.intents, intentCard, 'no intents yet: the agent has not planned an order')
  fill('#refusals', state.refusals, refusalCard, 'no refusals: no order has hit a boundary')
  say(`read at ${clock(new Date().toISOString())}`)
}

function say(text: string): void {
  document.querySelector('#freshness')!.textContent = text
}

function fill<T>(
  selector: string,
  rows: T[],
  card: (row: T) => HTMLElement,
  whenEmpty: string,
): void {
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
    ? { text: turn.failure, className: 'stopped' }
    : turn.finished_at
      ? { text: took(turn.started_at, turn.finished_at), className: '' }
      : { text: 'running', className: 'running' }

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

function refusalCard(refusal: Refusal): HTMLElement {
  const item = shell()
  item.append(
    head([
      { text: clock(refusal.at) },
      { text: refusal.boundary, className: 'who stopped' },
    ]),
    body(refusal.detail),
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

// Times are shown in the reader's own zone: a judge in another city should not
// have to convert New York's hours in their head.
function clock(at: string): string {
  return new Date(at).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
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
