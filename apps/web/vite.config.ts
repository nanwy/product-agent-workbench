import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'

import { tanstackStart } from '@tanstack/react-start/plugin/vite'

import viteReact from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { nitro } from 'nitro/vite'

const config = defineConfig({
  resolve: { tsconfigPaths: true },
  // 练习 19A/19C：只有新版 /api/v1 转发给 Go。旧页面的 /api/events 仍由 TanStack
  // Start 处理，迁移期间两条链路不会互相截获。前端类比：这就是 devServer.proxy 的窄匹配。
  server: {
    proxy: {
      '/api/v1': {
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
