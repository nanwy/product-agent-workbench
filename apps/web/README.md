# Workbench Web 应用

本包包含现有 React 19 + TanStack Start 应用及其 Node MVP 模块。迁移期间，它仍是可运行产品，也是 Go 控制面的行为参照。

在仓库根目录运行：

```bash
pnpm dev
pnpm typecheck
pnpm test
pnpm test:e2e
pnpm build
```

仓库级工作流 Skills 和目标项目配置继续放在 monorepo 根目录。当前包从 `apps/web` 运行时，由 `src/server/workbench-root.ts` 统一解析仓库根目录。
