import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import type { Snapshot } from './api'
import { clock, dollars, signed } from './format'

// Кривая капитала.
//
// Рисуется библиотекой, а не руками, и это смена решения. Голая ломаная -
// действительно двадцать строк. Но оси и подсказка под курсором - это разбор
// шкалы, засечки, попадание мышью в ближайшую точку и всплывающее окно, которое
// не уезжает за край; руками это уже не двадцать строк, а свой маленький
// график, который придётся чинить.
//
// Оси здесь не украшение. Без них читатель видит форму и не видит величины:
// подпись говорила «low $99,954.75 · high $100,074.40», а глазу негде было эти
// числа поставить, и любой скачок выглядел одинаково большим.
const gridDots = '3 3'

const whole = (value: number) =>
  value.toLocaleString('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })

// Разрыв в записи. Замеры идут раз в пять минут; когда стенд стоял, между
// соседними записями оказывалось десять часов. Линия, проведённая через такую
// дыру, показывает ровный подъём - то есть заработок, растянутый по времени, в
// котором мы вообще ничего не измеряли. Поэтому в дыру ставится пустое значение
// и линия рвётся: разрыв читается как «здесь не знаем», а это правда.
//
// Порог считается от самих данных, а не назначен: расписание замеров может
// поменяться, и число, вбитое руками, переживёт это изменение молча.
const gapAt = 4

type Point = {
  // Время числом, а не строкой: ось строит шкалу по часам, а не по номерам
  // замеров. Разница видна там, где агент молчал - ночной простой должен
  // занимать свою ширину, а не ширину одного замера, иначе график врёт о том,
  // когда что происходило.
  at: number
  equity: number | null
}

// Круглая величина не меньше заданной: 1, 2, 5, 10, 20, 50 и так далее. Нужна,
// чтобы засечки стояли на $100,050, а не на $100,092.35 - край шкалы, попавший
// в подпись, читается как значение, которого на счёте никогда не было.
function roundStep(least: number): number {
  const power = Math.pow(10, Math.floor(Math.log10(least)))
  for (const step of [1, 2, 5, 10]) {
    if (power * step >= least) return power * step
  }
  return power * 10
}

export function Equity({ line }: { line: Snapshot[] }) {
  // Пустой список, а не null - контракт на стороне данных. Но страница, которую
  // открывает судья, не имеет права падать целиком, даже если контракт нарушат.
  if ((line ?? []).length < 2) {
    // Пустая рамка читается как график, который не нарисовался. Пока замеров
    // меньше двух, линии нет, и подпись говорит почему.
    return (
      <p className="rounded-xl border border-dashed border-line px-4 py-5 text-sm text-muted">
        {(line ?? []).length === 0
          ? 'no account history recorded yet'
          : 'one reading so far: a line needs two'}
      </p>
    )
  }

  const values = line.map((point) => point.equity)
  const lowest = Math.min(...values)
  const highest = Math.max(...values)
  const opened = values[0]
  const rising = values[values.length - 1] >= opened
  const stroke = rising ? 'var(--color-gain)' : 'var(--color-loss)'

  const read = line.map((point) => ({
    at: Date.parse(point.recorded_at),
    equity: point.equity as number | null,
  }))

  const between = read.slice(1).map((point, index) => point.at - read[index].at).sort((a, b) => a - b)
  const usual = between[Math.floor(between.length / 2)] ?? 0
  const points: Point[] = []
  let gaps = 0
  for (const [index, point] of read.entries()) {
    if (index > 0 && usual > 0 && point.at - read[index - 1].at > usual * gapAt) {
      points.push({ at: (point.at + read[index - 1].at) / 2, equity: null })
      gaps++
    }
    points.push(point)
  }

  // Шкала от минимума к максимуму, а не от нуля. Счёт стоит около ста тысяч и
  // ходит на сотню долларов: шкала от нуля превратила бы всю работу недели в
  // прямую линию.
  const step = roundStep(Math.max((highest - lowest) * 1.3, 0.04) / 4)
  const floor = Math.floor(lowest / step) * step
  const ceiling = Math.ceil(highest / step) * step
  const marks: number[] = []
  for (let mark = floor; mark <= ceiling + step / 2; mark += step) marks.push(mark)

  return (
    <figure className="m-0">
      <div className="h-56 w-full rounded-xl border border-line bg-surface-raised p-3 pr-4">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={points} margin={{ top: 6, right: 8, bottom: 0, left: 0 }}>
            <defs>
              {/* Заливка гаснет к низу: сплошная спорила бы по весу с самой
                  линией, а показать надо линию. */}
              <linearGradient id="equity-fill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={stroke} stopOpacity={0.22} />
                <stop offset="100%" stopColor={stroke} stopOpacity={0.01} />
              </linearGradient>
            </defs>

            <CartesianGrid
              strokeDasharray={gridDots}
              stroke="var(--color-line)"
              vertical={false}
            />
            <XAxis
              dataKey="at"
              type="number"
              scale="time"
              domain={['dataMin', 'dataMax']}
              tickFormatter={(at: number) => clock(new Date(at).toISOString())}
              tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
              stroke="var(--color-line)"
              tickLine={false}
              minTickGap={56}
            />
            <YAxis
              domain={[floor, ceiling]}
              ticks={marks}
              // Центы на оси - шум: шкала нужна, чтобы прикинуть величину, а
              // точное число даёт подсказка под курсором.
              tickFormatter={(value: number) => whole(value)}
              tick={{ fill: 'var(--color-muted)', fontSize: 11 }}
              stroke="var(--color-line)"
              tickLine={false}
              width={78}
            />
            <Tooltip
              content={<Balance opened={opened} />}
              cursor={{ stroke: 'var(--color-line-strong)', strokeDasharray: gridDots }}
            />
            <Area
              // linear, а не monotone. Сглаживание красивее, но оно ДОРИСОВЫВАЕТ:
              // исполнение сделки поднимает счёт мгновенно, и monotone превращал
              // этот отвесный скачок в плавный подъём через шесть часов - то
              // есть рисовал заработок, растянутый во времени, которого не было.
              // Судья читает график как свидетельство, и он обязан показывать
              // замеры, а не кривую между ними.
              type="linear"
              dataKey="equity"
              stroke={stroke}
              strokeWidth={2}
              fill="url(#equity-fill)"
              connectNulls={false}
              // Точек больше пятисот: кружок на каждой залил бы график целиком.
              // Остаётся одна - под курсором.
              dot={false}
              activeDot={{ r: 4, fill: stroke, stroke: 'var(--color-surface-raised)', strokeWidth: 2 }}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
      <figcaption className="mt-2 text-xs text-muted">
        {line.length} readings, {clock(line[0].recorded_at)} — {clock(line[line.length - 1].recorded_at)}
        {highest === lowest ? ' · unchanged' : ` · low ${dollars(lowest)} · high ${dollars(highest)}`}
        {gaps > 0 && ` · ${gaps} ${gaps === 1 ? 'break' : 'breaks'} where nothing was recorded`}
      </figcaption>
    </figure>
  )
}

// Окно под курсором. Показывает не только баланс, но и разницу с началом: сам
// по себе «$100,041.20» не говорит, много это или мало, а «+$41.20» говорит.
function Balance({
  active,
  payload,
  opened,
}: {
  active?: boolean
  payload?: Array<{ payload: Point }>
  opened: number
}) {
  const point = payload?.[0]?.payload
  // Разрыв тоже попадает под курсор, и у него нет величины. Окно с «$NaN»
  // выглядело бы поломкой страницы, а это нормальное «здесь не мерили».
  if (!active || !point || point.equity === null) return null

  const change = point.equity - opened

  return (
    <div className="rounded-lg border border-line bg-surface-raised px-3 py-2 text-xs shadow-lg">
      <div className="font-mono text-sm tabular-nums text-strong">{dollars(point.equity)}</div>
      <div className={`font-mono tabular-nums ${change >= 0 ? 'text-gain' : 'text-loss'}`}>
        {signed(change)} since the start
      </div>
      <div className="mt-1 text-muted">{clock(new Date(point.at).toISOString())}</div>
    </div>
  )
}
