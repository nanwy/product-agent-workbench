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

## 当前进度：练习 1～11 已完成，练习 12 进行中

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

### 已完成：Requirement HTTP endpoint

现在连接第一条完整纵向请求链路：

```text
HTTP JSON → Requirement handler → Application → Store
          ← JSON response    ← Domain result
```

已完成内容：

1. `writeJSON`、响应 DTO 映射和创建 handler 成功路径；
2. 严格 `decodeJSON`、1 MiB 请求体上限和单个 JSON 值约束；
3. 应用错误的集中 HTTP 状态码映射；
4. 路径参数查询、方法限制和 request context 传播。

```bash
go test ./apps/server/internal/httpapi -run TestServer_CreateRequirement -v
go test ./apps/server/internal/httpapi -run TestServer_MapsApplicationErrors -v
go test ./apps/server/internal/httpapi -run TestServer_GetRequirement -v
go test -race ./apps/server/...
```

handler 只调用 `RequirementApplication`，没有导入 memory Store、重复领域校验或把内部错误
文本直接返回给浏览器。

### 已完成：TaskRun enqueue/get 应用用例

先完成 TaskRun 的创建与读取，把“HTTP 调用 Store”的复杂度收进 Application 模块。此小步
暂时不接 HTTP，也不做 claim/start/complete 等状态动作。

已完成内容：

1. `app.New` 校验并保存 `Clock`，构造期间不读取时间；
2. 入队前清理输入、检查父 Requirement，再生成 ID 和创建时间；
3. 使用领域构造函数创建 queued TaskRun，并交给 Store 持久化；
4. 按规范化 ID 查询 TaskRun，传播 context 与 Store 错误。

```bash
go test ./apps/server/internal/app -run TestNewRejectsMissingClock -v
go test ./apps/server/internal/app -run TestApplication_EnqueueTaskRun -v
go test ./apps/server/internal/app -run TestApplication_GetTaskRun -v
go test -race ./apps/server/...
```

这一小步的关键不是多写两个转发函数，而是把输入规范化、父实体存在性、ID、时间、领域构造
和持久化的顺序固定在一个深模块里。完成后再增加 TaskRun 状态动作和对应 HTTP endpoint。

### 已完成：TaskRun claim/start 应用用例

这一小步把“任务被调度器占有”和“执行进程真正开始”拆成两个可持久化事实：

```text
queued --ClaimNextTaskRun--> claimed --StartTaskRun--> running
```

已完成内容：

1. `ClaimNextTaskRun` 通过 Store 原子领取 queued TaskRun；
2. `StartTaskRun` 规范化 ID、调用领域 `Start` 并保存 running 事实；
3. context 取消、资源不存在和非法前态均不会产生错误状态写入。

```bash
go test ./apps/server/internal/app -run TestApplication_ClaimNextTaskRun -v
go test ./apps/server/internal/app -run TestApplication_StartTaskRun -v
go test -race ./apps/server/...
```

这里必须复用 `Store.ClaimNextTask` 的原子操作，不能在 Application 中组合 Get/Save 模拟
claim。Application 负责用例顺序，Store 负责并发原子性，领域对象负责状态是否合法。

### 已完成：完成一次 TaskRun

这一步只实现成功终态：

```text
running --CompleteTaskRun(result)--> completed
```

已完成内容：

1. context → 规范化 ID → GetTask → Clock；
2. 领域 `Complete` 校验前态、结果与时间；
3. SaveTask 成功后返回 completed TaskRun。

```bash
go test ./apps/server/internal/app -run TestApplication_CompleteTaskRun -v
go test -race ./apps/server/...
```

它只对应原 `workbench-execution.ts/executeQueue` 中“当前 Codex turn 与范围检查成功后，记录
本次执行完成”的部分。继续下一张 Ticket、运行验证矩阵和发布事件仍由后续 Runner、
verification 与事件模块负责，不能塞进这个 Application 用例。

## 后续学习骨架：已经铺到最小 Worker

下面的文件已经存在。练习 7～10 已完成；下一步从练习 11 开始。后续仍然一次只解锁一个公开
seam 的测试，保持 red → green。

### 练习 7：FailTaskRun

- 状态：已完成。
- 文件：`internal/app/task_run_terminal.go`
- 测试：`TestApplication_FailTaskRun`
- 实现：context → 清理 ID/Reason → GetTask → Clock → `run.Fail` → SaveTask。
- 对应旧代码：`workbench-execution.ts/recordExecutionFailure` 中保存失败终态的部分。

```bash
go test ./apps/server/internal/app -run TestApplication_FailTaskRun -v
```

### 练习 8：CancelTaskRun

