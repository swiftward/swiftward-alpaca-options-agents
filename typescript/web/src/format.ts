// How numbers and times are shown to the reader. One place for the whole page,
// because the look of a number is a decision too, and it must be the same
// everywhere.

const currency = new Intl.NumberFormat(undefined, {
  style: 'currency',
  currency: 'USD',
  maximumFractionDigits: 2,
})

export function dollars(value: number): string {
  return currency.format(value)
}

export function signed(value: number): string {
  return `${value > 0 ? '+' : ''}${currency.format(value)}`
}

export function percent(fraction: number): string {
  return `${fraction > 0 ? '+' : ''}${(fraction * 100).toFixed(2)}%`
}

// Quantities run from one contract to eight decimal places of a coin; no single
// rule fits that, so trailing zeros go and everything else stays.
export function trim(value: number): string {
  return Number(value.toFixed(8)).toString()
}

// Times are shown in the READER's zone: a judge in another city should not have
// to convert New York hours in their head.
export function clock(at: string): string {
  return new Date(at).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function took(from: string, to: string): string {
  const seconds = Math.max(0, Math.round((Date.parse(to) - Date.parse(from)) / 1000))
  if (seconds < 60) return `${seconds}s`

  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

export function ago(at: string): string {
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(at)) / 1000))
  if (seconds < 90) return `${seconds}s ago`

  return `${Math.round(seconds / 60)}m ago`
}
