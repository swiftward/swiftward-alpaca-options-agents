import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // The data lives with Go, the page with Vite. Without this a request to
    // `api/money` goes to 5173, Vite answers with its own index.html - because the
    // page's routes live in the browser and anything unknown gets the page - and
    // the JSON parse trips over `<!doctype`, so the section shows a failure.
    //
    // Proxying makes development the same as production: one source, the same
    // path, no headers for someone else's domain.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
