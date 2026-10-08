# Workbench Web 应用

本包包含现有 React 19 + TanStack Start 应用及其 Node MVP 模块。迁移期间，它仍是可运行产品，也是 Go 控制面的行为参照。

练习 19A 已建立 `src/api/contracts.ts` 和 `src/api/client.ts`；练习 19B 新增 `/go-control`
资源页，并用 `src/api/queries.ts` 把 Requirement、TaskRun 和 Verification 接入 React Query。
旧 `/` 页面继续承载尚未迁入 Go 的产品规划流程。完整中文脚手架和前端类比见
`GO_MIGRATION.md`。

练习 19C 已为 `/go-control` 接入 Go 持久化 SSE：全局游标跨页面刷新保存，TaskRun 事件
精确失效 React Query，游标断档时重新拉取全部活动 Go 资源。迁移期间 `/api/events`
继续属于旧 Node 页面，只有 `/api/v1` 由 Vite 代理到 Go。

练习 20 新增独立 `src/spa/main.tsx` 和 `vite.spa.config.ts`。该构建只包含 Go 控制面，
输出到 `apps/server/internal/webui/dist` 并由 Go `embed.FS` 打进二进制：

```bash
pnpm build:spa
pnpm build:standalone
pnpm workbench
```

旧 `pnpm dev` 仍启动 TanStack Start 行为参考；`pnpm dev:spa` 用于在 3001 端口单独调试
新 SPA，并把 `/api/v1` 代理给 8080 的 Go 服务。

在仓库根目录运行：

```bash
pnpm dev
pnpm typecheck
pnpm test
pnpm test:e2e
pnpm build
```

仓库级工作流 Skills 和目标项目配置继续放在 monorepo 根目录。当前包从 `apps/web` 运行时，由 `src/server/workbench-root.ts` 统一解析仓库根目录。
