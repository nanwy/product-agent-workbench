import { createRouter as createTanStackRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'

// TanStack Start 在服务端渲染和浏览器 hydration 时都会调用该工厂。
// routeTree.gen.ts 是路由生成器产物，业务路由应修改 src/routes，不直接编辑生成文件。
export function getRouter() {
  const router = createTanStackRouter({
    routeTree,
    scrollRestoration: true,
    defaultPreload: 'intent',
    defaultPreloadStaleTime: 0,
  })

  return router
}

declare module '@tanstack/react-router' {
  // 把本项目 Router 类型注册给 TanStack，使 Link、loader 和 Server Function 获得路由类型推导。
  interface Register {
    router: ReturnType<typeof getRouter>
  }
}
