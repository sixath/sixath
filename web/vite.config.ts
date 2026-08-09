import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    host: true, // 0.0.0.0 — allow LAN access
    port: 5173,
    proxy: {
      '/api/v1/sessions': {
        target: 'http://localhost:8088',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://localhost:8000',
        changeOrigin: true,
        router(req) {
          const u = req.url || ''
          if (/^\/api\/v1\/agents\/[^/]+\/sessions/.test(u)) {
            return 'http://localhost:8088'
          }
          if (u.startsWith('/api/v1/sessions')) {
            return 'http://localhost:8088'
          }
          return 'http://localhost:8000'
        },
      },
    },
  },
  preview: {
    host: true,
    port: 5173,
  },
})
