import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

import { GoControlPage } from '../components/go-control-page'
import '../styles.css'

/**
 * 练习 20：嵌入 Go 的纯浏览器入口。
 *
 * 前端视角：这里相当于原来 React 项目的 main.tsx。它只装配浏览器真正需要的 Provider，
 * 不导入 TanStack Start Router、Server Functions 或任何 Node 模块，因此能输出普通静态文件。
 */
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: false,
      refetchOnWindowFocus: false,
    },
  },
})

const root = document.getElementById('root')
if (!root) throw new Error('SPA 缺少 #root 挂载节点')

createRoot(root).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <GoControlPage standalone />
    </QueryClientProvider>
  </StrictMode>,
)
