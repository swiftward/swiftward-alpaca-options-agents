import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { Boundary } from './Boundary'

// The page's reusable parts.
//
// The rule for putting them here is written in ../CLAUDE.md: a part moves here
// when it is asked for MORE THAN ONCE. One place means it is written in place; a
// second means it moves, because from that moment two copies of the same thing
// start to diverge.
//
// They all take colours and radii from the page's roles (bg, surface, line,
// text-*) rather than from Tailwind's grey scale. A role knows what to do in each
// theme; a `neutral-800` written in place knows nothing.

// The label above a section. In the system this is monospace upper case with
// letter spacing - a caption, not a heading.
export function Eyebrow({ children }: { children: ReactNode }) {
  return <p className="m-0 font-mono text-xs uppercase tracking-[0.04em] text-muted">{children}</p>
}

export function Section({
  title,
  explains,
  children,
}: {
  title: string
  explains?: string
  children: ReactNode
}) {
  return (
    <section className="mb-16">
      <Eyebrow>{title}</Eyebrow>
      {explains ? (
        <p className="mt-2 max-w-[920px] text-[15px] leading-relaxed text-secondary">{explains}</p>
      ) : null}
      {/* Every section under its own boundary, and here rather than at the call
          site: otherwise the protection goes to whichever sections somebody
          remembered to wrap. On 28 August one field with a null value took the
          WHOLE live page to a white screen. Now a section falls and the rest stays
          readable. */}
      <div className="mt-5">
        <Boundary says={`this section failed`}>{children}</Boundary>
      </div>
    </section>
  )
}

// The system's card: a raised surface, a border, radius xl, spacing s-6.
export function Card({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-line bg-surface-raised p-6">{children}</div>
}

// A chip: a pill, a surface, a border, a monospace label.
export function Chip({
  children,
  tone,
}: {
  children: ReactNode
  tone?: 'gain' | 'loss' | 'strong'
}) {
  const colour =
    tone === 'gain'
      ? 'text-gain'
      : tone === 'loss'
        ? 'text-loss'
        : tone === 'strong'
          ? 'text-primary'
          : 'text-muted'

  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border border-line bg-surface px-2.5 py-0.5 font-mono text-[11px] uppercase tracking-[0.04em] ${colour}`}
    >
      {children}
    </span>
  )
}

// Emptiness and failure look DIFFERENT on purpose. The reader will not refresh on
// "nothing has happened yet" and will on "it did not answer", and the two must not
// be confused.
export function Empty({ says }: { says: string }) {
  return (
    <p className="m-0 rounded-xl border border-dashed border-line px-6 py-5 text-[15px] text-muted">
      {says}
    </p>
  )
}

export function Unavailable({ why }: { why: string }) {
  return (
    <p className="m-0 rounded-xl border border-dashed border-loss/50 px-6 py-5 text-[15px] text-loss">
      unavailable: {why}
    </p>
  )
}

export function Figure({
  name,
  value,
  tone,
  icon: Icon,
  hero,
}: {
  name: string
  value: string
  tone?: 'gain' | 'loss'
  icon?: LucideIcon
  hero?: boolean
}) {
  const colour = tone === 'gain' ? 'text-gain' : tone === 'loss' ? 'text-loss' : ''
  // hero is the number the page is opened for. One on the whole page.
  const size = hero ? 'text-[40px] leading-none tracking-[-0.024em]' : 'text-[26px] leading-none'

  return (
    <div className="flex flex-col gap-1.5">
      <dt className="font-mono text-[11px] uppercase tracking-[0.04em] text-muted">{name}</dt>
      <dd className={`m-0 flex items-center gap-2 font-medium ${size} ${colour}`}>
        {Icon ? <Icon className={hero ? 'size-7 shrink-0' : 'size-5 shrink-0'} aria-hidden /> : null}
        {value}
      </dd>
    </div>
  )
}

export function Figures({ children }: { children: ReactNode }) {
  return (
    <dl className="m-0 flex flex-wrap gap-x-12 gap-y-6 rounded-xl border border-line bg-surface-raised px-6 py-6">
      {children}
    </dl>
  )
}

export function Table({ head, rows, empty }: { head: string[]; rows: ReactNode[][]; empty: string }) {
  if (rows.length === 0) return <Empty says={empty} />

  return (
    <div className="overflow-x-auto rounded-xl border border-line">
      <table className="w-full border-collapse bg-surface-raised text-[15px]">
        <thead>
          <tr>
            {head.map((name) => (
              <th
                key={name}
                className="whitespace-nowrap border-b border-line px-4 py-3 text-left font-mono text-[11px] font-normal uppercase tracking-[0.04em] text-muted"
              >
                {name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index} className="[&:last-child>td]:border-b-0">
              {row.map((cell, column) => (
                <td key={column} className="whitespace-nowrap border-b border-line px-4 py-3">
                  {cell}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
