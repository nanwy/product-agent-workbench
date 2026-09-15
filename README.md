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
- 干净工作区检查、独立需求分支、连续 Codex session、SSE 事件与串行执行票。
- 宿主验证矩阵、显式产品验收、稳定候选提交与前端交付 Handoff。

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
pnpm test:e2e
pnpm build
```

Playwright 使用独立 fixture 目标项目，不会启动或修改真实业务仓库。

## Skill 分发

产品工作流位于 `.agents/skills/`，会随 Git 仓库一起交付。执行 Codex 时，工作台只传递 `SKILL.md` 的入口路径，由 Codex 自行读取正文和按需引用的资源，不在 Requirement JSON 或 prompt 中缓存 Skill 全文。

目标业务仓库可以在自己的 `.agents/skills/` 或 `.codex/skills/` 中提供项目专属 Skill；工作台负责发现和激活，Codex 负责读取和执行。默认从工作台仓库根目录启动；其他部署方式可通过 `WORKBENCH_ROOT` 指定本仓库绝对路径。

## 代码地图

- `src/domain/`：Requirement 聚合、阶段和公共数据契约；
- `src/server/`：配置、持久化、规则探索、进程和预览网关；
- `src/server/git-workspace.ts`：Git 起点记录、脏工作区保护和需求分支；
- `src/server/codex-app-server.ts`：app-server 子进程、JSON-RPC、Thread 续接、Turn 中断与清理；
- `src/server/codex-adapter.ts`：Codex 后端选择、应用事件归一化及显式 SDK 回退；
- `src/server/workbench-conversation.ts`：四阶段逻辑对话、Grilling Skill 注入与门禁协议；
- `src/store/workbench-query.ts`：React Query Key、工作台快照收敛和按 Requirement 隔离的对话投影；
- `src/server/verification.ts`：声明式命令、修改范围和真实路由验证；
- `src/routes/`：页面编排和 Server Function 调用；
- `src/components/`：需求、证据与预览三个可讲解的界面模块；
- `tests/fixtures/`：确定性目标项目和仓库规则；
- `tests/e2e/`：浏览器级主验收流程。

项目背景和领域词汇见 `CONTEXT.md`，完整需求见 `.requirements/`，面试讲解顺序见 `docs/interview-walkthrough.md`。
