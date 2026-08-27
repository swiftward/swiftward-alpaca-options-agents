import { defineConfig } from 'vite'

export default defineConfig({
  // Relative asset URLs, so the built page works at the site root and equally
  // under a path. The public address has to share a port - a Tailscale funnel is
  // allowed on three and all three are taken - and an absolute /assets/ URL
  // would leave the mount and fetch nothing.
  base: './',
  server: { port: 3000 },
  preview: { port: 3000 },
})
