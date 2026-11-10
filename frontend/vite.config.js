import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

const proxyTarget = 'http://127.0.0.1:8080'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/v1': {
        target: proxyTarget,
        changeOrigin: true,
      },
      '/admin': {
        // Keep the browser Host so the backend can verify same-origin requests.
        target: proxyTarget,
      },
      '/api/mcp': {
        target: proxyTarget,
      },
      '/mcp': {
        target: proxyTarget,
      },
      '/auth': {
        target: proxyTarget,
        changeOrigin: true,
      },
      '/health': {
        target: proxyTarget,
        changeOrigin: true,
      },
      '/openapi.json': {
        target: proxyTarget,
        changeOrigin: true,
      },
    },
  },
})
