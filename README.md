# Product Agent Workbench

本地规格驱动开发工作台：产品提交产品需求，系统展示 SDD 阶段、仓库生效规则和真实目标页面，为后续 Codex 执行、验证与前端交付建立可审计边界。

## 当前能力

- 产品需求创建、阶段证据和 JSON 持久恢复；
- `grilling → to-spec → to-tickets → implement` 的显式工作流门禁；
- 固定目标项目配置、开发进程组启动/停止及健康检查；
- localhost-only 预览网关、iframe 刷新和外部打开；
- 根与最近目录 `AGENTS.md` 解析；
- 可用/生效 Skill 区分及激活原因；
- 随仓库分发的产品工作流 Skill，以及由 Codex 按路径读取的目标项目 Skill；
- 真实代码位置探索、候选 Spec、按结果切分的执行票和高风险边界确认。
- 干净工作区检查、独立需求分支、连续 Codex session、SSE 事件与依赖感知的 Ticket 推进；
  当前 Node 执行循环仍一次消费一票。
- 宿主验证矩阵、显式产品验收、稳定候选提交与前端交付 Handoff。

## Monorepo 结构

```text
apps/web/     React + TanStack Start 前端，以及迁移期间仍在使用的 Node MVP 模块
apps/server/  Go 本地控制面学习骨架；核心逻辑按阶段实现
config/       仓库级目标项目配置
.agents/      随仓库交付的产品工作流 Skills
docs/         架构、面试与学习文档
```

根目录使用 pnpm workspace 统一编排。当前产品仍由 `apps/web` 完整运行；`apps/server` 是渐进迁移目标，不把尚未实现的 Go 能力描述成已完成。

## Go 重构学习进度

截至当前，实际可运行的 Go 重构约完成 **88%**；学习脚手架已经闭合持久化 SSE replay，
约覆盖后端迁移路线的 **96%**。两者必须区分：有类型、注释和编排代码只表示下一步可学习，不表示
该能力已经替代 Node 实现。

| 阶段 | 状态 | 对应旧实现 |
| --- | --- | --- |
| Domain 状态机 | 已完成 | `src/domain/workbench.ts` 中分散的执行状态规则 |
| Memory Store 契约 | 已完成 | `requirement-store.ts` 的内存串行与 JSON 读写语义 |
| Requirement HTTP | 已完成 | Requirement Server Functions 的创建与读取 |
| TaskRun enqueue/claim/start/complete | 已完成（状态 CAS 已接入） | `prepareExecution` 与 `executeQueue` 的主成功路径 |
| TaskRun fail/cancel | 已完成（状态 CAS 已接入） | `recordExecutionFailure`、`cancelRequirementExecution` |
| TaskRun HTTP | 已完成 | 浏览器执行/查询/取消 Server Functions |
| SQLite Adapter | migration、Requirement、TaskRun、原子 claim 与 TaskEvent 已完成 | JSON Store + Promise 写队列 |
| Ticket DAG Scheduler | 已完成 ready 计算、TicketVersion 历史隔离与幂等 Reconcile | Ticket artifact 的 `blockedBy` 与执行队列推进 |
| 最小同步 Worker + fake executor | 已完成同步 claim/start/execute/complete/fail 闭环 | `prepareExecution + executeQueue` 的最小纵向切片 |
| 有界多 Worker + Worktree 编排 | 已完成固定槽位、范围门禁、整合 seam 与完成后 DAG 重算 | Node 当前串行执行，无对应并行能力 |
| 真实 Git Worktree Adapter | 已完成 ownership、隔离提交、非快进整合、冲突回滚和重启恢复 | `git-workspace.ts` 当前占用用户 checkout |
| 真实 Codex Adapter 与进程装配 | 已完成 | 现有 Node 高权限模块与进程生命周期 |
| Verification 执行门禁 | 纯规则已完成；真实证据采集与交付接线待完成 | `verification.ts` 与交付前检查 |
| 持久化 SSE | 已完成全局游标、断线重放、heartbeat 和游标失效通知 | Node 进程内事件订阅与浏览器状态同步 |
| 前端切流 | 待完成 | Server Functions、Node 执行入口与 React Query 订阅 |

详细的解锁顺序、测试命令和中文实现提示见 `apps/server/README.md`。

## 本地运行

```bash
fnm use 24
pnpm install
pnpm dev
```

默认连接相邻目录中的 `Mysterious-Realm-v2/apps/front`。目标仓库、启动命令、健康地址和预览地址由 `config/target-project.json` 显式配置，产品输入不能修改命令。

如需复用用户已经启动的目标服务，可配置 `runtimeReuse.healthBodyIncludes`。工作台只在健康响应正文包含该项目专属标记时复用；否则把端口视为身份不明的占用者，既不覆盖也不终止。

Codex Turn 默认使用项目固定版本的 `codex app-server`。`WORKBENCH_CODEX_COMMAND` 只用于受控的可执行文件覆盖；临时回退旧 SDK 时必须显式设置 `WORKBENCH_CODEX_BACKEND=sdk`，app-server 失败不会静默切换后端。

## 验证

```bash
pnpm typecheck
pnpm test
pnpm test:server
pnpm test:e2e
pnpm build
```

Playwright 使用独立 fixture 目标项目，不会启动或修改真实业务仓库。

## Skill 分发

产品工作流位于 `.agents/skills/`，会随 Git 仓库一起交付。执行 Codex 时，工作台只传递 `SKILL.md` 的入口路径，由 Codex 自行读取正文和按需引用的资源，不在 Requirement JSON 或 prompt 中缓存 Skill 全文。

目标业务仓库可以在自己的 `.agents/skills/` 或 `.codex/skills/` 中提供项目专属 Skill；工作台负责发现和激活，Codex 负责读取和执行。默认从工作台仓库根目录启动；其他部署方式可通过 `WORKBENCH_ROOT` 指定本仓库绝对路径。

## 代码地图

- `apps/web/src/domain/`：Requirement 聚合、阶段和公共数据契约；
- `apps/web/src/server/`：迁移期间的 Node 配置、持久化、规则探索、进程和预览网关；
- `apps/web/src/server/git-workspace.ts`：Git 起点记录、脏工作区保护和需求分支；
- `apps/web/src/server/codex-app-server.ts`：app-server 子进程、JSON-RPC、Thread 续接、Turn 中断与清理；
- `apps/web/src/server/codex-adapter.ts`：Codex 后端选择、应用事件归一化及显式 SDK 回退；
- `apps/web/src/server/workbench-conversation.ts`：四阶段逻辑对话、Grilling Skill 注入与门禁协议；
- `apps/web/src/store/workbench-query.ts`：React Query Key、工作台快照收敛和按 Requirement 隔离的对话投影；
- `apps/web/src/server/verification.ts`：声明式命令、修改范围和真实路由验证；
- `apps/web/src/routes/`：页面编排和 Server Function 调用；
- `apps/web/src/components/`：需求、证据与预览三个可讲解的界面模块；
- `apps/web/tests/fixtures/`：确定性目标项目和仓库规则；
- `apps/web/tests/e2e/`：浏览器级主验收流程；
- `apps/server/internal/`：Go 领域、持久化、HTTP、Runner 与外部 adapter 的学习骨架。

项目背景和领域词汇见 `CONTEXT.md`，完整需求见 `.requirements/`，面试讲解顺序见 `docs/interview-walkthrough.md`。
