# Product Agent Workbench 全栈升级设计

> 状态：目标设计，不代表已经实现。
>
> 目的：把当前可运行的 TypeScript MVP 渐进迁移为本地 Go 全栈产品，并把迁移过程本身变成可讲、可测、可演示的面试项目。本文只固定模块、接口语义、数据流、学习顺序和验收标准；核心逻辑由项目作者实现。

## 1. 目标 MVP 架构

最终形态是一个本地模块化单体，而不是云控制平面：

```text
Browser
  ├─ React + TypeScript + Vite
  ├─ TanStack Router / Query
  └─ EventSource（只接收失效通知和进度）
             │ HTTP + SSE
             ▼
Go workbench process
  ├─ HTTP interface / embedded SPA
  ├─ Requirement workflow
  ├─ Task lifecycle + bounded scheduler
  ├─ SQLite persistence + ordered events
  ├─ Codex app-server JSON-RPC adapter
  ├─ managed Git worktree adapter
  ├─ target process + preview gateway
  └─ verification + delivery handoff
             │
             ├─ native codex child process
             ├─ git executable
             └─ target project toolchain
```

进程边界只有三个：浏览器、Go 工作台、工作台拥有的子进程。MVP 不拆 daemon，不做多机器 runtime，不引入 WebSocket。Go 不是 Codex runtime；它只负责启动、监督、中断原生 `codex app-server`，并通过 stdin/stdout 交换 JSON-RPC。

架构采用模块化单体和手工构造器注入。原因不是追求“分层”，而是当前只有一个可执行程序；每个外部系统在真实变化点设置 seam，避免为假想替换能力制造浅模块。

## 2. 借鉴 Multica 的内容

### 保留

- 借鉴显式任务生命周期，但按本地语义命名为：`queued → claimed → running → completed | failed | cancelled`。Multica 的 `dispatched` 表示远程派发，本项目没有这层含义。
- 只有合法前态才能迁移；重复完成、失败或取消必须返回现有事实或明确冲突，不能静默覆盖。
- claim、状态变更和事件追加在同一事务中完成。
- task run 独立于 Requirement 阶段；执行失败不会伪造业务阶段成功。
- 有序事件持久化；浏览器断线后从数据库事实恢复，而不是依赖内存消息。
- Runner seam 隔离“控制面决定做什么”和“本机怎样执行”。
- 受控并发、取消、超时和进程退出后的 reconciliation。

### 不保留

- 云端与本地 runtime 联邦、heartbeat 注册和多机器调度。
- workspace membership、多租户、Agent 市场和多厂商 Agent。
- Electron/Web 双端、WebSocket、对象存储和生产部署设施。
- Linear 式通用 issue/project 管理。

这里的目标不是缩小版 Multica，而是把它严谨的任务生命周期接到本项目更强的 Spec、Git 隔离、真实预览、验证矩阵和人工 Handoff 上。

## 3. Go 模块布局

脚手架目录只声明责任，尚未实现：

```text
apps/server/cmd/workbench/          最薄的进程入口：配置、装配、信号退出
apps/server/internal/app/           生命周期总装配；启动和关闭各模块
apps/server/internal/domain/        Requirement、Ticket、TaskRun、Validation 的状态规则
apps/server/internal/httpapi/       HTTP/SSE interface；鉴权不在本地 MVP 范围
apps/server/internal/store/         持久化 seam；SQLite adapter 放在其子目录
apps/server/internal/runner/        Runner interface、调度、取消、超时、恢复
apps/server/internal/codex/         app-server 子进程与 JSON-RPC adapter
apps/server/internal/workspace/     clone/worktree、分支、范围检查与清理
apps/server/internal/verification/  命令、Git、浏览器证据和验证矩阵
apps/server/migrations/             人工评审的 SQLite migrations
apps/web/                           当前 Web 应用；迁移完成后成为由 Go embed 的 Vite SPA
```

最关键的深模块是 Runner。调用者只应知道“一次 Run 的输入、事件 sink、最终结果和取消语义”，不应知道 JSON-RPC id、子进程 stdout 分帧、Git 命令或心跳细节。SQLite store 同样以用例级方法表达事务，而不是把通用 CRUD 泄漏给上层。

