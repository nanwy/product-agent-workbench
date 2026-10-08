# Web 迁移说明

在 Go HTTP 合约稳定前，当前 TanStack Start UI 继续作为可运行产品，不提前改写页面。

迁移顺序：

1. 保持 `src/store/workbench-query.ts` 的语义：服务端 Snapshot 是权威状态，SSE 只负责失效和重新拉取。
2. 每次只把一组 Server Function 替换为带类型的 HTTP client。
3. 对应 Go endpoint 通过契约测试后，再迁移路由和展示模块。
4. Zustand 只保存瞬时 UI 状态；Requirement、TaskRun、事件和验证记录由 TanStack Query 管理。
5. 把已经迁入 Go 的控制面提取为独立 Vite SPA 并嵌入 Go；旧 Server Functions 暂时作为
   尚未迁移产品规划能力的行为参考，不进入单二进制生产包。

开发形态：

```text
Vite 浏览器页面 → /api/v1 proxy → Go 控制面
                 → EventSource → Go SSE endpoint
```

生产形态：

```text
Go 可执行文件 → 嵌入式 SPA 资源 + /api/v1（包含 SSE）
```

## 练习 19A：Go API Client（已完成）

本步不修改页面，也不接 SSE。先把浏览器与 Go 之间最容易重复出错的机械逻辑收进一个模块：

```text
React 页面（暂未切换）
        ↓ 以后只调用业务方法
src/api/client.ts
        ↓ fetch + JSON + HTTP error
Vite /api/v1 proxy
        ↓
Go net/http :8080
```

### 从前端视角理解文件职责

| 文件 | 熟悉的前端概念 | 当前职责 |
| --- | --- | --- |
| `src/api/contracts.ts` | OpenAPI 生成的 `types.ts` | 镜像 Go JSON DTO 和枚举 |
| `src/api/client.ts` | 配置好的 axios instance | 统一 URL、fetch、AbortSignal、JSON 和错误 |
| `tests/go-api-client.test.ts` | MSW/axios adapter 契约测试 | 不启动 Go 进程也能验证请求协议 |
| `vite.config.ts` | `devServer.proxy` | 开发时把同源 `/api/v1` 转发到 `127.0.0.1:8080` |

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

当前 Go 已实现的资源已经形成独立可运行页面 `/go-control`。19B 完成时旧 `/` 页面仍保留，
因为当时 Grilling、Spec 草稿、Ticket 草稿和目标预览尚无 Go endpoint。21A～21C 已补上
Grilling、Spec 与 TicketVersion；目标预览仍不能让 Node JSON 与 Go SQLite 同时成为真值来源。

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

## 练习 19C：SSE 重放与缓存收敛（已完成）

本步把 `GET /api/v1/events` 接入原生 `EventSource`。SSE 不把增量 payload 当作完整
TaskRun，而是精确失效对应 Query；React Query 随后通过 19A HTTP Client 回源。

```text
Codex / Worker
      ↓ 持久化 TaskEvent
SQLite 全局事件 ID
      ↓ GET /api/v1/events
EventSource
      ↓ task-event(taskRunId)
invalidate ['go-control-plane', 'task-run', taskRunId]
      ↓
GET /api/v1/task-runs/{id}
```

### 从前端视角理解断线恢复

1. 同一个 `EventSource` 网络闪断时，浏览器自动重连并发送 `Last-Event-ID`。
2. 页面刷新会创建新实例，浏览器不会替新实例恢复旧游标，因此前端把最后确认的全局 ID
   存入版本化 localStorage key，并在首次 URL 中发送 `?after=`。
3. Go 优先读取标准 `Last-Event-ID` 请求头；请求头不存在时才读取 `?after=`。这样首次恢复
   和浏览器自动重连共用同一套服务端校验。
4. `task-event` 只失效对应 TaskRun，不能整页刷新。
5. `replay-reset` 表示本地游标已不属于数据库保留窗口。此时保存服务端最新边界，并失效
   全部 Go Query，不能猜测遗漏了哪些业务状态。
6. SSE 正常时，活动 TaskRun 的轮询降到 30 秒校准；SSE 断线时自动退化为 1 秒轮询。

### 为什么不手写重连定时器

