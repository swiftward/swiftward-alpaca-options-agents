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
// EVERY TIME ON THIS PAGE IS NEW YORK, and it says so where it is shown.
//
// It used to be the reader's own: `toLocaleString(undefined, ...)` takes the
// browser's zone, so the same run read `03:41 AM` in New York, `08:41` in London
// and `04:41 PM` in Singapore. This whole page is about a schedule of market
// windows - 10:20, 12:30, 14:20 - and a judge abroad would have concluded the
// agent trades in the middle of the night.
//
// 24-hour, because the declaration and every other page write the windows that
// way, and because `04:44 PM` for a run at four in the morning is the exact
// mistake this is here to prevent.
export function clock(at: string): string {
  return new Date(at).toLocaleString('en-US', {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
    timeZone: 'America/New_York',
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
  // Minutes stop being readable somewhere around an hour: `772m ago` is thirteen
  // hours and reads as a fault rather than as a night.
  if (seconds < 3600) return `${Math.round(seconds / 60)}m ago`
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h ago`

  return `${Math.round(seconds / 86400)}d ago`
}

// Whether the New York market is open right now. Used to say WHY something is
// old: a screener pass thirteen hours behind is a closed market, not a broken
// screener, and a reader cannot tell those apart from the age alone.
export function marketOpen(now = new Date()): boolean {
  const there = new Date(now.toLocaleString('en-US', { timeZone: 'America/New_York' }))
  const day = there.getDay()
  if (day === 0 || day === 6) return false
  const minutes = there.getHours() * 60 + there.getMinutes()

  return minutes >= 9 * 60 + 30 && minutes < 16 * 60
}
