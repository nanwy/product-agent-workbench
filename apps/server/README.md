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

## 第一阶段怎么开始

1. 打开 `internal/domain/task_run.go`，阅读状态和字段含义。
2. 打开 `internal/domain/task_run_test.go`，只删除 `TestNewTaskRun` 中的 `t.Skip`。
3. 运行测试，观察它因为 `ErrNotImplemented` 失败。
4. 只实现 `NewTaskRun`，直到第一个测试通过。
5. 再解锁 `TestTaskRun_Claim`，实现 `Claim`。
6. 自己为 `Start/Complete/Fail/Cancel` 补充表驱动测试，然后实现。

每次只解锁一个练习：

```bash
go test ./apps/server/internal/domain -run TestNewTaskRun -v
go test ./apps/server/internal/domain -run TestTaskRun_Claim -v
pnpm test:server
```

`internal/store`、`httpapi`、`runner` 中的接口现在只用于告诉你后续模块需要什么。不要提前实现它们；先让领域状态机全部通过。
