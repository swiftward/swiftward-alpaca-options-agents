import { ArrowRight, Eye, GitBranch, Ruler } from 'lucide-react'
import { Link } from 'react-router'

import { Card, Eyebrow } from './parts'

// Лендинг. Пока минимальный: он говорит, что это, чем отличается и куда идти
// смотреть. Всё остальное - потом.
//
// Три утверждения выбраны не по красоте. Это то единственное, что у нас есть и
// чего почти наверняка не будет у соседей по хакатону: пределы, приходящие
// обнаружением; числа, посчитанные на истории; и рассуждение, которое видно.
export function Landing() {
  return (
    <main className="mx-auto max-w-[1100px] px-6 pb-32 pt-20">
      <Eyebrow>[ live · alpaca paper trading ]</Eyebrow>

      <h1 className="mt-6 max-w-[16ch] text-[56px] font-medium leading-[1.05] tracking-[-0.024em] text-primary sm:text-[72px]">
        An options agent that shows its reasoning.
      </h1>

      <p className="mt-7 max-w-[46ch] text-[22px] font-medium leading-[1.25] tracking-[-0.01em] text-secondary">
        It wakes on a schedule, reads what the market offers, states what it means to do — and
        only then sends an order. Every number it trades on was measured, not guessed.
      </p>

      <div className="mt-10 flex flex-wrap items-center gap-4">
        <Link
          to="/live"
          className="inline-flex items-center gap-2 rounded-md bg-accent-ink px-5 py-3 font-mono text-sm uppercase tracking-[0.02em] text-inverse transition-opacity hover:opacity-90"
        >
          Watch it trade
          <ArrowRight className="size-4" aria-hidden />
        </Link>
        <span className="font-mono text-xs uppercase tracking-[0.04em] text-muted">
          updates every 15 seconds
        </span>
      </div>

      <div className="mt-20 grid gap-4 sm:grid-cols-3">
        <Claim
          icon={Ruler}
          title="Limits it discovered"
          says="Nothing about risk is written into its instructions. It asks what it is allowed to do while it works, and the page shows the same answer it gets — including the rule that admits it exists and withholds its number."
        />
        <Claim
          icon={GitBranch}
          title="Numbers that were measured"
          says="Two and a half years of option prices decided the thresholds. The first version of the rule lost money by construction; the measurement is what found that, and the comment beside every number names the script that produced it."
        />
        <Claim
          icon={Eye}
          title="Reasoning you can read"
          says="Each run shows what woke it, what it asked the broker, and what it concluded — in its own words. Including the runs where it looked and decided to do nothing."
        />
      </div>
    </main>
  )
}

function Claim({
  icon: Icon,
  title,
  says,
}: {
  icon: typeof Eye
  title: string
  says: string
}) {
  return (
    <Card>
      <Icon className="size-5 text-muted" aria-hidden />
      <h2 className="mt-4 text-[22px] font-medium leading-tight tracking-[-0.01em] text-primary">
        {title}
      </h2>
      <p className="mt-2.5 text-[15px] leading-relaxed text-secondary">{says}</p>
    </Card>
  )
}
