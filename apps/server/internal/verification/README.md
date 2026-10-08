# Verification 学习纸：17A 纯规则 + 18B 真实执行门禁

这一小步只写纯规则，不运行验证目标里的 `pnpm`、浏览器或 Git 命令；这里列出的
`go test` 只用于检查这段 Go 规则自身。

## 先用前端视角理解

页面现在大致这样判断是否受阻：

```ts
const hasBlockingRecord = records.some(
  (item) => item.required && item.result !== "verified",
)
```

Go 的 `Matrix.BlocksDelivery` 表达同一条业务规则。差别只是 Go 标准库没有数组的
`.some()` 链式方法，需要用 `for range` 和 early return。

```text
遍历 Records
  如果 Required 且 Status 不是 verified
    立即返回 true
遍历结束仍没找到
  返回 false
```

## 为什么还需要 BuildMatrix

前端收到不合法接口数据时，最多是页面渲染异常；后端如果把不合法证据持久化，重启后仍会
继续影响交付判断。因此进入 Matrix 前必须检查：

1. `ID`、`Label` 不是空白；
2. `Status` 只能是三个已知值；
3. `not-run` 必须解释为什么没运行；
4. 同一个 Matrix 不能出现重复 ID；
5. 返回新的 `Records` 切片，不持有调用方的切片。

## TypeScript 与 Go 语法对照

| TypeScript | Go |
| --- | --- |
| `const records: Evidence[] = []` | `records := make([]Evidence, 0, len(evidence))` |
| `const seen = new Set<string>()` | `seen := make(map[string]struct{}, len(evidence))` |
| `for (const item of evidence)` | `for _, item := range evidence` |
| `records.push(item)` | `records = append(records, item)` |
| `seen.has(item.id)` | `_, exists := seen[item.ID]` |
| `seen.add(item.id)` | `seen[item.ID] = struct{}{}` |
| `value.trim()` | `strings.TrimSpace(value)` |
| `throw new Error(...)` | `return Matrix{}, fmt.Errorf(...)` |

`struct{}{}` 是零字节的空结构体。这里不保存额外值，只利用 map 的 key 表示 ID 是否出现过。

## 推荐实现顺序

每完成一步先运行：

```bash
go test ./apps/server/internal/verification
```

1. 实现 `validateEvidence` 的 ID、Label 必填检查；
2. 增加 `switch`，拒绝未知 Status；
3. 增加 `not-run` 的 Reason 检查；
4. 实现 `BuildMatrix` 的 records、seenIDs 和循环；
5. 最后实现 `BlocksDelivery`。

## 自查场景

写完以后，先不要看答案，逐个在脑中运行：

| 输入 | 预期 |
| --- | --- |
| 空数组 | 合法空 Matrix，不阻止交付 |
| required + verified | 不阻止交付 |
| required + failed | 阻止交付 |
| required + not-run + Reason | 阻止交付 |
| not required + failed | 不阻止交付 |
| not-run 但 Reason 为空 | 返回 `ErrInvalidEvidence` |
| 两条记录 ID 相同 | 返回 `ErrInvalidEvidence` |
| Status 是 `"success"` | 返回 `ErrInvalidEvidence` |

17A 完成后再增加证据收集 Adapter。纯函数不应该知道命令、HTTP、Git 或浏览器是如何运行的。

---

## 练习 18B：把“禁用发布按钮”升级为服务端门禁

### 先从前端视角看完整流程

在前端项目里，你可能会写：

```ts
const mutation = useMutation({ mutationFn: verifyRequirement })
const deliverable = !report.records.some(
  record => record.required && record.status !== 'verified',
)
```

但后端不能只相信一个 `deliverable` 布尔值。浏览器可以伪造请求，也可能在刷新后丢失内存。
所以 Go 端需要自己完成整条链路：

```text
浏览器只提交 requirementId + ticketVersionId
  ↓
Application 检查确认版本中的每张 Ticket 都已有 completed TaskRun
  ↓
从 TaskRun 取得服务端保存的 Requirement 整合 Worktree
  ↓
GitCandidateInspector 读取真实 HEAD，并确认工作区干净
  ↓
HostCommandExecutor 串行执行受信任配置里的 typecheck/test/build
  ↓
每个原始结果映射为 Evidence
  ↓
BuildMatrix 校验完整矩阵
  ↓
BlocksDelivery=true  → 保存 blocked 报告，Requirement 留在 implementation
BlocksDelivery=false → 保存 passed 报告，Requirement CAS 推进到 acceptance
```

这里对应三个前端概念：

