# 练习 17C：像组合 React Provider 一样装配 Go 服务

## 什么是依赖注入

依赖注入不是框架，也不是特殊语法。它只是：对象需要什么，由外部创建后传进来。

```ts
// 前端：组件不在内部 new APIClient
<UserPage api={apiClient} />
```

```go
// Go：Application 不在内部打开数据库
application, err := app.New(app.Dependencies{Store: store})
```

本项目服务数量还不大，使用手动构造函数注入，不引入 Wire、Fx 或其它 DI 容器。

## 什么是 composition root

它是唯一知道所有具体实现的地方，一般位于 `main` 附近：

```text
main / buildRuntime
  ├─ 创建或接收 SQLite Store
  ├─ 创建 Application
  ├─ 创建 Scheduler
  ├─ 创建 EventSink / RequestBuilder
  ├─ 创建 ManagedRunner
  ├─ 创建 WorkerFactory / Supervisor
  └─ 创建 HTTP Server
```

下层模块只知道自己需要的接口。不要把整个 `runtimeDependencies` 继续传进 Application 或
Worker，否则它就会变成类似到处调用 `container.get(...)` 的 service locator。

## 17C 中闭包出现在哪里

Supervisor 需要的类型是：

```go
type WorkerFactory func(slot int) (TaskWorker, error)
```

它不是让你定义一个新 struct，而是要求传入“一个函数”。这个函数需要使用已经创建好的
Application、Executor、Builder 和 Sink，因此适合写成闭包：

```text
buildRuntime
  创建 application、managedRunner、builder、sink
  定义 WorkerFactory 匿名函数
    匿名函数捕获上面四个变量
    每次被调用时 new 一个 Worker
  把 WorkerFactory 传给 NewSupervisor
```

闭包的价值不是少写参数，而是把“启动期固定依赖”绑定进工厂；Supervisor 以后只需要提供
每次变化的 `slot`。

## 为什么 buildRuntime 不打开数据库

打开文件、运行 migration、创建 GitManager 和启动 HTTP 都涉及资源生命周期。先把纯装配
单独练会，后面再由 `run()` 负责：

```text
run
  打开资源
  defer 关闭资源
  buildRuntime
  启动 HTTP
  等待退出信号
```

这样 `buildRuntime` 可以全部使用 fake 依赖测试，不需要真实 SQLite、Git 仓库或 Codex 进程。

## 实现顺序

`runtime.go` 已按 1～10 步完成。阅读时可以按以下顺序跟踪变量：

```text
dependencies
  → application / reconciler
  → eventSink / buildRequest / managedRunner
  → newWorker 闭包
  → supervisor / server
  → runtimeComponents
```

运行检查：

```bash
go test ./apps/server/cmd/workbench
```

本步完成标准已经满足：`buildRuntime` 返回非空 Handler 和 Supervisor，但不会监听端口或
执行任务。下一步才由 `run()` 创建真实 Adapter，并管理启动与关闭。

## 练习 17D：像管理页面挂载一样管理进程生命周期

17C 只把对象连起来，17D 才让程序真正“通电”：

```text
main
  → 读取环境配置
  → SignalContext（类似应用级 AbortController）
  → run
      → 打开 SQLite + 执行未应用 migration
      → 创建 Artifact Store / GitManager / Codex Executor
      → buildRuntime
      → 把上次进程遗留的 claimed/running 标记为 failed
      → HTTP Server + Supervisor 并行运行
  ← SIGINT / SIGTERM
      → 取消 Worker 与 Codex
      → HTTP Shutdown
      → 等待 goroutine 退出
      → Close SQLite
```

这里最重要的所有权规则是：**谁创建资源，谁负责释放资源。** `run` 打开数据库，所以在
`run` 中 `defer db.Close()`；`serveRuntime` 启动两个 goroutine，所以返回前等待两者结束。
这和 React effect 返回 cleanup 的思想相近，但 Go 的资源不只包括订阅，还包括连接池、
监听器、goroutine 和子进程。

启动恢复不等于自动重试。前端类比是页面重新加载后，把上次永远停在 `loading` 的请求改成
明确的 `error`，而不是自动再次提交表单。旧任务可能已经修改 Worktree，所以服务端只释放
活动占用并保留失败原因与 Codex Thread ID；真正重试要创建更大的 attempt。

### 启动配置

| 环境变量 | 默认值 | 作用 |
| --- | --- | --- |
| `WORKBENCH_ADDRESS` | `127.0.0.1:8080` | HTTP 地址；无鉴权阶段只允许 loopback |
| `WORKBENCH_DATABASE_PATH` | `.data/server/workbench.db` | SQLite 文件 |
| `WORKBENCH_ARTIFACT_ROOT` | `.data/server/artifacts` | 已确认 TicketVersion JSON 根目录 |
| `WORKBENCH_WORKTREE_ROOT` | `.data/server/worktrees` | 工作台拥有的 Worktree 根目录 |
| `WORKBENCH_ROOT` | 自动从当前目录向上查找 | 工作台 monorepo 根目录 |
| `WORKBENCH_CODEX_COMMAND` | 项目固定的 `@openai/codex` | 显式覆盖受信任 Codex 可执行文件 |
| `WORKBENCH_GIT_COMMAND` | `git` | 受信任 Git 可执行文件 |
| `WORKBENCH_CONCURRENCY` | `2` | 固定 Worker 数量 |

Codex 子进程只继承 PATH、HOME、语言、临时目录、证书和代理等白名单环境变量，不会继承
`AWS_SECRET_ACCESS_KEY`、`GITHUB_TOKEN`、`DATABASE_URL` 或 `NODE_ENV`。因此生产运行前应先用
Codex 自己的登录机制完成认证，不依赖把通用宿主凭据暴露给 Worktree 中执行的命令。

### 练习 20：同一进程交付 SPA

`buildRuntime` 最后用 `internal/webui` 包装 `httpapi.Server`：`/api/*` 保持应用接口语义，
其它 GET/HEAD 请求读取编译期嵌入的 Vite 文件。无扩展名路径回退 `index.html`，静态资源
缺失则返回 404。前端、API、SSE 和 Supervisor 因而共享同一个进程生命周期和监听地址。

从仓库根目录运行：

```bash
pnpm workbench        # 构建 SPA 后运行 Go
pnpm build:standalone # 生成 apps/server/workbench 单二进制
```

### TicketVersion artifact 格式

文件位置是 `<artifact-root>/<requirementId>/<ticketVersionId>.json`。它是一份确认后不可变的
执行 manifest，不是浏览器临时请求体：

```json
{
  "id": "req-1-tickets-v1",
  "requirementId": "req-1",
  "repositoryRoot": "/absolute/path/to/repository",
  "baseRevision": "完整的 Git commit SHA",
  "requirementBranch": "workbench/req-1",
  "tickets": [
    {
      "id": "ticket-a",
      "blockedBy": [],
      "prompt": "实现 ticket-a 的确认内容",
      "modificationScopes": ["apps/web"]
    }
  ]
}
```

Artifact Store 从同一文件产生两种投影：Scheduler 只读取 `id/blockedBy`，Worker 才读取
Prompt、仓库和 Worktree 计划。这样依赖计算不需要知道执行细节。
