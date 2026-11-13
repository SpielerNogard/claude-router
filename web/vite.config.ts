import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: '/ui/',
  plugins: [vue()],
  server: {
    proxy: {
      '/api': 'http://localhost:4141',
      '/healthz': 'http://localhost:4141',
    },
  },
})