原生 `EventSource` 已实现重连和 `Last-Event-ID`。如果 `onerror` 中再次 `new EventSource()`，
旧实例也可能仍在自动重连，最终形成重复连接和重复失效。19C 的 `onerror` 只更新 UI 状态，
组件卸载时才调用 `close()`。

### 迁移期代理边界

Vite 只把 `/api/v1` 代理给 Go。旧页面仍使用 TanStack Start 的 `/api/events`，因此两个 SSE
入口在迁移期可以并存，不会因宽泛的 `/api` proxy 互相截获。

### 19C 验收命令

```bash
go test ./apps/server/internal/httpapi
pnpm --filter @product-agent-workbench/web typecheck
pnpm --filter @product-agent-workbench/web test
pnpm --filter @product-agent-workbench/web build
```

## 练习 20：Vite SPA + Go embed + 单命令启动（已完成）

步骤 20 没有直接把整个 TanStack Start 项目塞进 Go。旧构建包含 SSR、Nitro、Server
Functions 和 Node-only Codex 模块，浏览器不需要这些依赖。实现先把 19B/19C 的 Go 控制面
提取成共享组件，再为生产新增一个只导入浏览器依赖的入口。

```text
src/components/go-control-page.tsx
              │
       ┌──────┴─────────┐
       │                │
TanStack 迁移路由    src/spa/main.tsx
  /go-control            │
                         ▼
                  vite.spa.config.ts
                         │
                         ▼
          apps/server/internal/webui/dist
                         │ go:embed
                         ▼
                  Go 单一可执行文件
```

### 从前端视角理解 Go embed

1. Vite 仍然负责 TypeScript、React、Tailwind 和浏览器 bundle；Go 不编译 TSX。
2. `go:embed` 在 Go 编译阶段把 Vite 输出复制进二进制，类似把静态资源打进桌面应用安装包。
3. 浏览器请求 `/assets/app.js` 时，文件来自二进制内的只读 `embed.FS`，不是运行时磁盘目录。
4. `/api/*` 始终转发给 `httpapi.Server`；`/` 和无扩展名路径回退 `index.html`；缺失的
   `.js/.css` 返回真正 404，不能错误返回 HTML。
5. SPA 和 API 同源后，生产环境不再需要 CORS 或 Vite proxy；proxy 只服务 `dev:spa`。

### 为什么有 `dist/README.txt` 和 fallback

`go:embed` 的 pattern 必须至少匹配一个文件。生成产物默认不提交，因此仓库保留一个说明文件，
让纯 Go 测试仍能编译。若直接运行 Go 而没有先构建 SPA，根页面返回 503 和明确操作提示；
`pnpm workbench` 会先执行 Vite 构建，所以正常启动不会看到 fallback。

### 单命令

```bash
# 开发者直接启动完整 Go 工作台
pnpm workbench

# 或使用 Makefile
make start

# 生成包含 SPA 的单一二进制 apps/server/workbench
pnpm build:standalone
```

完整构建顺序是：

```text
Vite build:spa → Go go:embed → go run / go build → 同端口提供 SPA + API + SSE
```

### 20 验收命令

```bash
pnpm build:spa
go test ./apps/server/internal/webui ./apps/server/cmd/workbench
pnpm build:standalone
curl http://127.0.0.1:8080/
curl http://127.0.0.1:8080/api/v1/health
```

## 步骤 21：迁移剩余产品规划链路

### 21A Grilling（已完成）

页面把临时讨论收敛成共同理解，Go Application 通过 `expectedRevision` 只允许
`grilling → spec`。SQLite 在一个事务中同时保存共同理解和阶段变化。

### 21B Spec（已完成）

Go 启动只读 Codex Turn 检查目标仓库，解析严格的结构化协议并保存不可覆盖 Spec 版本。
确认当前草稿后阶段推进到 `tickets`。浏览器只提交修订意图，不能传仓库路径、命令或 sandbox。

### 21C Tickets（已完成）

Go 使用只读 Codex 把确认 Spec 拆成 Ticket DAG，SQLite 保存版本历史；确认后发布现有
Scheduler 与 Worker 消费的不可变 JSON manifest，并推进到 `implementation`。前端不再猜
TicketVersion ID，而是从确认响应取得真实 ID 并交给队列 Reconcile。

后续顺序：21D 目标预览、21E 删除 Node Server Functions，
最后由步骤 22 完成 Playwright 单二进制验收、故障演示、benchmark 和面试材料。
