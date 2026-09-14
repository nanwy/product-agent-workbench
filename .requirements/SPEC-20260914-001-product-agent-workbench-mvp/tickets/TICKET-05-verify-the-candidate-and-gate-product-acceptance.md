# TICKET-05 验证候选实现并建立产品验收门禁

**Type:** AFK  
**Status:** Complete

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

全部执行票完成后，由工作台宿主运行目标配置声明的类型检查、测试和构建命令，并把命令、退出码、起止时间、脱敏日志与 Git 范围检查写入验证矩阵。每条 Spec 验收条件必须具有稳定 ID，并被归类为 `verified`、`failed` 或带原因的 `not-run`。

UI 验收采用两层流程：先使用 `agent-browser` 的 Accessibility Tree 和语义化引用探索真实目标路由，在相关 viewport 下确认关键行为并记录新增 browser console error；再用 Playwright 固化已经明确的稳定断言。不得使用 Computer Use 代替这条流程。

验证面板同时显示候选实现状态和矩阵证据。任何必要的 `failed` 或 `not-run` 都必须停留在 verification；环境恢复后只能重跑验证，不能重放 Codex 或重建分支。全部自动门禁通过后，才显示产品验收按钮；产品点击后创建稳定候选 commit 和页面交付卡，但不 push、不建 PR、不合并或发布。

## User stories covered

- US-43～US-50：验证命令、三态矩阵、运行时与 console 证据、产品验收责任。
- US-51、US-52、US-55：稳定候选提交、页面交付卡和前端生产所有权。

## Acceptance criteria

- [x] `ValidationRecord` 包含稳定 ID、关联的 Spec 验收条件、结果、来源、详情、命令、退出码、起止时间和 `not-run` 原因。
- [x] 配置按顺序声明并由宿主执行类型检查、测试和构建命令；输出限长并脱敏，超时终止整个自有进程组。
- [x] 每条 Spec 验收条件都有对应矩阵记录，不能用泛化的“页面可访问”冒充产品行为验证。
- [x] `not-run` 在构造函数层强制携带原因；必要项的 `failed` 与 `not-run` 都阻止进入产品验收。
- [x] Git 范围检查独立贡献证据，范围外修改为失败。
- [x] 使用 `agent-browser` 访问托管预览，通过 Accessibility Tree 和语义化引用记录目标路由、相关 viewport、关键行为和新增 console error。
- [x] 验证代码、脚本和留存证据中不使用 Computer Use；探索结果明确后再用 Playwright 固化稳定断言。
- [x] UI 验证面板展示 `verified`、`failed`、`not-run`、证据摘要、命令退出码和未运行原因。
- [x] 环境恢复后可单独重跑矩阵，不重放 Codex turn、不重建需求分支。
- [x] 全部必要自动门禁通过后才允许产品显式验收；服务端不能只依赖按钮 disabled 状态。
- [x] 产品验收创建稳定 commit 和页面交付卡，并明示产品验收不等于生产批准。
- [x] Playwright 覆盖全量通过、代码检查失败、环境导致 `not-run`、console error 和验收门禁五条路径。

## Blocked by

- TICKET-04 在安全需求分支中连续执行 Codex 票。

## Explicitly excluded

- 本票不生成目标仓库内的最终 Spec/Ticket/Handoff 文件，也不执行真实目标冒烟；这些属于 TICKET-07。
- 不连接 GitHub、CI 或部署系统。

## Completion note · 2026-09-15

验证现在分别记录命令、Git 范围、健康端点、页面 HTTP 与 agent-browser 证据，并逐条映射 `SPEC-AC-NN`。机器无法诚实判断的产品语义条件保持非机器门禁的 `not-run`，在产品结合真实预览确认后转为 `verified`；必要机器项的 `failed/not-run` 则始终阻止验收。真实目标配置已按 typecheck、focused test、production build 的顺序补齐。