## 4. SQLite 数据模型草案

这一节是建模输入，不是 migration SQL。写 migration 前需要逐项确认查询模式、唯一约束、外键行为和索引。

| 表 | 主要职责 | 关键关系/约束 |
| --- | --- | --- |
| `projects` | 受控目标项目配置与 preflight 结果 | repository identity 唯一；命令来自配置而非产品输入 |
| `requirements` | 产品需求身份、当前阶段、revision、度量时间点 | 第一阶段可保留 `snapshot_json` 渐进迁移；阶段只经领域迁移函数改变 |
| `spec_versions` | 不可变 Spec 历史与确认事实 | `(requirement_id, version)` 唯一 |
| `ticket_versions` | 一次票拆分的版本头 | 只有一个版本可被 Requirement 激活 |
| `tickets` | 有序纵向执行票 | `(ticket_version_id, position)` 唯一；依赖只能指向同版本更早位置 |
| `task_runs` | 一次可调度执行尝试及 Codex thread/turn、worktree 事实 | 重试新增 attempt，不覆盖失败历史；同 Requirement 默认最多一个 active run |
| `events` | 可 replay 的有序领域/执行事件 | 单调 `id`；记录 requirement revision 与可公开 payload |
| `validation_records` | `verified/failed/not-run` 证据 | 属于 task run；`not-run` 必须有 reason |
| `delivery_artifacts` | commit、diff stat、handoff 路径 | 只在产品验收后创建；不等于 production merge |

SQLite 约定：启用 foreign keys；写事务短小；单机 writer 使用有限连接数；所有数据库操作传播 `context.Context`；动态值参数化；事件 payload 在写入前脱敏。不要把完整目标仓库、Skill 正文或秘密写进数据库。

### revision 与事件

每次改变 Requirement 可见事实时：

1. 在同一个事务中校验预期 revision。
2. 写业务表并把 revision 加一。
3. 追加一条包含新 revision 的事件。
4. 提交后才通知内存 SSE hub。

SSE 事件只是“发生了变化”的低成本投影。客户端收到事件后按 Requirement id 重新读取 snapshot；断线重连使用 `Last-Event-ID` 补发有限窗口内的事件，窗口外直接要求重新拉取完整 snapshot。

## 5. 从创建需求到 Handoff

```text
创建 Requirement
  → Grilling 形成并确认共同理解
  → 生成不可变 Spec version
  → 确认 Spec，生成 ticket version
  → 边界确认
  → 创建 task run（queued）
  → scheduler 原子 claim（claimed）
  → Runner 创建 managed worktree，启动/续接 Codex（running）
  → 按 Ticket DAG 计算 ready 队列，先同步执行，再升级为独立 worktree 的有界并行
  → host verification + browser evidence
  → completed 或 failed/cancelled
  → 产品查看真实 preview 并验收
  → 创建候选 commit 与 delivery handoff
  → 前端工程师审查；工作台不 push、不合并生产分支
```

HTTP 请求只负责可靠地登记动作，不等待长任务。Runner 使用脱离请求但属于应用生命周期的 context；用户取消、超时或服务关闭都必须能到达 Codex turn 和自有子进程。服务重启后，数据库中仍为 `claimed/running` 的 run 被显式标记为失败，重试时创建新的 attempt，不能覆盖旧记录或假装继续运行。

## 6. 当前实现到目标模块的映射

