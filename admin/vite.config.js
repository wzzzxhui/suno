import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) }
  },
  server: {
    port: 5180,
    proxy: {
      // 后台接口转发到 Go 服务
      '/admin/api': { target: 'http://127.0.0.1:8080', changeOrigin: true }
    }
  }
})
