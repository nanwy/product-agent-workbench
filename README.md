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

根目录使用 pnpm workspace 统一编排。原 TanStack Start 产品工作台仍是默认入口；已经迁入
Go 的资源接口、SSE 和单二进制页面继续保留，等待 21E 在不改页面逻辑的前提下完成切流。

## Go 重构学习进度

截至当前，Go 全栈重构约完成 **96%**：步骤 1～20，以及 21A～21D 已落地。
21E 仍需在保留原页面布局和交互逻辑的前提下替换数据边界；旧 Node 服务源码不会删除。

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
| Verification 执行门禁 | 已完成候选 Git 检查、宿主命令、三值证据、历史报告和 acceptance 门禁 | `verification.ts` 与交付前检查 |
| 持久化 SSE | 已完成全局游标、断线重放、heartbeat 和游标失效通知 | Node 进程内事件订阅与浏览器状态同步 |
| 前端切流 | 19A Client/DTO、19B Query/Mutation、19C 持久化 SSE 已完成 | Server Functions、Node 执行入口与 React Query 订阅 |
| 单二进制交付 | Vite SPA、embed.FS、路由 fallback 和单命令启动已完成 | 原 TanStack Start/Nitro 独立 Node 进程 |
| Grilling 迁移 | 21A 共同理解确认、阶段 CAS、SQLite 持久化和 SPA 表单已完成 | Node Grilling 门禁与聚合内字段 |
| Spec 迁移 | 21B 只读 Codex 生成、不可覆盖版本、确认门禁和 SPA 已完成 | `workbench-spec.ts` 与 Spec Server Functions |
| Tickets 迁移 | 21C Ticket DAG 生成、版本持久化、manifest 发布和 SPA 已完成 | `workbench-tickets.ts` 与 Tickets Server Functions |
| 目标预览迁移 | 21D Go 进程托管、健康检查、安全外部复用、iframe 反向代理和 SPA 控制卡已完成 | `target-runtime.ts` 与 `preview-gateway.ts` |
| 默认入口切流 | 21E 已完成 Target Runtime 局部切流；完整页面切流统一在步骤 25 完成 | TanStack Start Server Functions 保留为迁移参照 |
| 对话持久化基础 | 22 已完成 Conversation、Turn、Message 领域模型、SQLite migration 和双 Store 契约 | `workbench-conversation.ts` 中嵌套在 Requirement JSON 的对话状态 |
| 对话领域状态机 | 23A 已完成创建、开始/结束 Turn、失败/取消和阶段推进的纯状态迁移 | `beginAssistantTurn`、`finishImplementationTurn` 与各阶段对象展开更新 |
| 对话运行闭环 | 23B～23F 已完成原子写入、事件收口、只读 Runtime、Grilling 回复/重试/取消 | Node Grilling 对话、AbortController 与后台 Promise |
| 对话原子写入 | 23B 已完成 Requirement/Conversation/初始消息创建事务，以及开始 Turn 的 Conversation/Turn/消息事务 | Node 中分散在 JSON 聚合更新和对话辅助函数里的多字段写入 |
| 对话应用用例 | 23C 已完成原子创建聚合与开始 Turn 的应用编排，ID、时间、revision 和 sequence 均由服务端控制 | `createRequirement`、`appendUserMessage`、`beginAssistantTurn` 的持久化前半段 |
| 对话事件与终态 | 23D 已完成 Codex text/activity 事件折叠，以及 completed/failed/cancelled 三元组原子收口 | `applyPlanningEvent` 与 `finishPlanningTurn` 的持久化后半段 |

详细的解锁顺序、测试命令和中文实现提示见 `apps/server/README.md`。
后续迁移步骤的唯一编号与完成标准见 [`docs/go-migration-roadmap.md`](docs/go-migration-roadmap.md)。

## 本地运行

```bash
fnm use 24
pnpm install
pnpm workbench
```

浏览器打开 `http://127.0.0.1:8080`。该命令先构建 Vite SPA，再启动同时提供页面、API、
SSE 和 Worker 的 Go 进程。生成独立二进制使用 `pnpm build:standalone`。

原产品工作台通过 `pnpm dev` 启动并访问 `http://127.0.0.1:3000/`。Go 单二进制仍可用
`pnpm workbench` 启动在 `127.0.0.1:8080`，用于检查已经迁移的 HTTP/SSE 能力。

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
- `apps/web/src/api/`：Go HTTP DTO、统一 fetch client 与错误边界；
- `apps/web/src/routes/index.tsx`：原产品工作台页面与布局逻辑；
- `apps/web/src/spa/`：不包含 Node Server Functions 的嵌入式浏览器入口；
- `apps/server/internal/webui/`：Go embed 静态文件、API 分流和 SPA 路由 fallback；
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