| 当前文件/模块 | 当前事实 | 目标处理 |
| --- | --- | --- |
| `apps/web/src/domain/workbench.ts` | 完整 Requirement 聚合和浏览器共享类型 | 保留语义；状态规则迁到 `apps/server/internal/domain`，前端改用 HTTP contract 类型 |
| `apps/web/src/server/requirement-store.ts` | JSON 原子写、单活动 Requirement、历史快照 | 被 SQLite store 替换；先写兼容导入器，再停止双写 |
| `apps/web/src/server/workbench-service.ts` | 多阶段用例集中，文件过大 | 按 workflow 用例拆入 Go，但保持单一 Requirement 状态机 |
| `apps/web/src/server/workbench-execution.ts` | 内存 Promise、心跳、串行票执行 | 拆为持久 task lifecycle + scheduler + Runner |
| `apps/web/src/server/codex-app-server.ts` | 原生进程 JSON-RPC 已真实可用 | 按协议行为移植到 `apps/server/internal/codex`，不改变业务语义 |
| `apps/web/src/server/codex-adapter.ts` | 后端选择和事件归一化 | 保留归一化思想；MVP 删除静默 fallback |
| `apps/web/src/server/git-workspace.ts` | 在用户 checkout 创建需求分支 | 替换为 managed clone/worktree，避免占用或污染用户 checkout |
| `apps/web/src/server/verification.ts` | 主机命令、范围和浏览器证据 | 移植到 `apps/server/internal/verification`；保持三值结果 |
| `apps/web/src/server/target-runtime.ts` | 自有进程组、健康检查和安全复用 | 移植到 Runner 内部 adapter；保留 ownership 规则 |
| `apps/web/src/server/preview-gateway.ts` | localhost proxy + iframe/HMR | 移植到 Go reverse proxy；继续限制 loopback |
| `apps/web/src/server/agent-events.ts` / `apps/web/src/routes/api.events.ts` | 内存 SSE 通知，无 replay | 改为数据库事件游标 + 有界 replay + snapshot revision |
| `apps/web/src/server/functions.ts` | 浏览器动作白名单 | 替换为语义化 HTTP action endpoints 与幂等键 |
| `apps/web/src/store/workbench-query.ts` | snapshot 权威、SSE 触发失效 | 保留；把 Server Functions adapter 改为 HTTP client |
| `apps/web/src/routes/index.tsx` / UI | 当前完整可演示界面 | 后端迁移期间保留；最终去除 Node Server Functions，构建成 Vite SPA |

## 7. 分阶段实施与学习验收

每个阶段都必须形成独立、可回退、可讲解的提交。不要先删 Node 实现。

### Phase 0：设计与脚手架（本次）

- 固定进程边界、模块 seam、数据模型和迁移顺序。
- 创建可编译但不执行业务的 Go 包目录。
- 验收：`go test ./...` 可发现全部包；现有 `pnpm` 验证不受影响。

学习输出：能用两分钟解释为什么是模块化单体、为什么 SSE 不是状态源、为什么 Runner 是深模块。

### Phase 1：领域状态机与内存 adapter

- 先实现纯 Go 的 Requirement/TaskRun 合法迁移。
- 用内存 store 和 fake Runner 驱动 HTTP contract test。
- 暂不接 SQLite、Codex 或 Git。
- 验收：表驱动测试覆盖全部合法/非法迁移；重复命令语义明确；`go test -race ./...` 通过。

学习重点：领域不变量、乐观并发、interface 应定义在消费方。

### Phase 2：SQLite 与恢复

- 人工评审首个 migration 后，实现 SQLite adapter。
- 实现 Requirement list/detail、版本历史、task claim、事件追加和 revision 冲突。
- 写一次性 JSON 导入器，保留原文件作为备份，不做长期双写。
- 验收：进程退出再启动后 snapshot、事件顺序和 task 状态一致；事务回滚不留下半条事实。

学习重点：事务、约束、索引依据、`sql.ErrNoRows`、context 和连接池。

### Phase 3：HTTP + SSE 控制面

- 提供 versioned JSON endpoints、幂等 mutation、统一错误 envelope。
- SSE 支持 id、heartbeat、`Last-Event-ID` 和窗口外 snapshot recovery。
- 先让现有前端通过 adapter 调 Go；不要同时重写 UI。
- 验收：断线、刷新、重复提交和慢客户端测试通过。

学习重点：HTTP 语义、流式响应、背压、服务端生命周期。

### Phase 4：Runner + Codex

- 实现有界 scheduler 和 Runner seam。
- 移植 app-server 初始化、thread resume、turn start/interrupt、通知归一化和日志脱敏。
- task run context 不绑定启动它的 HTTP request，但绑定应用 shutdown、用户取消和超时。
- 验收：fake Codex contract test、真实本地 smoke test、取消和异常退出无 goroutine/进程泄漏。

