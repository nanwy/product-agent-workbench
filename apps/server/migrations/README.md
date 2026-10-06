# SQLite Migration

这里将保存经过评审、只向前执行的 SQLite migration。

在第一阶段固定领域状态迁移前，不要创建第一份 SQL。写 SQL 前必须明确回答：

1. 哪些查询会列出 Requirement，按哪些列排序或过滤？
2. 哪条唯一约束阻止同一 Ticket 同时存在两个活动 TaskRun，同时允许同一 Requirement
   的无依赖 Ticket 并行？
3. 哪些外键应该级联、限制删除或保留审计历史？
4. 调用方看到的 Requirement revision 如何参与每次写操作？
5. 事件 id 如何分配，SSE replay 保留多大的窗口？
6. 哪些 payload 字段必须在持久化前脱敏？
7. 每个索引服务于哪条明确查询，而不是凭感觉添加？

回答被记录后，再手写 migration，单独评审，并使用临时数据库和升级 fixture 验证。

## 练习 11 设计记录（已确认）

这一节先固定“数据库必须表达什么”，不是最终 migration SQL。第一次写 SQL 时不要同时做
业务建模；按本节逐项翻译成 SQLite 语法即可。

### 1. 当前查询与索引边界

当前 Store 只有以下读取：

- 按 `requirements.id` 读取一条 Requirement；
- 按 `task_runs.id` 读取一条 TaskRun；
- 按稳定入队顺序领取最早的 queued TaskRun；
- 按 `(task_run_id, sequence)` 顺序读取某次执行之后的事件。

目前没有 ListRequirements 用例，因此第一份 migration 不为“可能出现的列表页”提前建立
`title`、`stage` 等索引。以后先增加公开用例和查询，再用真实查询形状设计索引。

### 2. Driver 与连接约定

- Driver 选择 `modernc.org/sqlite`：通过 `database/sql` 使用且不依赖 CGO，适合本地桌面工具。
- driver 只由 `cmd/workbench` 导入；`internal/store/sqlite` 继续只依赖 `*sql.DB`。
- 每个物理连接都要启用 foreign keys；不能只在某一个临时连接执行一次 PRAGMA。
- 写事务使用 immediate 模式并设置有限 busy timeout；连接池至少保留两个连接，以便并发
  claim 测试真实覆盖两个独立连接。
- 集成测试使用 `t.TempDir()` 下的磁盘数据库，不直接使用彼此隔离的 `:memory:` 连接。

### 3. 时间编码

所有时间统一保存为 UTC Unix 纳秒整数：

- `created_at` 必填；
- `claimed_at`、`started_at`、`finished_at` 未发生时为 NULL；
- 写入前转换为 UTC，读取后恢复为 UTC；
- 可空时间在 Go 中用 `sql.NullInt64` 扫描；
- 不依赖 driver 自动解析日期字符串。

### 4. 当前 migration 后的逻辑模型

只创建当前 Store 契约已经使用的三张表，不提前创建 Project、Ticket、Validation 等未来表。

| 表 | 字段 | 约束目的 |
| --- | --- | --- |
| `requirements` | `id`, `project_id`, `title`, `stage`, `revision` | `id` 唯一；`revision >= 1`；stage 只能取领域枚举 |
| `task_runs` | `enqueue_seq`, `id`, `requirement_id`, `ticket_version_id`, `ticket_id`, `attempt`, `status`, 四个时间、四个 result 字段、`failure_reason` | `id` 唯一；attempt 正数；状态只能取领域枚举 |
| `task_events` | `id`, `task_run_id`, `sequence`, `type`, `payload`, `created_at` | `id` 是全局 SSE 游标；sequence 正数 |

额外业务约束：

- `(requirement_id, ticket_version_id, ticket_id, attempt)` 唯一，同一版本内的同一次重试不能重复创建；
- 同一个 `(requirement_id, ticket_id)` 最多存在一个 `queued`、`claimed` 或 `running`
  TaskRun；同一 Requirement 的不同 Ticket 可以同时活动；
- `(task_run_id, sequence)` 唯一，同一个 TaskRun 的事件序号不能重复；
- `task_runs.requirement_id` 和 `task_events.task_run_id` 使用限制删除的外键，保留审计历史；
- result 四个字段整体表达可选结果；第一版由领域层和行扫描共同校验组合是否合法。

### 5. 稳定入队顺序

