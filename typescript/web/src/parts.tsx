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
// A card is WHITE ON AN ALMOST-WHITE PAGE, so the border alone was carrying the
// separation and the page read as one flat sheet. The shadow is two layers: a
// hairline that sits the card on the ground, and a wide soft one that lifts it.
// Kept faint on purpose - a shadow a reader can name is louder than the content.
export function Card({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-xl border border-line bg-surface-raised p-6 shadow-[0_1px_2px_rgba(16,18,22,0.04),0_12px_32px_-16px_rgba(16,18,22,0.14)]">
      {children}
    </div>
  )
}

// A chip: a pill, a surface, a border, a monospace label.
export function Chip({
  children,
  tone,
}: {
  children: ReactNode
  tone?: 'gain' | 'loss' | 'strong' | 'accent'
}) {
  const colour =
    tone === 'gain'
      ? 'text-gain'
      : tone === 'loss'
        ? 'text-loss'
        : tone === 'strong'
          ? 'text-primary'
          : tone === 'accent'
            ? 'text-accent'
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

export function Figures({ children, spread }: { children: ReactNode; spread?: boolean }) {
  // Packed left by default, because most of these plaques hold however many
  // figures the data happened to have, and evenly spacing an unknown count
  // leaves the last row straggling.
  //
  // `spread` pushes a fixed set out to both edges instead of leaving the border
  // running on past the last figure. Equal columns were tried first and were
  // worse: a quarter of the row is generous to "16" and too mean for a sum with
  // a percentage after it, which wrapped onto a second line. Spacing the gaps
  // rather than the columns lets every figure keep the width its own content
  // needs.
  //
  // On a narrow screen it is ONE COLUMN, figure under figure. Two to a row there
  // put a wide sum beside a two-digit count with a hole between them, and the eye
  // read the pairing as meaning something it did not.
  const layout = spread
    ? 'flex flex-col gap-6 sm:flex-row sm:flex-wrap sm:justify-between sm:gap-x-8'
    : 'flex flex-wrap gap-x-12 gap-y-6'

  return (
    <dl className={`m-0 rounded-xl border border-line bg-surface-raised px-6 py-6 ${layout}`}>
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

// A panel with a title bar, which is what a reader already knows an editor and a
// terminal look like. NO LINE NUMBERS: they are for pointing at a line during a
// conversation, and nobody is going to say "look at line four" about twelve lines
// quoted on a landing page - they would be furniture with a number on it.
//
// The title says WHERE the thing came from, which is the whole reason it is
// quoted: a reader who wants to check it now knows what to open.
export function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="overflow-hidden rounded-xl border border-code-edge bg-code-bg">
      <div className="flex items-center gap-3 border-b border-code-edge bg-code-chrome px-4 py-2.5">
        <span className="flex gap-1.5" aria-hidden>
          <span className="size-2.5 rounded-full bg-code-edge" />
          <span className="size-2.5 rounded-full bg-code-edge" />
          <span className="size-2.5 rounded-full bg-code-edge" />
        </span>
        <span className="font-mono text-[12px] text-code-punct">{title}</span>
      </div>
      <div className="overflow-x-auto p-5">{children}</div>
    </div>
  )
}

// The markup in the agent's words.
//
// Ours rather than a library's, and that is counted: across the lines collected
// so far there are exactly `**bold**` (15 times), `` `code` `` (14) and two list
// items. No headings, no links, no tables, no code blocks. A full parser costs a
// hundred kilobytes on top of the hundred and eighty already added - a bad trade
// for two constructs.
//
// Anything unfamiliar stays as text: a heading, if one ever appears, will show as
// a line with a hash - exactly as it does today, and no worse. There can be no
// HTML injection here: React escapes text on output, and we build nodes rather
// than a markup string.
const marks = /(\*\*[^*]+\*\*|`[^`]+`)/g

export function inline(text: string) {
  return text.split(marks).map((piece, index) => {
    if (piece.startsWith('**') && piece.endsWith('**') && piece.length > 4) {
      return <strong key={index} className="font-semibold">{piece.slice(2, -2)}</strong>
    }
    if (piece.startsWith('`') && piece.endsWith('`') && piece.length > 2) {
      return (
        <code key={index} className="rounded bg-surface-sunk px-1 py-0.5 font-mono text-[13px]">
          {piece.slice(1, -1)}
        </code>
      )
    }

    return piece
  })
}
