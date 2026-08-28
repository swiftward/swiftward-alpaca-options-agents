import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'

// Переиспользуемые части страницы.
//
// Правило, по которому они здесь заводятся, записано в ../CLAUDE.md: часть
// переезжает сюда, когда её просят БОЛЬШЕ ОДНОГО раза. Одно место - пишется по
// месту; второе - переезжает, потому что с этого мгновения два вида одного и
// того же начинают расходиться.
//
// Все берут цвета и радиусы из ролей Haia (bg, surface, line, text-*), а не из
// шкалы серых Tailwind. Роль знает, что делать в каждой теме; `neutral-800`,
// поставленный по месту, не знает ничего.

// Надпись над разделом. У системы это моноширинный верхний регистр с
// разрядкой - подпись, а не заголовок.
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
      <div className="mt-5">{children}</div>
    </section>
  )
}

// Карточка системы: приподнятая поверхность, граница, радиус xl, отступ s-6.
export function Card({ children }: { children: ReactNode }) {
  return <div className="rounded-xl border border-line bg-surface-raised p-6">{children}</div>
}

// Чип: пилюля, поверхность, граница, моноширинная подпись.
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

// Пустота и отказ выглядят ПО-РАЗНОМУ нарочно. «Ничего ещё не было» читатель
// обновлять не станет, «не ответило» - станет, и путать их нельзя.
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
      не ответило: {why}
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
  // hero - число, ради которого страницу открывают. Одно на всю страницу.
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