- 状态：已完成。
- 文件：`internal/app/task_run_terminal.go`
- 测试：`TestApplication_CancelTaskRun`
- 实现：context → 清理 ID → GetTask → Clock → `run.Cancel` → SaveTask。
- 对应旧代码：`cancelRequirementExecution` 中保存 cancelled 的部分；中断 Codex 属于 Worker。

```bash
go test ./apps/server/internal/app -run TestApplication_CancelTaskRun -v
```

### 练习 7～8 补充：为什么保存时还要判断旧状态

Application 会先读取 TaskRun，再调用领域方法完成状态迁移，最后保存。若取消和失败两个请求
同时读到 `running`，它们各自在内存里得到 `cancelled` 与 `failed` 都是合法的；只有给 map 加锁，
后保存的旧快照仍会覆盖先保存的终态。这属于业务逻辑竞争，不是内存数据竞争，因此
`go test -race` 不会替我们发现它。

当前 Store 使用状态 CAS（compare-and-swap）解决：Application 在迁移前保存
`originalStatus`，调用 `SaveTask(run, originalStatus)`；Memory Store 在同一个写锁临界区内比较
当前状态并写入。状态已变化时返回 `store.ErrTaskConflict`，HTTP 层映射为 `409 Conflict`。
SQLite 练习必须用等价的条件更新：

```sql
UPDATE task_runs SET ... WHERE id = ? AND status = ?;
```

不能在事务外先查状态、再执行无条件 UPDATE。回归测试位于 Store 公共契约中的
“TaskRun 状态 CAS 阻止旧快照覆盖并发终态”。

### 练习 9～10：TaskRun HTTP

- 状态：已完成。
- 文件：`internal/httpapi/task_run_handler.go`、`contracts.go`、`server.go`
- 练习 9：实现 DTO 映射与 `POST /api/v1/task-runs`。
- 练习 10：实现 `GET /api/v1/task-runs/{id}` 与
  `POST /api/v1/task-runs/{id}/cancel`。
- 对应旧代码：浏览器启动执行、读取快照和取消执行的 Server Function 入口。

只有浏览器需要的 enqueue/get/cancel 暴露为 HTTP。claim/start/complete/fail 是 Worker 内部
生命周期，不能让浏览器伪造。

```bash
go test ./apps/server/internal/httpapi -run TestServer_EnqueueTaskRun -v
go test ./apps/server/internal/httpapi -run TestServer_GetAndCancelTaskRun -v
```

### 练习 11～12：SQLite Store

- 入口：`internal/store/sqlite/` 与 `migrations/README.md`。
- 练习 11（已完成）：选定 driver、UTC 编码、稳定入队顺序和原子 claim 策略；
  手写的 `000001_initial.up.sql` 已通过 SQLite 执行、外键和索引验证。
- 练习 12 Requirement（已完成）：Create/Get/Save 通过公共契约、20 次重复运行和
  `go test -race`；revision 条件 UPDATE 能阻止旧快照覆盖。
- 练习 12 TaskRun 基础（已完成）：`EnqueueTask`、`GetTask`、`SaveTask` 与 `scanTaskRun`
  已通过公共契约、20 次重复运行、race detector 和 vet；状态 CAS 会拒绝旧快照覆盖。
- 练习 12 原子 claim（当前）：事务 API 学习说明、公共顺序/并发/context 契约和中文实现
  步骤已解锁，核心 `ClaimNextTask` 由你实现。TaskEvent 尚未解锁。
- 使用数据库 API 前先读 `internal/store/sqlite/README.md`，分清 `ExecContext`、
  `QueryRowContext`、`QueryContext`、`Scan` 和项目自定义的 `scanTaskRun`。
- 对应旧代码：`requirement-store.ts` 的 JSON 快照、临时文件 rename 和 Promise 写队列。

SQLite Adapter 接收 `*sql.DB`，不锁死 driver、不在构造函数迁移，也不使用 ORM。旧 JSON
Store 与 SQLite 在正式切流前不双写。

当前 Red 测试（现在应当失败在 `ClaimNextTask` 尚未实现）：

```bash
go test ./apps/server/internal/store -run TestSQLiteClaimStoreContract -v
```

TaskRun 对应原 TypeScript `prepareExecution` 中把 queued Ticket 放入 JSON 聚合，以及
`executeQueue`/`recordExecutionFailure` 把后续状态覆盖回文件。SQLite 改为每次执行独立一行，
并用 `WHERE id = ? AND status = ?` 代替进程内旧快照写回。

### 练习 13：最小同步 Worker