学习重点：子进程、JSON-RPC、structured concurrency、恢复语义。

### Phase 5：managed worktree、验证与预览

- 为每次 Requirement 创建受管 checkout/worktree。
- 移植目标进程 ownership、preview reverse proxy、Git 范围验证和三值验证矩阵。
- 验收：用户原 checkout 有脏改动也不会被触碰；越界文件阻止交付；只清理工作台拥有的资源。

学习重点：Git object/worktree 模型、进程组、reverse proxy、安全边界。

### Phase 6：Vite SPA 与单二进制

- 将 `apps/web` 从 TanStack Start 迁为 Vite SPA，Server Functions 全部替换为 HTTP client。
- Zustand 只保留瞬时 UI 状态；服务端事实全部由 TanStack Query 管理。
- Go `embed` 生产构建并支持 SPA fallback；开发态仍由 Vite proxy Go。
- 验收：一个命令构建；一个 Go 可执行文件启动 UI/API/SSE；Playwright 主流程通过。

学习重点：前后端契约、缓存失效、静态资源嵌入、跨平台打包。

### Phase 7：面试证据

- 写 benchmark/性能记录：SSE fan-out、snapshot 查询、事件 replay。
- 补故障演示：服务重启、Codex 退出、worktree 冲突、验证失败。
- 记录真实改造前后数据；没有记录就只称“目标指标”。
- 更新架构图、ADR、面试 walkthrough 和三分钟演示脚本。

## 8. 风险与验证策略

| 风险 | 控制方式 | 必须有的测试 |
| --- | --- | --- |
| 状态迁移竞态 | 数据库前态条件 + revision | 同一 run 并发完成/取消只有一个成功 |
| SSE 丢事件 | snapshot 权威 + replay 游标 | 断线跨多事件后恢复一致 |
| goroutine/子进程泄漏 | 明确 owner、context、Wait | race/leak test + 真实取消 smoke |
| 用户仓库被污染 | managed worktree + ownership registry | 脏 checkout fixture 保持字节级不变 |
| Codex 协议变化 | 单一 adapter + fixture transcript | JSON-RPC contract fixtures |
| 前后端大爆炸迁移 | compatibility adapter，按 seam 替换 | 新旧 snapshot contract 对照测试 |
| SQLite 成为“只是换存储” | task/event/revision 真实使用事务语义 | crash recovery 与原子 claim 集成测试 |

单元测试覆盖纯状态规则；SQLite、Git、进程和 HTTP 使用集成测试；最终价值流仍由 Playwright 从浏览器验证。不要用 mock 证明真实 worktree、进程组或浏览器页面可用。Spec/Ticket 文件制品继续由独立 ArtifactStore 原子发布：先写文件，再在 SQLite 中记录引用，并允许清理无引用 orphan，不伪造跨文件系统和数据库的事务。

## 9. 面试叙事

面试中最有竞争力的不是“用了 Go + React”，而是以下工程判断：

- 把浏览器、控制面和执行器的责任分开，但拒绝过早拆 daemon。
- 用 SQLite task/event/revision 取代内存 Promise，解决重启恢复和可审计性。
- 用 SSE 通知 + snapshot 权威模型解决实时性与正确性冲突。
- 用 managed worktree、进程 ownership、命令 allowlist 和验证矩阵收窄 Agent 风险。
- 用 native Codex app-server JSON-RPC adapter 展示真实子进程与协议工程，而不是 SDK 套壳。

### 目标简历表述（实现完成后才能使用）

> 设计并实现本地规格驱动研发控制面：使用 Go、SQLite 与 React 管理 Requirement/TaskRun 状态机，通过 SSE 事件 replay 支持断线恢复；以 Ticket DAG 计算 ready 队列，通过 JSON-RPC 监督 Codex app-server，在受管 Git worktree 中执行并整合候选变更，并通过真实预览、主机验证矩阵和人工 Handoff 保留生产合并权。

在 Phase 2～6 未完成前，只能表述为“正在将 TypeScript MVP 迁移到上述架构”，不能把目标能力写成已交付事实。
