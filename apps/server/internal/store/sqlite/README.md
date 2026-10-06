# Go `database/sql` 使用前必读

这份说明只解释当前 SQLite Store 会用到的 Go API。先理解数据是怎样从 SQL
进入 Go 结构体的，再实现 `task_run.go`。

## 先记住整体流程

```text
Go 发出 SQL
    ↓
SQLite 执行并返回一行列值
    ↓
Scan 按 SELECT 列顺序把值写入 Go 变量
    ↓
组装成 domain.Requirement / domain.TaskRun
```

`database/sql` 不会自动把表变成 struct。SQL 负责选哪些列，`Scan` 负责把每列
放进哪个 Go 变量。

## 当前会遇到的 API

| API | 什么时候用 | 返回什么 |
| --- | --- | --- |
| `ExecContext` | `INSERT`/`UPDATE`/`DELETE`，不需要读取数据行 | `sql.Result`，可继续读 `RowsAffected` |
| `QueryRowContext` | 预期最多只有一行，例如按 ID 查询 | `*sql.Row`，继续调用 `Scan` |
| `QueryContext` | 可能有多行，例如列出事件 | `*sql.Rows`，需要 `Next`/`Scan`/`Close`/`Err` |
| `Scan` | 把数据库列值复制到 Go 变量 | `error` |
| `RowsAffected` | 判断 INSERT/UPDATE 实际改了几行 | `(int64, error)` |

`Context` 版本的方法会接收 `ctx`。请求取消或超时后，数据库操作可以尽快
停止；因此 Store 不使用没有 Context 的 `Exec`/`Query` 版本。

## `QueryRowContext` 和 `Scan` 分别做什么

项目中已完成的 `GetRequirement` 可以拆成两步：

```go
row := s.db.QueryRowContext(ctx, query, id)

err := row.Scan(
    &requirement.ID,
    &requirement.ProjectID,
    &requirement.Title,
    &requirement.Stage,
    &requirement.Revision,
)
```

### `QueryRowContext`

- `query` 是 SQL 模板。
- `id` 会绑定给 SQL 中的 `?`。
- 返回的 `row` 代表“一行待读结果”。
- 查询错误通常在后面的 `Scan` 时返回，所以这里没有单独的 `err`。

### `Scan`

`Scan` 的意思是“按位置把列值写进 Go 变量”。`&` 表示把变量的地址交给
`Scan`，否则它无法修改这个变量。

```sql
SELECT id, project_id, title, stage, revision
```

必须严格对应：

```text
第 1 列 id          → &requirement.ID
第 2 列 project_id  → &requirement.ProjectID
第 3 列 title       → &requirement.Title
第 4 列 stage       → &requirement.Stage
第 5 列 revision    → &requirement.Revision
```

数量、顺序或类型不对，`Scan` 就会返回错误。查询没有找到行时，`Scan` 返回
`sql.ErrNoRows`。

## 为什么 TaskRun 不直接写一长串 `Scan`

Requirement 只有 5 列，TaskRun 却包含状态、4 个时间、3 个结果字段和失败原因。
`GetTask` 和以后的 `ClaimNextTask` 都要读同样的列。如果复制两遍长 `Scan`，其中一处调整
列顺序时，另一处很容易忘记修改。

所以项目会自己定义：

```go
func scanTaskRun(scanner rowScanner) (domain.TaskRun, error)
```

`scanTaskRun` **不是 Go 标准库函数**，是我们要写的项目辅助函数。它做三件事：

1. 集中写一次 `scanner.Scan(...)`。
2. 把数据库的 `NULL` 还原成 Go 的 `nil`。
3. 组装并返回 `domain.TaskRun`。

`rowScanner` 也是项目自定义的小接口：

```go
type rowScanner interface {
    Scan(dest ...any) error
}
```

它表示“任何拥有 `Scan` 方法的对象”。`*sql.Row` 和 `*sql.Rows` 都满足这个接口，
所以单行查询和多行查询可以复用同一个还原函数。

## 数据库 `NULL` 为什么不能直接 Scan 到 `int64`

