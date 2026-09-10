import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// dev 模式把 /api 与 /uploads 代理到本地 Go 后端（8080）——
// 前端代码里永远写相对路径，无需关心 CORS。
export default defineConfig({
  plugins: [vue()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/uploads': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
})