- 文件：`internal/runner/worker.go`
- 测试：`TestWorker_RunNextCompletesTaskWithFakeExecutor`
- 实现：claim → start → BuildRequest → fake Executor.Run → complete；失败时进入 fail。
- 对应旧代码：`prepareExecution + executeQueue` 的最小纵向切片。

```bash
go test ./apps/server/internal/runner -run TestWorker_RunNextCompletesTaskWithFakeExecutor -v
```

第一版 `RunNext` 必须保持同步，不创建 goroutine。等 fake executor 端到端通过后，再增加
Supervisor、轮询、并发上限、重启恢复和 Codex Adapter。

### 当前解锁顺序

```text
7 FailTaskRun（已完成）
→ 8 CancelTaskRun（已完成）
→ 9 TaskRun enqueue HTTP（已完成）
→ 10 TaskRun get/cancel HTTP（已完成）
→ 11 migration 设计（已完成）
→ 12 SQLite Requirement 契约（已完成）
→ 12 SQLite TaskRun 入队/读取/CAS（已完成）
→ 12 SQLite 原子 claim（当前）
→ 12 SQLite TaskEvent 契约
→ 13 fake executor Worker
```

完成练习 13 后，Go 后端会第一次具备“不依赖真实 Codex 的完整任务执行闭环”。随后才进入
Codex app-server、Git Worktree、验证矩阵、事件/SSE 和前端切流。

### Go 迁移步骤与原 TypeScript 实现的对应关系

Go 版本不是逐文件翻译。原项目把 Ticket、Codex 状态和后台 Promise 都塞在一个 Requirement
聚合里；Go 会把“一次执行尝试”独立成 TaskRun，才能保存重试历史并安全恢复。

| Go 学习步骤 | 这一阶段实现什么 | 原项目对应位置 | 迁移后的变化 |
| --- | --- | --- | --- |
| Requirement Application | 创建、查询需求，固定输入与持久化顺序 | `apps/web/src/server/workbench-service.ts` 的需求创建与读取流程 | Server Function 中的业务编排进入 Go Application |
| TaskRun enqueue/get | 为某张 Ticket 创建一次 queued 执行尝试并查询 | `workbench-execution.ts` 的 `prepareExecution` 以前直接从 queued Ticket 开始执行 | 新增独立 TaskRun；一次重试是一条新记录，不再覆盖 Ticket 上的运行事实 |
| Clock 依赖 | 给 TaskRun 提供可测试的创建/迁移时间 | 原代码散落的 `new Date().toISOString()` | 时间成为显式依赖，单元测试不依赖真实时钟 |
| claim/start 动作（已完成） | 调度器原子领取最早任务，再标记 Runner 真正启动 | `prepareExecution` 的 queued 领取部分，随后 `executeQueue` 调用 `runCodexTurn` | 拆开“已被调度器占有”和“子进程已经开始”，便于恢复与排障 |
| complete 动作（已完成） | 保存成功结果和完成时间 | `executeQueue` 中当前 Ticket 成功收尾 | 状态迁移统一经过领域状态机；调度下一票与验证仍留给 Runner |
| fail/cancel 动作（已完成） | 保存失败原因或取消终态 | `recordExecutionFailure`、`cancelRequirementExecution` | 失败与取消不再通过对象展开手写状态；状态 CAS 防止旧快照覆盖并发终态 |
| TaskRun HTTP（已完成） | 浏览器创建、查询和取消执行 | `startRequirementExecution`、`cancelRequirementExecution` 等 Server Function | 前端改为普通 HTTP client；handler 只处理协议 |
| SQLite Store（进行中） | Requirement 与 TaskRun 基础已完成；当前实现原子 claim | `requirement-store.ts` 的 JSON 文件和 Promise 写队列 | JSON 聚合替换为可查询、可原子 claim 的关系数据 |
| 最小 Worker（骨架） | 同步编排一次 fake 执行 | `prepareExecution`、`executeQueue` | 先验证闭环，再增加并发和真实 Codex |

当前实际完成到 SQLite TaskRun 基础；学习骨架已铺到最小 Worker。不要提前调用真实 Codex；
先让 SQLite claim、TaskEvent 契约和 fake executor 闭环稳定。

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
GET  /api/v1/task-runs/{id}
POST /api/v1/task-runs/{id}/cancel
```

claim/start/complete/fail 只供 Worker 在进程内部调用，不是浏览器 endpoint。

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

TaskRun HTTP 已完成，但暂不切换前端：Memory Store 无法跨重启保存数据，执行也还没有 Worker
消费。SQLite 和最小 fake Worker 已有后续骨架；SSE replay、真实 Codex 子进程、Git Worktree
和前端迁移仍然留到这些基础契约完成之后。
