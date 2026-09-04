import { renderToString } from 'react-dom/server'
import { StaticRouter } from 'react-router'

import { App } from './App'

export { PAGES } from './App'

// One page, as HTML, at build time. `prerender.mjs` calls this once per route and
// writes what comes back into the built template.
export function render(path: string) {
  return renderToString(
    <StaticRouter location={path}>
      <App />
    </StaticRouter>,
  )
}
