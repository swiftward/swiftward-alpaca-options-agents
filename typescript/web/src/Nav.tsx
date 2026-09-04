import { Menu, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router'

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

const REPOSITORY = 'https://github.com/swiftward/swiftward-alpaca-options-agents/'

export function Nav() {
  const [open, setOpen] = useState(false)
  const { pathname } = useLocation()

  // Choosing a link closes the panel. Without this the reader taps a link, the
  // page underneath changes, and the menu stays over it hiding what they asked
  // for - the commonest way a hand-rolled mobile menu goes wrong.
  useEffect(() => setOpen(false), [pathname])

  // Escape closes it too. A panel that can only be dismissed by hitting the same
  // small button again is a trap for anyone not using a mouse.
  useEffect(() => {
    if (!open) return

    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setOpen(false)
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [open])

  return (
    <header className="sticky top-0 z-50 border-b border-line bg-bg/85 backdrop-blur">
      <nav className="mx-auto max-w-[1100px] px-6" aria-label="Site">
        {/* Three columns rather than one row with `ml-auto`: the outer two are
            equal fractions, so the links sit at the MIDDLE OF THE BAR and stay
            there when the brand or the button beside them changes width. A row
            would centre them between their neighbours instead, which drifts. */}
        <div className="flex items-center justify-between gap-4 py-3.5 sm:grid sm:grid-cols-[1fr_auto_1fr]">
          <NavLink to="/" className="flex shrink-0 items-center gap-2.5">
            <img
              src="/swiftward-mark.png"
              alt=""
              width={30}
              height={26}
              className="h-[26px] w-auto"
            />
            <span className="text-[15px] font-medium tracking-[-0.01em] text-primary">
              Swiftward Alpaca
            </span>
          </NavLink>

          <ul className="m-0 hidden list-none items-center gap-x-7 p-0 sm:flex">
            {LINKS.map((link) => (
              <li key={link.to} className="flex items-center">
                <Item {...link} />
              </li>
            ))}
          </ul>

          <div className="flex items-center justify-end gap-2">
            <a
              href={REPOSITORY}
              target="_blank"
              rel="noreferrer"
              aria-label="The source on GitHub"
              title="The source on GitHub"
              className="inline-flex size-9 items-center justify-center rounded-lg border border-line text-secondary transition-colors hover:border-line-strong hover:text-primary"
            >
              <GitHubMark />
            </a>

            <button
              type="button"
              onClick={() => setOpen((was) => !was)}
              aria-expanded={open}
              aria-controls="site-menu"
              aria-label={open ? 'Close the menu' : 'Open the menu'}
              className="inline-flex size-9 cursor-pointer items-center justify-center rounded-lg border border-line text-secondary transition-colors hover:border-line-strong hover:text-primary sm:hidden"
            >
              {open ? <X className="size-4" aria-hidden /> : <Menu className="size-4" aria-hidden />}
            </button>
          </div>
        </div>

        {/* The narrow-screen panel. It holds the SAME `Item` the wide bar does,
            so a link cannot end up styled or spelled two ways. */}
        <ul
          id="site-menu"
          hidden={!open}
          className="m-0 list-none border-t border-line py-2 pl-0 sm:hidden"
        >
          {LINKS.map((link) => (
            <li key={link.to} className="flex items-center py-2">
              <Item {...link} />
            </li>
          ))}
        </ul>
      </nav>
    </header>
  )
}

function Item({ to, says, live }: { to: string; says: string; live?: boolean }) {
  return (
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
  )
}

// Drawn here rather than imported: lucide dropped its brand marks, and this is
// the only one the page needs.
function GitHubMark() {
  return (
    <svg viewBox="0 0 16 16" className="size-[18px]" fill="currentColor" aria-hidden>
      <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.6 7.6 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8Z" />
    </svg>
  )
}
