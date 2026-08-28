import type { ReactNode } from 'react'

// Мелкие части страницы. Вынесены отдельно не ради порядка, а потому что вид
// раздела - решение, и принимать его надо один раз на все разделы.

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
    <section className="mb-12">
      <h2 className="text-xs font-semibold uppercase tracking-[0.12em] text-neutral-500 dark:text-neutral-400">
        {title}
      </h2>
      {explains ? (
        <p className="mt-1.5 max-w-2xl text-sm leading-relaxed text-neutral-600 dark:text-neutral-400">
          {explains}
        </p>
      ) : null}
      <div className="mt-4">{children}</div>
    </section>
  )
}

export function Card({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-xl border border-neutral-200 bg-white p-4 dark:border-neutral-800 dark:bg-neutral-900">
      {children}
    </div>
  )
}

// Пустота и отказ выглядят ПО-РАЗНОМУ нарочно. «Ничего ещё не было» читатель
// обновлять не станет, а «не ответило» - станет, и путать их нельзя.
export function Empty({ says }: { says: string }) {
  return (
    <p className="rounded-xl border border-dashed border-neutral-300 px-4 py-5 text-sm text-neutral-500 dark:border-neutral-700 dark:text-neutral-400">
      {says}
    </p>
  )
}

export function Unavailable({ why }: { why: string }) {
  return (
    <p className="rounded-xl border border-dashed border-loss/50 px-4 py-5 text-sm text-loss">
      не ответило: {why}
    </p>
  )
}

export function Figure({ name, value, tone }: { name: string; value: string; tone?: 'gain' | 'loss' }) {
  const colour = tone === 'gain' ? 'text-gain' : tone === 'loss' ? 'text-loss' : ''

  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-[0.68rem] font-medium uppercase tracking-[0.1em] text-neutral-500 dark:text-neutral-400">
        {name}
      </dt>
      <dd className={`text-2xl font-semibold leading-none ${colour}`}>{value}</dd>
    </div>
  )
}

export function Table({
  head,
  rows,
  empty,
}: {
  head: string[]
  rows: ReactNode[][]
  empty: string
}) {
  if (rows.length === 0) return <Empty says={empty} />

  return (
    <div className="overflow-x-auto rounded-xl border border-neutral-200 dark:border-neutral-800">
      <table className="w-full border-collapse bg-white text-sm dark:bg-neutral-900">
        <thead>
          <tr>
            {head.map((name) => (
              <th
                key={name}
                className="whitespace-nowrap border-b border-neutral-200 px-4 py-2.5 text-left text-[0.68rem] font-semibold uppercase tracking-[0.08em] text-neutral-500 dark:border-neutral-800 dark:text-neutral-400"
              >
                {name}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {rows.map((row, index) => (
            <tr key={index} className="last:[&>td]:border-b-0">
              {row.map((cell, column) => (
                <td
                  key={column}
                  className="whitespace-nowrap border-b border-neutral-200 px-4 py-2.5 dark:border-neutral-800"
                >
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
