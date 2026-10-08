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

## 练习 19A：Go API Client（已完成）

本步不修改页面，也不接 SSE。先把浏览器与 Go 之间最容易重复出错的机械逻辑收进一个模块：

```text
React 页面（暂未切换）
        ↓ 以后只调用业务方法
src/api/client.ts
        ↓ fetch + JSON + HTTP error
Vite /api proxy
        ↓
Go net/http :8080
```

### 从前端视角理解文件职责

| 文件 | 熟悉的前端概念 | 当前职责 |
| --- | --- | --- |
| `src/api/contracts.ts` | OpenAPI 生成的 `types.ts` | 镜像 Go JSON DTO 和枚举 |
| `src/api/client.ts` | 配置好的 axios instance | 统一 URL、fetch、AbortSignal、JSON 和错误 |
| `tests/go-api-client.test.ts` | MSW/axios adapter 契约测试 | 不启动 Go 进程也能验证请求协议 |
| `vite.config.ts` | `devServer.proxy` | 开发时把同源 `/api` 转发到 `127.0.0.1:8080` |

### 逐步阅读

1. 先看 `contracts.ts`：请求类型只包含浏览器有权提交的字段。例如验证请求只能提交
   `ticketVersionId`，不能提交命令和 Worktree 路径。
2. 再看 `createWorkbenchApiClient`：组件面对的是 `getRequirement(id)` 这样的业务方法，
   不再自己拼 URL 和 HTTP method。
3. 最后看 `requestJSON`：它类似 axios response interceptor，统一序列化 body、解析一次
   response，并把 Go `{ code, message }` 转成 `WorkbenchApiError`。
4. 网络失败与 `AbortError` 不在这里改写。它们没有收到服务端 HTTP 响应，不能伪装成 500。

### 为什么现在没有 Snapshot client

旧 Node 页面依赖一个很大的 `WorkbenchSnapshot`，但当前 Go API 只提供 Requirement、TaskRun
和 Verification 等明确资源，还没有等价的 Snapshot endpoint。19A 不虚构不存在的后端合约。
后续页面切流会先用这些资源建立 React Query，再根据真实页面读取需求决定是否增加聚合读取接口。

### 19A 验收命令

```bash
pnpm --filter @product-agent-workbench/web typecheck
pnpm --filter @product-agent-workbench/web test -- go-api-client.test.ts
pnpm --filter @product-agent-workbench/web build
```

## 练习 19B：React Query 页面切流（已完成）

当前 Go 已实现的资源已经形成独立可运行页面 `/go-control`。旧 `/` 页面仍保留，因为其中的
Grilling、Spec 草稿、Ticket 草稿和目标预览尚无 Go endpoint；直接把两套不兼容模型拼在一个
页面中，会让 Node JSON 与 Go SQLite 同时成为真值来源。

```text
表单输入 / 当前选中 ID              Go 服务端事实
         │                              │
         ▼                              ▼
 React useState                 TanStack Query Cache
                                        ▲
                                        │
                              query / mutation hooks
                                        │
                                        ▼
                                19A API Client
```

### 逐步理解 `src/api/queries.ts`

1. `goQueryKeys` 给每种服务端资源稳定地址，例如
   `['go-control-plane', 'task-run', taskRunId]`。
2. Query hook 读取资源，空 ID 时用 `enabled: false` 阻止无意义请求。
3. 活动 TaskRun 每秒轮询；进入 completed/failed/cancelled 后自动停止。
4. Mutation 成功后直接把 Go 响应写入准确的缓存槽，不先 invalidate 再重复请求。
5. 缓存写入函数是小型纯 adapter，所以无需渲染 React 也可以做契约测试。

### `/go-control` 已接通的动作

- 创建和按 ID 读取 Requirement；
- 根据确认的 TicketVersion 调用 Scheduler，并展示本次入队 TaskRun；
- 按 ID 查询 TaskRun、轮询活动状态并取消；
- 启动 Verification、读取最近报告并展示三值证据；
- 展示 Go 健康状态和结构化 HTTP 错误。

页面组件没有保存 Requirement/TaskRun/Report 副本。`useState` 只用于输入框和选择 ID，
服务端资源全部由 React Query 管理。这相当于前端项目中“不把 server state 再复制进 Zustand”。

### 19B 验收命令

```bash
pnpm --filter @product-agent-workbench/web typecheck
pnpm --filter @product-agent-workbench/web test
pnpm --filter @product-agent-workbench/web build
```

下一步 19C 会把 `GET /api/v1/events` 接入 `EventSource`：SSE 只负责推进或失效 Query Cache，
断线时携带全局事件游标恢复，收到 `replay-reset` 时重新拉取权威资源。
