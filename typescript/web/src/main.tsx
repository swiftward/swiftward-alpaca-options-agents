import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter, Route, Routes } from 'react-router'

import { Landing } from './Landing'
import { Live } from './Live'
import { Nav } from './Nav'
import { Submission } from './Submission'
import './style.css'

const mount = document.querySelector('#root')
if (!mount) throw new Error('no #root: the page cannot mount')

createRoot(mount).render(
  <StrictMode>
    <BrowserRouter>
      {/* The bar sits OUTSIDE Routes, so it is written once and every page
          gets the same one - including the ones that do not exist yet. */}
      <Nav />
      <Routes>
        <Route path="/" element={<Landing />} />
        <Route path="/live" element={<Live />} />
        <Route path="/submission" element={<Submission />} />
        {/* Any other path leads to the landing page rather than nowhere: a link
            with a typo should show where you landed, not a white screen. */}
        <Route path="*" element={<Landing />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