| 前端概念 | Go 18B 对应 |
| --- | --- |
| mutation input | `VerifyRequirementInput`，只允许业务身份，不允许命令和路径 |
| API adapter | `HostCommandExecutor`、`GitCandidateInspector` |
| derived state | `Matrix.BlocksDelivery()` 计算是否允许进入 acceptance |
| query cache 中的一次 CI run | 持久化的 `verification.Report` |
| optimistic concurrency | `SaveRequirement(expectedRevision)`，防止旧验证覆盖新状态 |

### 为什么 Ticket completed 后还要验证

`completed` 只表示：Codex Turn 成功、当前 Ticket 的修改通过范围检查并整合进 Requirement
分支。它不等于整个候选版本已经通过 typecheck/test/build。

因此这是两组独立状态：

```text
TaskRun: completed       // 实现事实，不因验证失败而回滚
Report:  blocked         // 当前候选暂时不能交给产品验收
Requirement: implementation
```

环境恢复后可以重新运行验证，产生一条新的 Report。旧报告不覆盖，类似 CI 会保留历史 run。

### 三值结果不能合并

| 原始执行事实 | Evidence.Status | 含义 |
| --- | --- | --- |
| 命令退出码 0 | `verified` | 检查已运行并给出正向证据 |
| 命令正常启动但退出码非 0 | `failed` | 检查已运行并发现代码问题 |
| 命令不存在、超时、环境无法访问 | `not-run` | 没有得到完整检查结果，必须附原因 |

`failed` 和 `not-run` 都会阻止必要项交付，但排障方向不同：前者修代码，后者修环境。

### 按文件阅读实现

1. `plan.go`
   - 从受信任的 `config/target-project.json` 读取声明式命令；
   - 校验 ID、超时、环境和代理策略；
   - 浏览器请求不能覆盖这些值。
2. `executor.go`
   - 使用 `exec.CommandContext(executable, args...)`，不经过 Shell；
   - 使用环境变量允许列表，避免把宿主 token 交给验证子进程；
   - 输出最多保存末尾 4 KiB，并对常见秘密字段脱敏；
   - 超时/取消时终止进程组，避免遗留 pnpm/tsc/vitest 子进程。
3. `runtime.go`
   - `requiresRuntime` 命令执行前先请求受信任配置中的 loopback `healthUrl`；
   - 目标服务不可访问时记录 not-run，不把环境问题伪装成测试断言失败；
   - 18B 不接管未知目标进程，当前可复用用户或旧 Node 工作台已经启动的目标服务。
4. `candidate.go`
   - 在 Requirement 整合 Worktree 中读取 `git rev-parse HEAD` 和 `git status`；
   - HEAD 必须属于已完成 TaskRun 保存的整合 revision，且工作区必须干净。
5. `service.go`
   - 串行执行候选检查和验证命令；
   - 把 Adapter 结果转换成统一 Evidence；
   - 构建 Matrix、计算门禁并保存 Report。
6. `sqlite/repository.go`
   - 每次验证只 INSERT，不 UPDATE；
   - `LatestReport` 供后续前端读取最近一次结果；
   - 完整 Matrix 使用 JSON 快照保存，报告身份和查询字段仍使用普通列。
7. `app/requirement_verification.go`
   - 核对全部 Ticket 的最新 attempt 已完成；
   - blocked 时返回报告但不修改 Requirement；
   - passed 时通过 revision CAS 推进到 `acceptance`。
8. `httpapi/verification_handler.go`
   - endpoint：`POST /api/v1/requirements/{id}/verify`；
   - body 只有 `{ "ticketVersionId": "..." }`；
   - blocked 仍返回 200 和完整矩阵，因为它是可展示的业务结果，不是服务器故障；
   - 刷新后用 `GET /api/v1/requirements/{id}/verification?ticketVersionId=...` 恢复最新报告。

### 为什么命令串行执行

同一个 Worktree 中，`build`、开发服务器和测试可能同时写 `.next`、coverage、缓存或临时文件。
并行虽然更快，却会把工具之间的目录竞争误报成代码失败。18B 先保证证据可靠；以后只有确认
彼此不共享输出目录的命令，才适合显式分组并行。

### 自查问题

1. 为什么请求体不能包含 `command`？
2. 为什么 `exit 1` 是 failed，而“可执行文件不存在”是 not-run？
3. 为什么验证失败不能把 completed TaskRun 改成 failed？
4. 为什么验证必须运行在 Requirement 整合 Worktree，而不是最后一张 Ticket Worktree？
5. 为什么报告要保留历史，而不是只在 Requirement 上保存 `passed: true`？
6. 为什么通过后更新 Requirement 仍要使用 expected revision？

### 18B 验收命令

```bash
go test ./apps/server/internal/verification/... -v
go test ./apps/server/internal/app -run VerifyRequirement -v
go test ./apps/server/internal/httpapi -run VerifyRequirement -v
go test -race ./apps/server/...
```
