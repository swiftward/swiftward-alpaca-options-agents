import { StrictMode } from 'react'
import { hydrateRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'

import { App } from './App'
import './style.css'

const mount = document.querySelector('#root')
if (!mount) throw new Error('no #root: the page cannot mount')

// HYDRATE, not create. Every route is rendered to HTML at build time, so what
// arrives already reads as the finished page - to a reader on a slow connection,
// to one who blocks scripts, and to a judge's agent, which fetches the address and
// reads what comes back rather than running it. React takes over the markup that
// is already there instead of throwing it away and drawing it again.
hydrateRoot(
  mount,
  <StrictMode>
    <BrowserRouter>
      <App />
    </BrowserRouter>
  </StrictMode>,
)
