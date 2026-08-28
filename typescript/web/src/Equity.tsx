import type { Snapshot } from './api'
import { clock, dollars } from './format'

// Кривая капитала. Рисуется руками, а не библиотекой: одна ломаная с заливкой -
// это двадцать строк, а любая библиотека графиков стоит сотню килобайт и своих
// решений о том, как должен выглядеть наш график.
export function Equity({ line }: { line: Snapshot[] }) {
  if (line.length < 2) {
    // Пустая рамка читается как график, который не нарисовался. Пока замеров
    // меньше двух, линии нет, и подпись говорит почему.
    return (
      <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-5 text-sm text-neutral-500 dark:border-neutral-700 dark:text-neutral-400">
        {line.length === 0
          ? 'история счёта ещё не записана'
          : 'записан один замер: для линии нужны два'}
      </p>
    )
  }

  const width = 720
  const height = 180
  const values = line.map((point) => point.equity)
  const lowest = Math.min(...values)
  const highest = Math.max(...values)
  const span = highest - lowest
  const rising = values[values.length - 1] >= values[0]

  const at = (point: Snapshot, index: number): [number, number] => [
    (index / (line.length - 1)) * width,
    span === 0 ? height / 2 : height - ((point.equity - lowest) / span) * (height - 16) - 8,
  ]

  const points = line.map(at)
  const path = points.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(' ')
  const [lastX, lastY] = points[points.length - 1]
  const stroke = rising ? 'var(--color-gain)' : 'var(--color-loss)'

  return (
    <figure className="m-0">
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={`Капитал счёта: ${dollars(values[0])} в начале, ${dollars(values[values.length - 1])} сейчас`}
        className="block h-44 w-full rounded-xl border border-neutral-200 bg-white dark:border-neutral-800 dark:bg-neutral-900"
      >
        {/* Заливка под линией - не украшение: она показывает, что величина
            считается от нуля вниз, а голая ломаная этого не говорит. */}
        <polygon points={`0,${height} ${path} ${width},${height}`} fill={stroke} opacity="0.08" />
        <polyline points={path} fill="none" stroke={stroke} strokeWidth="2" vectorEffect="non-scaling-stroke" />
        {/* Точка на конце: глаз ищет, где «сейчас», и без неё ищет долго. */}
        <circle cx={lastX} cy={lastY} r="3.5" fill={stroke} vectorEffect="non-scaling-stroke" />
      </svg>
      <figcaption className="mt-2 text-xs text-neutral-500 dark:text-neutral-400">
        {line.length} замеров, {clock(line[0].recorded_at)} — {clock(line[line.length - 1].recorded_at)}
        {span === 0
          ? ' · без изменений'
          : ` · низ ${dollars(lowest)} · верх ${dollars(highest)}`}
      </figcaption>
    </figure>
  )
}
