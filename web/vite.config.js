import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// 拾游集前端构建配置
// 说明：后端静态资源目录解析基于「绝对根路径」，故 base 固定为 '/'
// 开发期通过 proxy 直连后端 8666 端口，避免跨域
export default defineConfig({
  base: '/',
  plugins: [react()],
  build: {
    outDir: 'dist',
    assetsDir: 'assets',
    emptyOutDir: true,
    chunkSizeWarningLimit: 2048,
  },
  server: {
    host: true,
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8666', changeOrigin: true },
      '/play': { target: 'http://localhost:8666', changeOrigin: true },
    },
  },
})
