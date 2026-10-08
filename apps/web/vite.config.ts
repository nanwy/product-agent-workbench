import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'

import { tanstackStart } from '@tanstack/react-start/plugin/vite'

import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { nitro } from 'nitro/vite'

const config = defineConfig({
  resolve: { tsconfigPaths: true },
  // 练习 19A：浏览器继续请求同源 /api，Vite 在开发环境转发给 Go 控制面。
  // 前端类比：这和常见的 devServer.proxy 相同，既避免组件硬编码 8080，也避免开发期 CORS。
  server: {
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: false,
      },
    },
  },
  // 插件顺序覆盖开发工具、Nitro 服务端、Tailwind、TanStack Start 路由和 React 编译。
  plugins: [
    devtools(),
    nitro({ rollupConfig: { external: [/^@sentry\//] } }),
    tailwindcss(),
    tanstackStart(),
    viteReact(),
  ],
})

export default config
