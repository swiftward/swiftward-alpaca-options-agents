import { Route, Routes } from 'react-router'

import { Landing } from './Landing'
import { Live } from './Live'
import { Nav } from './Nav'
import { Submission } from './Submission'

// The page's own shape, written once.
//
// It is separate from `main.tsx` because it is now rendered twice: in the browser
// where it hydrates, and at BUILD TIME where it is turned into HTML files. A tree
// declared in the browser entry could only be rendered there, and the two would
// drift the first time a route was added to one of them.
export function App() {
  return (
    <>
      {/* The bar sits OUTSIDE Routes, so it is written once and every page gets
          the same one - including the ones that do not exist yet. */}
      <Nav />
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/live" element={<Live />} />
        <Route path="/submission" element={<Submission />} />
        {/* Any other path leads to the landing page rather than nowhere: a link
            with a typo should show where you landed, not a white screen. */}
        <Route path="*" element={<Landing />} />
      </Routes>
    </>
  )
}

// EVERY ROUTE THIS SITE HAS, and what each is for.
//
// One list, and it is the source of three things that used to be written apart
// and drifted: which pages are rendered to HTML at build time, what the sitemap
// says, and what the server will answer at all. A route added here appears in all
// three; a path not here is a 404 rather than a copy of the landing page.
export const PAGES = [
  {
    path: '/',
    // The tab and the search result. Every title names the project first, because
    // a reader with three tabs open sees the first thirty characters of each.
    title: 'Swiftward Alpaca — the model decides, the engine refuses',
    says: 'An autonomous options agent on Alpaca paper trading. It reads its risk limits while it works and cannot move them: every order passes a policy engine that can refuse it. Every number the project publishes recomputes from committed data, with no credentials and no network.',
  },
  {
    path: '/live',
    title: 'Live account — Swiftward Alpaca',
    says: "The account as the broker reports it right now: equity, profit since the start, the limits the agent discovered at runtime, the screener's last pass, open positions, and every run the agent made in its own words - refusals included.",
  },
  {
    path: '/submission',
    title: 'For judges — Swiftward Alpaca',
    says: 'The hackathon entry in one page: the account it is judged on, the result at the close the organiser measures, what answers each scoring criterion, and where to open the evidence for every claim.',
  },
] as const
