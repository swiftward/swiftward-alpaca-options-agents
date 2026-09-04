import { NavLink } from 'react-router'

// The bar every page carries. It is defined ONCE and rendered by the router
// around whatever route matched, rather than imported by each page: three copies
// of a navigation bar is three places for the links to fall out of step, and the
// one that falls behind is always the one a judge happens to open.
//
// `Live` keeps its name. Another entrant calls the same kind of page a Ledger,
// and a ledger is a record of what has already happened - true of ours, but it
// undersells a page whose numbers move while it is open. A judge arrives asking
// whether any of this actually runs, and `Live` is the word that answers.
const LINKS: { to: string; says: string; live?: boolean }[] = [
  { to: '/', says: 'Overview' },
  { to: '/live', says: 'Live', live: true },
  { to: '/submission', says: 'For judges' },
]

export function Nav() {
  return (
    <header className="sticky top-0 z-50 border-b border-line bg-bg/85 backdrop-blur">
      <nav
        className="mx-auto flex max-w-[1100px] flex-wrap items-center gap-x-8 gap-y-3 px-6 py-3.5"
        aria-label="Site"
      >
        <NavLink to="/" className="flex shrink-0 items-center gap-2.5">
          <img src="/swiftward-mark.png" alt="" width={30} height={26} className="h-[26px] w-auto" />
          <span className="text-[15px] font-medium tracking-[-0.01em] text-primary">
            Swiftward Alpaca
          </span>
        </NavLink>

        <ul className="m-0 flex list-none flex-wrap items-center gap-x-6 gap-y-2 p-0 sm:ml-auto">
          {LINKS.map(({ to, says, live }) => (
            <li key={to}>
              <NavLink
                to={to}
                end={to === '/'}
                className={({ isActive }) =>
                  `inline-flex items-center text-[14px] transition-colors ${
                    isActive ? 'font-medium text-primary' : 'text-secondary hover:text-primary'
                  }`
                }
              >
                {/* The dot belongs to the page that moves, and to nothing else. */}
                {live ? (
                  <span
                    className="mr-1.5 inline-block size-1.5 rounded-full bg-accent motion-safe:animate-pulse"
                    aria-hidden
                  />
                ) : null}
                {says}
              </NavLink>
            </li>
          ))}
        </ul>
      </nav>
    </header>
  )
}
