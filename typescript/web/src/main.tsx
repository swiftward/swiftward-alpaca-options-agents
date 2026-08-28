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
        {/* Любой другой путь ведёт на лендинг, а не в пустоту: ссылка с опечаткой
            должна показывать, куда пришли, а не белый экран. */}
        <Route path="*" element={<Landing />} />
      </Routes>
    </BrowserRouter>
  </StrictMode>,
)
