import { fileURLToPath, URL } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import viteReact from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const spaRoot = fileURLToPath(new URL('./src/spa/', import.meta.url))
const goStaticDirectory = fileURLToPath(
  new URL('../server/internal/webui/dist/', import.meta.url),
)

/**
 * 练习 20 的独立 SPA 构建，不复用 TanStack Start/Nitro 插件。
 *
 * 输出文件名固定，避免 emptyOutDir=false 时遗留失效的哈希 chunk；README.txt 会保留，
 * 让尚未执行前端构建的 Go 源码仍满足 go:embed 的编译要求。
 */
export default defineConfig({
  root: spaRoot,
  base: '/',
  publicDir: false,
  plugins: [tailwindcss(), viteReact()],
  build: {
    outDir: goStaticDirectory,
    emptyOutDir: false,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/app.js',
        chunkFileNames: 'assets/[name].js',
        assetFileNames: (assetInfo) =>
          assetInfo.names.some((name) => name.endsWith('.css'))
            ? 'assets/app.css'
            : 'assets/[name][extname]',
      },
    },
  },
  server: {
    proxy: {
      '/api/v1': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
})

