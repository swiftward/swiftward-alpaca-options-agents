// Как числа и время показываются читателю. Одно место на всю страницу, потому
// что вид числа - это тоже решение, и оно должно быть одинаковым везде.

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

// Количества идут от одного контракта до восьми знаков монеты; под одно правило
// это не ложится, поэтому хвостовые нули уходят, а остальное остаётся.
export function trim(value: number): string {
  return Number(value.toFixed(8)).toString()
}

// Время показывается в зоне ЧИТАТЕЛЯ: судья из другого города не должен
// пересчитывать нью-йоркские часы в уме.
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
  if (seconds < 60) return `${seconds} с`

  return `${Math.floor(seconds / 60)} мин ${seconds % 60} с`
}

export function ago(at: string): string {
  const seconds = Math.max(0, Math.round((Date.now() - Date.parse(at)) / 1000))
  if (seconds < 90) return `${seconds} с назад`

  return `${Math.round(seconds / 60)} мин назад`
}
