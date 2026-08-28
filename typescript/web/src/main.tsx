import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { App } from './App'
import './style.css'

const mount = document.querySelector('#root')
if (!mount) throw new Error('нет #root: страница не может быть смонтирована')

createRoot(mount).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
