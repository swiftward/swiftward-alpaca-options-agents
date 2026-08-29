import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

import { Landing } from './Landing'
import { Live } from './Live'
import './style.css'

const mount = document.querySelector('#root')
if (!mount) throw new Error('no #root: the page cannot mount')

createRoot(mount).render(
  <StrictMode>
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/live" element={<Live />} />
        {/* Any other path leads to the landing page rather than nowhere: a link
            with a typo should show where you landed, not a white screen. */}
        <Route path="*" element={<Landing />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
