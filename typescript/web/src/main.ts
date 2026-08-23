type Refusal = { at: string; boundary: string; detail: string }
type State = { ruleset: string; limits: string[]; refusals: Refusal[] }

const recorder = import.meta.env.VITE_RECORDER_URL ?? 'http://localhost:8080'

async function render(): Promise<void> {
  const response = await fetch(`${recorder}/state`)
  const state: State = await response.json()

  document.querySelector('#ruleset')!.textContent = `ruleset: ${state.ruleset}`
  fill('#limits', state.limits)
  fill('#refusals', state.refusals.map((r) => `${r.at} — ${r.boundary}: ${r.detail}`))
}

function fill(selector: string, lines: string[]): void {
  const list = document.querySelector(selector)!
  list.replaceChildren(
    ...lines.map((line) => {
      const item = document.createElement('li')
      item.textContent = line
      return item
    }),
  )
}

void render()
