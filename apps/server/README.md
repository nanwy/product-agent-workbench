# Workbench Go 控制面

这里是未来本地 Go 控制面的学习脚手架。文件已经给出类型、接口、方法签名、中文注释和练习顺序，但核心业务逻辑仍由你实现。

仓库根目录的 `go.work` 让编辑器和根目录命令可以发现本模块。当前临时 module path 是 `product-agent-workbench`；建立正式 Git remote 后，再替换成仓库的 canonical URL。

## 目录职责

- `cmd/workbench`：进程入口和依赖装配入口。
- `internal/domain`：纯 Requirement 与 TaskRun 状态规则，从这里开始。
- `internal/store`：持久化 seam；先写内存 Adapter，再写 SQLite。
- `internal/httpapi`：JSON 动作、Snapshot、健康检查、环境预检和 SSE。
- `internal/runner`：有界调度、取消、超时和重启恢复。
- `internal/codex`：原生 Codex app-server JSON-RPC Adapter。
- `internal/workspace`：受管仓库与 Worktree 生命周期。
- `internal/verification`：验证证据和交付门禁。
- `migrations`：经过评审、只向前执行的 SQLite migration。

## 当前进度：第一阶段已完成

`internal/domain/task_run.go` 已完成 TaskRun 状态机：

- `queued → claimed → running → completed` 主路径；
- claimed/running 进入 failed；
- queued/claimed/running 进入 cancelled；
- 阶段时间存在性、零值和倒流检查；
- completed/failed/cancelled 终态事实互斥；
- 非法迁移返回原 TaskRun 和可识别的领域错误。

领域测试和 race 检查：

```bash
go test ./apps/server/internal/domain -v
go test -race ./apps/server/...
pnpm test:server
```

## 第二阶段：内存 Store

### 学习目标

把领域对象从“单次函数调用中的值”升级为“可以被多个用例安全读写的服务端事实”。这一阶段只使用内存，不引入 SQLite、HTTP 或 goroutine worker。

需要掌握：

- interface 由消费方定义，Adapter 负责满足接口；
- map、`sync.RWMutex` 和临界区；
- `context.Context` 传播，但不把 context 保存进结构体；
- not-found、conflict、revision mismatch 等稳定错误；
- 返回数据副本，避免调用方绕过 Store 修改内部状态；
- `go test -race` 验证并发访问。

### 文件计划

```text
internal/store/
├── store.go                 已有的 Store interface
├── errors.go                Store 层稳定错误
├── contract_test.go         所有 Adapter 必须通过的公共契约
└── memory/
    ├── store.go             map + mutex 内存实现
    └── store_test.go        内存 Adapter 测试入口
```

### 实现顺序

1. 定义 `ErrNotFound`、`ErrAlreadyExists`、`ErrRevisionConflict`。
2. 实现 Requirement 的 create/get，不允许重复 ID。
3. 实现带 `expectedRevision` 的 save；成功后 revision 加一。
4. 实现 TaskRun enqueue/get/save，不能覆盖不存在的任务。
5. 实现原子 claim：同一临界区内选择 queued 任务并迁移状态。
6. 实现事件追加，保证同一 TaskRun 的 sequence 严格递增。
7. 并发运行 create/save/claim，使用 race detector 验证没有数据竞争。

### 怎么开始

1. 打开 `internal/store/memory/store.go`，先阅读 `Store` 的字段和锁保护范围。
2. 打开 `internal/store/contract_test.go`，只删除“练习 1”中的 `t.Skip`。
3. 运行下面的单组测试，观察占位实现为什么失败。
4. 只实现 `CreateRequirement`、`GetRequirement` 和 `SaveRequirement`，不要提前写后面的函数。
5. 第一组通过后再解锁练习 2；每次只处理一个新的失败。

```bash
go test ./apps/server/internal/store -run 'TestMemoryStoreContract/Requirement_创建、读取与乐观锁更新' -v
go test -race ./apps/server/internal/store/...
```

内存 Store 已完成后，不再保留 `memory.ErrNotImplemented`。所有 Store 契约默认执行，不能用
`t.Skip` 隐藏回归。

### 验收标准

- 重复创建返回 `ErrAlreadyExists`，不覆盖原值。
- 不存在的 Requirement/TaskRun 返回 `ErrNotFound`。
- revision 不匹配返回 `ErrRevisionConflict`，原值不变。
- 两个 goroutine 同时 claim 时，同一 TaskRun 最多成功一次。
- list/get 返回副本，调用方修改返回值不影响 Store 内部事实。
- 事件顺序稳定，重复 sequence 被拒绝。
- `go test -race ./apps/server/internal/store/...` 通过。

这一阶段不要实现：SQLite migration、HTTP handler、SSE、Codex 和 Git。

