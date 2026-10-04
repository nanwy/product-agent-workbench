import path from 'node:path'

/**
 * 返回 Product Agent Workbench monorepo 根目录。
 *
 * pnpm filter 会以 package 目录作为 cwd 运行前端脚本，而 `.agents`、`.data`
 * 和受控目标配置仍属于整个产品仓库。显式环境变量优先，便于打包或测试覆盖；
 * 默认值只描述当前 `apps/web` 的 monorepo 布局，不扫描任意父目录。
 */
export function resolveWorkbenchRoot() {
  return path.resolve(
    process.env.WORKBENCH_ROOT ?? path.join(process.cwd(), '..', '..'),
  )
}
