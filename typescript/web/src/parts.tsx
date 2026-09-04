import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { Link } from 'react-router'

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

// A HEADING, not a label. This titled with an Eyebrow - 12px mono, uppercase,
// muted - on the reasoning that a dashboard's title names a panel the reader is
// already looking at. That holds for somebody operating the page, and not for the
// reader it is actually built for: a judge reads it from the top like a document,
// and six identical grey labels give them nothing to steer by.
//
// It stays smaller than the landing's, which has to stop a stranger scrolling
// past. This one only has to be found.
export function Section({
  title,
  explains,
  children,
}: {
  title: string
  explains?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="mb-16">
      <h2 className="text-[26px] font-medium leading-tight tracking-[-0.015em] text-primary">
        {title}
      </h2>
      {explains ? (
        <p className="mt-3 max-w-[920px] text-[15px] leading-relaxed text-secondary">{explains}</p>
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
export function Card({ children, marked }: { children: ReactNode; marked?: boolean }) {
  // `marked` is the accent rail the landing uses on the card that carries the
  // section's evidence. One per page at most: a rail on everything is a border.
  return (
    <div
      className={`rounded-xl border bg-surface-raised p-6 shadow-[0_1px_2px_rgba(16,18,22,0.04),0_12px_32px_-16px_rgba(16,18,22,0.14)] ${
        marked ? 'border-line border-l-[3px] border-l-accent' : 'border-line'
      }`}
    >
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
export function Panel({
  title,
  aside,
  children,
}: {
  title: string
  aside?: ReactNode
  children: ReactNode
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-code-edge bg-code-bg">
      <div className="flex items-center gap-3 border-b border-code-edge bg-code-chrome px-4 py-2.5">
        <span className="flex gap-1.5" aria-hidden>
          <span className="size-2.5 rounded-full bg-code-edge" />
          <span className="size-2.5 rounded-full bg-code-edge" />
          <span className="size-2.5 rounded-full bg-code-edge" />
        </span>
        <span className="font-mono text-[12px] text-code-punct">{title}</span>
        {/* The state of the file being shown, where an editor puts it. A status
            set ABOVE the panel needs a card of its own to hang on, and a card
            holding nothing but a panel is a frame around a frame. */}
        {aside ? <span className="ml-auto">{aside}</span> : null}
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

// A marker pen, and the only thing on the page drawn in Alpaca's yellow. One
// meaning: THIS IS THE SENTENCE TO TAKE AWAY. It is a ground with dark ink on it
// and never text in the yellow itself - measured, black on it is 13.95:1 and the
// yellow on this page's own background is 1.41:1, which no reader can see.
//
// Four of these on the whole page. A fifth would make it a colour scheme rather
// than an emphasis, and nothing marked is emphasised.
export function Mark({ children }: { children: ReactNode }) {
  return (
    <mark className="rounded-[3px] bg-mark px-1.5 py-0.5 text-mark-ink decoration-clone">
      {children}
    </mark>
  )
}

// A tiny YAML highlighter. Two pages quote YAML now: the declaration on the
// landing, and the risk engine's own answer on the live page.
//
// Not a library. The same trade the markdown in the agent's words was weighed
// against and lost: a real highlighter is around a hundred kilobytes to colour
// four kinds of token in twelve lines, and it would carry grammars for languages
// this page will never show. This handles exactly the shapes the sample has - a
// list dash, a key, a quoted string, a bracketed list, the block scalar `|` and
// the indented prose under it - and anything it does not recognise stays the
// colour the text already was.
//
// There is no injection risk: React nodes are built, never a markup string.
export function Yaml({
  title,
  aside,
  source,
}: {
  title: string
  aside?: ReactNode
  source: string
}) {
  return (
    <Panel title={title} aside={aside}>
      <pre className="m-0 font-mono text-[13px] leading-relaxed text-code-fg">
        {source.split('\n').map((line, index) => (
          <span key={index} className="block">
            {colour(line)}
            {'\n'}
          </span>
        ))}
      </pre>
    </Panel>
  )
}

// One line at a time. Prose under `task: |` is indented four spaces or more and is
// left as prose - it is English, and colouring it as code would say it is not.
function colour(line: string) {
  // A comment is the one thing here that is not data, and it says so by taking
  // the colour of punctuation.
  if (line.trim().startsWith('#')) {
    return <span className="text-code-punct">{line}</span>
  }

  // Prose used to be recognised by its indent - four spaces or more - which was
  // right for the one sample this handled and wrong the moment a second arrived:
  // nested YAML keys are indented too, and every one of them lost its colour.
  //
  // The regex settles it instead. A key is lower-case letters straight up against
  // a colon, and English prose does not look like that: `A morning entry` starts
  // with a capital, `movement lies ahead` has a space before its colon.
  const key = /^(\s*)(-\s)?([a-z_]+)(:)(.*)$/.exec(line)
  if (!key) {
    return <span className="text-code-fg">{line}</span>
  }

  const [, indent, dash, name, colon, rest] = key

  return (
    <>
      {indent}
      {dash ? <span className="text-code-punct">{dash}</span> : null}
      <span className="text-code-key">{name}</span>
      <span className="text-code-punct">{colon}</span>
      {value(rest)}
    </>
  )
}

// What follows the colon: a quoted string, a bracketed list, a duration or clock
// time, the block marker, or nothing it knows.
function value(rest: string) {
  const string = /^(\s*)(".*")$/.exec(rest)
  if (string) {
    return (
      <>
        {string[1]}
        <span className="text-code-string">{string[2]}</span>
      </>
    )
  }

  const list = /^(\s*)(\[)(.*)(\])$/.exec(rest)
  if (list) {
    return (
      <>
        {list[1]}
        <span className="text-code-punct">{list[2]}</span>
        <span className="text-code-fg">{list[3]}</span>
        <span className="text-code-punct">{list[4]}</span>
      </>
    )
  }

  const number = /^(\s*)(\d+[a-z]?)$/.exec(rest)
  if (number) {
    return (
      <>
        {number[1]}
        <span className="text-code-number">{number[2]}</span>
      </>
    )
  }

  if (rest.trim() === '|') {
    return (
      <>
        {' '}
        <span className="text-code-punct">|</span>
      </>
    )
  }

  return <span className="text-code-fg">{rest}</span>
}

// The way to the live page. Two pages send the reader there, and the pulsing dot is
// the promise the page keeps: the figures around it are frozen, that one is not.
export function LiveLink() {
  return (
    <Link
      to="/live"
      className="inline-flex cursor-pointer items-center gap-2 rounded-lg bg-accent px-5 py-2.5 text-[15px] font-medium text-on-accent transition-opacity hover:opacity-90"
    >
      <span
        className="inline-block size-1.5 rounded-full bg-on-accent motion-safe:animate-pulse"
        aria-hidden
      />
      See it live
    </Link>
  )
}