## 第三阶段：应用用例与 HTTP JSON 接口

### 已完成：Application 的 Requirement 用例

`internal/app` 已完成依赖注入、Requirement 创建与读取、输入规范化、Store 错误透传和
context 传播，三组测试默认全部执行。

### 已完成：GET /health

健康检查已完成 GET 方法限制、JSON 响应、错误方法 405 和多余响应内容检查。

### 当前小步：Requirement HTTP endpoint

现在连接第一条完整纵向请求链路：

```text
HTTP JSON → Requirement handler → Application → Store
          ← JSON response    ← Domain result
```

练习顺序：

1. 删除 `TestServer_CreateRequirement` 的 `t.Skip`，实现 `writeJSON`、响应 DTO 映射和
   创建 handler 的成功路径。
2. 删除 `TestServer_CreateRequirementRejectsInvalidJSON` 的 `t.Skip`，完成严格
   `decodeJSON`。
3. 删除 `TestServer_MapsApplicationErrors` 的 `t.Skip`，完成集中错误映射。
4. 删除 `TestServer_GetRequirement` 的 `t.Skip`，实现路径参数查询。
5. 四组通过后删除私有的 `errNotImplemented` 占位错误和所有失效占位代码。

```bash
go test ./apps/server/internal/httpapi -run TestServer_CreateRequirement -v
go test ./apps/server/internal/httpapi -run TestServer_MapsApplicationErrors -v
go test ./apps/server/internal/httpapi -run TestServer_GetRequirement -v
go test -race ./apps/server/...
```

这一步 handler 只能调用 `RequirementApplication`，不能导入 memory Store、实现领域校验，
也不能把内部错误文本直接返回给浏览器。

### 学习目标

让浏览器能通过普通 HTTP 调用 Go，但先不做 SSE 和长任务执行。HTTP 层只处理协议，`internal/app` 负责编排领域与 Store。

需要掌握：

- `net/http`、`http.ServeMux`、JSON 编解码和状态码；
- handler、应用用例、领域状态机、Store 的职责分离；
- request context 从 handler 传到应用和 Store；
- `httptest` 契约测试；
- expected revision 与幂等键的传递；
- 领域错误到 HTTP 状态码的集中映射。

### 首批接口

```text
GET  /api/v1/health
POST /api/v1/requirements
GET  /api/v1/requirements/{id}
POST /api/v1/task-runs
POST /api/v1/task-runs/{id}/claim
POST /api/v1/task-runs/{id}/start
POST /api/v1/task-runs/{id}/complete
POST /api/v1/task-runs/{id}/fail
POST /api/v1/task-runs/{id}/cancel
```

### 文件计划

```text
internal/app/
├── application.go           依赖装配与用例入口
├── requirement.go           Requirement 用例
└── task_run.go              TaskRun 用例

internal/httpapi/
├── contracts.go             请求、响应和错误 JSON 合约
├── server.go                ServeMux 与统一中间件
├── requirement_handler.go   Requirement endpoint
├── task_run_handler.go      TaskRun action endpoint
└── server_test.go           httptest 黑盒测试
```

### 实现顺序

1. 完成 `app.New` 的依赖校验，不启动后台 goroutine。
2. 先实现 `GET /health`，理解 ServeMux 和 httptest。
3. 实现 Requirement create/get 用例，再接两个 HTTP endpoint。
4. 实现 TaskRun enqueue 和状态动作；handler 不直接调用领域方法或 Store。
5. 集中映射错误：非法 JSON、输入错误、资源不存在、revision 冲突、非法状态迁移。
6. 写 HTTP 黑盒测试，断言状态码、Content-Type 和 JSON body。

### 建议状态码

| 场景 | 状态码 |
| --- | --- |
| 创建成功 | `201 Created` |
| 查询或动作成功 | `200 OK` |
| JSON/输入无效 | `400 Bad Request` |
| Requirement/TaskRun 不存在 | `404 Not Found` |
| revision 或状态迁移冲突 | `409 Conflict` |
| 未知服务端错误 | `500 Internal Server Error` |

### 验收标准

- handler 中没有 SQL、mutex、Git 或 Codex 逻辑。
- 所有 JSON 响应都设置正确的 `Content-Type`。
- 未知 JSON 字段和空 body 的处理策略明确并有测试。
- 同一幂等键重复提交不会创建两条 TaskRun。
- revision 冲突返回 409，不覆盖较新的事实。
- request 被取消后，Store 调用能收到 context cancellation。
- `go test -race ./apps/server/internal/app/... ./apps/server/internal/httpapi/...` 通过。

这一阶段不要实现：SQLite、SSE replay、Runner 调度、Codex 子进程、Git Worktree 和前端迁移。它们从第四阶段开始。