`created_at` 可能相同，不能作为唯一队列顺序。`task_runs.enqueue_seq` 由数据库生成、单调递增，
claim 使用 `status + enqueue_seq` 查找最早 queued 记录。业务时间回答“何时创建”，序号回答
“谁先入队”，两者不能混用。

### 6. 原子 claim

SQLite 没有 `SELECT FOR UPDATE`。claim 使用一个 immediate 写事务：

1. 按 `enqueue_seq` 查询最早 queued TaskRun；
2. 扫描为领域对象并调用 `TaskRun.Claim(now)`，保留时间和状态校验；
3. 条件更新 `id` 相同且状态仍为 queued 的行；
4. 更新一行后提交；没有候选返回 `store.ErrNotFound`；任一步失败则回滚。

两个连接同时 claim 时，immediate 写事务让它们串行获得写权限。同一任务最多被领取一次；
练习 12 必须用两个独立连接验证，`go test -race` 不能证明数据库事务正确。

### 7. revision、状态 CAS 与事件

- Requirement 更新条件包含 `id + expectedRevision`，成功后 revision 加一；更新零行时再区分
  `ErrNotFound` 与 `ErrRevisionConflict`。
- TaskRun 保存条件包含 `id + expectedStatus`；更新零行时再区分 `ErrNotFound` 与
  `ErrTaskConflict`。
- TaskEvent 的全局 `id` 单调递增，单 TaskRun 使用独立 sequence。
- 第一版本地数据库不自动删除事件。等 snapshot 与 SSE replay 协议完成后，再定义窗口外
  强制重新读取快照的规则。
- payload 到达 Store 前必须已经脱敏，不保存 token、Cookie、Authorization、完整环境变量
  或未经筛选的子进程输出。

### 8. 每个索引服务的查询

| 索引/约束 | 服务的查询或不变量 |
| --- | --- |
| `requirements.id` 唯一 | `GetRequirement(id)` |
| `task_runs.id` 唯一 | `GetTask(id)` 与状态 CAS |
| `task_runs(status, enqueue_seq)` | `ClaimNextTask()` |
| Ticket 活动态部分唯一约束 | 防止同一 Ticket 重复执行，同时允许同一 Requirement 多票并行 |
| `task_events(task_run_id, sequence)` 唯一 | 连续追加、游标读取与稳定排序 |
| `task_events.id` 全局单调 | 后续 SSE `Last-Event-ID` replay |

不要添加无法指向具体查询的索引。

## 第一次写 migration 的方法

使用 `000001_initial.up.sql.todo` 作为练习纸。它不会被 migration 工具识别，避免半成品误被
应用。按 Requirement → TaskRun → TaskEvent 的顺序填写；完成评审后再改成 migration 工具
要求的正式文件名。

每张表遵循同一个小循环：

1. 先只写字段和主键；
2. 再添加 NOT NULL、数值范围和枚举 CHECK；
3. 再添加外键和唯一约束；
4. 最后只为上表列出的查询添加索引；
5. 用自己的话在 SQL 旁解释每条非显然约束防止了什么错误。

## 当前学习入口

`000001_initial.up.sql` 已完成并通过空库执行、外键、BLOB、部分唯一索引和
队列查询计划验证。`modernc.org/sqlite` 已加入模块，migration 通过 `go:embed`
供启动程序和集成测试读取。生产 driver 仍只由 `cmd/workbench` 导入；测试二进制
在 `_test.go` 中单独注册 driver。

实现顺序：

1. 手写并验证第一份只向前 migration。（已完成）
2. 安装 SQLite driver，嵌入 migration，用独立临时磁盘数据库建立集成测试。（已完成）
3. 复用 Memory Requirement 契约，实现 `sqlite/requirement.go` 的参数化 SQL。（已完成）
4. 完成 TaskRun 行还原，并把活动唯一约束固定为 Requirement + Ticket。（已完成）
5. 用至少两个物理连接验证原子 claim 和 revision 并发，不只依赖 `go test -race`。（已完成）
6. 实现 TaskEvent 整批事务契约。（已完成）
7. Store 稳定后再实现 Ticket DAG Scheduler；依赖图仍来自版本化 artifact，不提前建 Ticket 表。（已完成）

对应旧 TypeScript：`requirement-store.ts` 的 JSON 快照和 Promise 写队列会被事务与约束替代，
但 Spec/Ticket Markdown 仍是文件 artifact，不迁入这些表。切流前禁止同时写 JSON 和 SQLite。
