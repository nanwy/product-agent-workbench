# Web 迁移说明

在 Go HTTP 合约稳定前，当前 TanStack Start UI 继续作为可运行产品，不提前改写页面。

迁移顺序：

1. 保持 `src/store/workbench-query.ts` 的语义：服务端 Snapshot 是权威状态，SSE 只负责失效和重新拉取。
2. 每次只把一组 Server Function 替换为带类型的 HTTP client。
3. 对应 Go endpoint 通过契约测试后，再迁移路由和展示模块。
4. Zustand 只保存瞬时 UI 状态；Requirement、TaskRun、事件和验证记录由 TanStack Query 管理。
5. 所有 Server Function 移除后，再把本目录构建为 Vite SPA，并把生产资源嵌入 Go 可执行文件。

开发形态：

```text
Vite 浏览器页面 → /api proxy → Go 控制面
                 → EventSource → Go SSE endpoint
```

生产形态：

```text
Go 可执行文件 → 嵌入式 SPA 资源 + /api + /events
```

第一个前端练习是在不修改视觉模块的前提下，编写 HTTP 合约类型和 Snapshot client。
