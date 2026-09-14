# Product Agent Workbench

本地规格驱动开发工作台：产品提交产品需求，系统展示 SDD 阶段、仓库生效规则和真实目标页面，为后续 Codex 执行、验证与前端交付建立可审计边界。

## 当前能力

- 产品需求创建、阶段证据和 JSON 持久恢复；
- `grilling → to-spec → to-tickets → implement` 的显式工作流门禁；
- 固定目标项目配置、开发进程组启动/停止及健康检查；
- localhost-only 预览网关、iframe 刷新和外部打开；
- 根与最近目录 `AGENTS.md` 解析；
- 可用/生效 Skill 区分及激活原因；
- 真实代码位置探索、候选 Spec、按结果切分的执行票和高风险边界确认。
- 干净工作区检查、独立需求分支、连续 Codex session、SSE 事件与串行执行票。

## 本地运行

```bash
fnm use 24
pnpm install
pnpm dev
```

默认连接相邻目录中的 `Mysterious-Realm-v2/apps/front`。目标仓库、启动命令、健康地址和预览地址由 `config/target-project.json` 显式配置，产品输入不能修改命令。

## 验证

```bash
pnpm typecheck
pnpm test
pnpm test:e2e
pnpm build
```

Playwright 使用独立 fixture 目标项目，不会启动或修改真实业务仓库。

## 代码地图

- `src/domain/`：Requirement 聚合、阶段和公共数据契约；
- `src/server/`：配置、持久化、规则探索、进程和预览网关；
- `src/server/git-workspace.ts`：Git 起点记录、脏工作区保护和需求分支；
- `src/server/codex-adapter.ts`：CLI 探测、JSONL 归一化、续接与取消；
- `src/routes/`：页面编排和 Server Function 调用；
- `src/components/`：需求、证据与预览三个可讲解的界面模块；
- `tests/fixtures/`：确定性目标项目和仓库规则；
- `tests/e2e/`：浏览器级主验收流程。

项目背景和领域词汇见 `CONTEXT.md`，完整需求见 `.requirements/`，面试讲解顺序见 `docs/interview-walkthrough.md`。
