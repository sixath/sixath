import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true, // 0.0.0.0 — allow LAN access
    port: 5173,
    proxy: {
      // Chat / SSE must hit Gateway (Portal public inbound is disabled by default).
      // Prefer path regex over `router` — Vite/http-proxy router is easy to miss-match.
      '^/api/v1/agents/[^/]+/sessions': {
        target: 'http://127.0.0.1:8088',
        changeOrigin: true,
      },
      '/api/v1/sessions': {
        target: 'http://127.0.0.1:8088',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:8000',
        changeOrigin: true,
      },
    },
  },
  preview: {
    host: true,
    port: 5173,
  },
})