queued TaskRun 还没有 `claimed_at`，数据库中存的是 `NULL`。`int64` 只能表示数字，
不能表示“还没发生”，所以先使用：

```go
var claimedAt sql.NullInt64
```

`sql.NullInt64` 包含两个信息：

- `Valid == false`：数据库值是 `NULL`。
- `Valid == true`：数在 `Int64` 字段中。

还原时的思路是：

```go
if claimedAt.Valid {
    value := time.Unix(0, claimedAt.Int64).UTC()
    run.ClaimedAt = &value
}
```

`sql.NullString` 同理，用来区分数据库 `NULL` 和真正的空字符串 `""`。

## 三种方法怎样选

```text
只写数据，不读行        → ExecContext
按 ID 读取最多一行     → QueryRowContext + Scan
读取多行列表            → QueryContext + Next + Scan
```

当前 TaskRun 练习先完成 `EnqueueTask`。它只写入数据，所以只需要
`ExecContext` 和 `RowsAffected`，暂时不需要写 `scanTaskRun`。

## 下一步：事务 API 与原子 Claim

`ClaimNextTask` 比普通查询多一个要求：**“选中最早任务”和“把它改成 claimed”必须是一个
不可分割的操作**。否则两个 Worker 可能同时读到同一条 queued 记录，然后都认为自己领取
成功。

### `BeginTx`、`Rollback`、`Commit` 分别做什么

```go
tx, err := s.db.BeginTx(ctx, nil)
if err != nil {
    // 事务还没开始，直接返回包装后的错误。
}
defer tx.Rollback()
```

- `BeginTx(ctx, nil)`：从连接池取得一个连接并开始事务，返回 `*sql.Tx`。
- `tx.Rollback()`：放弃这次事务内尚未提交的修改。把它写在 `defer` 中可以覆盖所有提前
  返回的错误路径。
- `tx.Commit()`：永久提交事务内的修改；只有所有步骤都成功后才调用。
- `Commit` 成功后，延迟执行的 `Rollback` 只会得到 `sql.ErrTxDone`，这里忽略它即可，已经
  提交的数据不会被撤销。

测试连接串已经配置 `_txlock=immediate`。因此这里的事务从开始时就申请 SQLite 写权限，
两个连接会依次执行 claim，而不是同时选中同一条 queued 记录。SQLite 没有 PostgreSQL 的
`SELECT ... FOR UPDATE`，所以不能照搬那种写法。

### 事务里为什么必须使用 `tx`

开始事务以后，本次领取的所有数据库操作都要通过同一个 `tx`：

```go
row := tx.QueryRowContext(ctx, query)
result, err := tx.ExecContext(ctx, update, args...)
```

如果中途改用 `s.db.QueryRowContext` 或调用内部使用 `s.db` 的 `s.SaveTask`，操作可能落到连接池
里的另一个连接，已经不属于这个事务。`ClaimNextTask` 因此要在方法内部用 `tx` 完成查询和
条件更新，不能直接复用当前的 `SaveTask`。

### 实现顺序

```text
BeginTx
  ↓
SELECT ... WHERE status = 'queued' ORDER BY enqueue_seq LIMIT 1
  ↓
scanTaskRun(row)
  ├─ sql.ErrNoRows → store.ErrNotFound
  └─ 其他错误 → 包装后返回，defer 自动 Rollback
  ↓
queued.Claim(now)             // 仍由领域方法校验状态和时间
  ↓
tx.ExecContext 条件更新
WHERE id = ? AND status = 'queued'
  ↓
确认 RowsAffected == 1
  ↓
tx.Commit
  ↓
返回 claimed TaskRun
```

条件中的 `status = 'queued'` 是第二道并发保护。虽然 immediate 事务已经让 SQLite 写事务
串行化，但条件更新仍明确表达“只有 queued 才能被领取”，也防止以后事务配置改变时静默
覆盖新状态。

当前 `scanTaskRun` 已经能拒绝“TaskResult 四列只有一部分非 NULL”的损坏数据。SQLite Store
完成前还要补一层读取校验，拒绝数据库中与状态不一致的时间、结果和失败原因组合；这属于
持久化边界加固，不阻塞先学习事务 claim。
