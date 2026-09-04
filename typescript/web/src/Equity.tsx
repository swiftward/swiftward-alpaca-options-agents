import {
  Area,
  AreaChart,
  CartesianGrid,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import type { Snapshot } from './api'
import { clock, dollars, signed } from './format'

// The equity curve.
//
// Drawn by a library rather than by hand, and that is a changed decision. A bare
// polyline really is twenty lines. But axes and a tooltip under the cursor mean
// scale parsing, ticks, hitting the nearest point with the mouse, and a popup
// that does not run off the edge; by hand that is no longer twenty lines but a
// small chart of one's own that will need fixing.
//
// The axes here are not decoration. Without them the reader sees the shape and
// not the magnitude: the caption said "low $99,954.75 · high $100,074.40" and the
// eye had nowhere to put those numbers, so every jump looked equally large.
const gridDots = '3 3'

const whole = (value: number) =>
  value.toLocaleString('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })

// A gap in the record. The measurements run every five minutes; while the
// deployment was down, ten hours passed between neighbouring rows.
//
// THE CHART SHOWS THE JUDGED WINDOW, not the whole record.
//
// The account was funded on the kickoff day and sat at exactly 100,000 until the
// Monday; the organiser measures from Monday 31 August, 09:30 New York. Both of
// the two gaps this record contains fall in those idle days - the deployment was
// down for two hours and then for twelve - and there is not a third after them.
const WINDOW_OPENS = Date.parse('2026-08-31T13:30:00Z')

// The other end of it. The organiser measures "portfolio's total equity as of EOD
// Thursday Sep 3rd", and the broker's own reference defines the field that carries
// it - `last_equity` - as "Equity as of previous trading day at 16:00:00 ET".
//
// The line does not stop here. The agent goes on trading after the measurement, so
// the chart goes on drawing, and the two marks say which part of it was measured.
const MEASURED_AT = Date.parse('2026-09-03T20:00:00Z')

// A hole in the record is drawn ACROSS, not broken at. A break left a white notch
// that reads as a chart which failed to draw, and a reader cannot tell that from a
// deployment that was down.
//
// What makes that safe is the window above rather than the drawing: inside the
// judged week there is no hole, and the three days that held the only two are
// outside it. If the deployment ever goes down mid-week, the segment across the
// hole will claim a measurement that was not taken - that is the cost, and it is
// written here rather than left for somebody to discover in the chart.

type Point = {
  // Time as a number, not a string: the axis builds its scale on hours, not on
  // measurement numbers. The difference shows where the agent was silent - an
  // overnight pause must take its own width rather than the width of one
  // measurement, or the chart lies about when things happened.
  at: number
  equity: number | null
}

// A round magnitude no smaller than the one given: 1, 2, 5, 10, 20, 50 and so
// on. It is needed so ticks land on $100,050 rather than $100,092.35 - an edge of
// the scale that reaches a label reads as a value the account never held.
function roundStep(least: number): number {
  const power = Math.pow(10, Math.floor(Math.log10(least)))
  for (const step of [1, 2, 5, 10]) {
    if (power * step >= least) return power * step
  }
  return power * 10
}

export function Equity({ line: recorded }: { line: Snapshot[] }) {
  // Cut to the window - but only if the cut leaves a line. On a fresh deployment,
  // or a week that has not reached the Monday, the whole record is better than an
  // empty frame.
  const windowed = (recorded ?? []).filter((point) => Date.parse(point.recorded_at) >= WINDOW_OPENS)
  const line = windowed.length >= 2 ? windowed : recorded

  // An empty list, not null - that is the contract on the data side. But a page a
  // judge opens has no right to die outright even if the contract is broken.
  if ((line ?? []).length < 2) {
    // An empty frame reads as a chart that failed to draw. While there are fewer
    // than two measurements there is no line, and the caption says why.
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

  const points: Point[] = read

  // The scale runs from the minimum to the maximum, not from zero. The account
  // stands near a hundred thousand and moves by a hundred dollars: a scale from
  // zero would turn the whole week's work into a straight line.
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
              {/* The fill fades downwards: a solid one would compete in weight
                  with the line itself, and the line is what matters. */}
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
              // Cents on the axis are noise: the scale is there to judge the
              // magnitude, and the tooltip gives the exact number.
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
            <ReferenceLine x={WINDOW_OPENS} {...edge('window opens')} />
            <ReferenceLine x={MEASURED_AT} {...edge('measured')} />

            <Area
              // linear, not monotone. Smoothing is prettier, but it DRAWS DATA
              // IN: a fill lifts the account instantly, and monotone turned that
              // vertical step into a gentle rise over six hours - it drew
              // earnings spread across time that never happened. A judge reads
              // the chart as evidence, and it must show the measurements, not a
              // curve between them.
              type="linear"
              dataKey="equity"
              stroke={stroke}
              strokeWidth={2}
              fill="url(#equity-fill)"
              connectNulls
              // There are more than five hundred points: a dot on each would
              // fill the chart entirely. One is left - under the cursor.
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
        <br />
        Between the two marks is the window the organiser measures; the account keeps
        trading past the second one, and so does this line.
      </figcaption>
    </figure>
  )
}

// Both marks are drawn the same way, so neither reads as the more important one.
const edge = (label: string) => ({
  stroke: 'var(--color-line-strong)',
  strokeDasharray: '4 4',
  label: {
    value: label,
    position: 'insideTop' as const,
    fill: 'var(--color-muted)',
    fontSize: 10,
  },
})

// The popup under the cursor. It shows not only the balance but the difference
// from the start: "$100,041.20" on its own does not say whether that is much,
// and "+$41.20" does.
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
  // A break falls under the cursor too, and it has no magnitude. A popup reading
  // "$NaN" would look like a broken page, while this is an ordinary "we did not
  // measure here".
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
