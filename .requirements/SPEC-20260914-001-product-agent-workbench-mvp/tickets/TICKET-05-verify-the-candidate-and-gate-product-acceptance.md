# TICKET-05 验证候选实现并建立产品验收门禁

**Type:** AFK  
**Status:** Partial

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

- [ ] `ValidationRecord` 包含稳定 ID、关联的 Spec 验收条件、结果、来源、详情、命令、退出码、起止时间和 `not-run` 原因。
- [ ] 配置按顺序声明并由宿主执行类型检查、测试和构建命令；输出限长并脱敏，超时终止整个自有进程组。
- [ ] 每条 Spec 验收条件都有对应矩阵记录，不能用泛化的“页面可访问”冒充产品行为验证。
- [ ] `not-run` 在类型层或构造函数层强制携带原因；必要项的 `failed` 与 `not-run` 都阻止进入产品验收。
- [ ] Git 范围检查独立贡献证据，范围外修改为失败。
- [ ] 使用 `agent-browser` 访问托管预览，通过 Accessibility Tree 和语义化引用记录目标路由、相关 viewport、关键行为和新增 console error。
- [ ] 验证代码、脚本和留存证据中不使用 Computer Use；探索结果明确后再用 Playwright 固化稳定断言。
- [ ] UI 验证面板展示 `verified`、`failed`、`not-run`、证据摘要、命令退出码和未运行原因。
- [ ] 环境恢复后可单独重跑矩阵，不重放 Codex turn、不重建需求分支。
- [ ] 全部必要自动门禁通过后才允许产品显式验收；服务端不能只依赖按钮 disabled 状态。
- [ ] 产品验收创建稳定 commit 和页面交付卡，并明示产品验收不等于生产批准。
- [ ] Playwright 覆盖全量通过、代码检查失败、环境导致 `not-run`、console error 和验收门禁五条路径。

## Blocked by

- TICKET-04 在安全需求分支中连续执行 Codex 票。

## Explicitly excluded

- 本票不生成目标仓库内的最终 Spec/Ticket/Handoff 文件，也不执行真实目标冒烟；这些属于 TICKET-07。
- 不连接 GitHub、CI 或部署系统。

## Audit result · 2026-09-15

已实现：`ValidationRecord` 基础结构、宿主命令执行、退出码/时间/脱敏末尾日志、Git 范围检查、HTTP 路由检查、验证面板、单独重跑、failed 服务端门禁、显式产品验收、稳定 commit 和页面交付卡。

仍缺：

- 验证矩阵没有逐条消费 `RequirementSpec.acceptanceCriteria` 或 Ticket 验收条件，只生成命令、Git、HTTP 和产品四类泛化记录。
- 没有接入 `agent-browser`；仓库没有 Accessibility Tree、语义化引用或视觉探索证据。
- 没有记录相关 viewport，也没有采集“相对基线新增的 browser console error”。
- 运行时验证只是服务端 `fetch` 后检查 HTTP 状态，不能证明目标行为在真实页面可见。
- `not-run` 原因在类型上仍是 nullable 字段，没有构造约束；除 failed 外，必要的 `not-run` 也会进入待产品验收。
- UI 面板未展示证据 `source` 与起止时间。
- 真实目标配置目前只声明 typecheck，没有声明 test/build；通用执行器虽支持多命令，但没有跑这两类检查。
- Playwright 只有成功主流程，没有 failed、环境 `not-run`、console error 和验收门禁场景。

`docs/issues/0001-0004` 中更深入的环境清理、代理策略、首个错误保留和旧端口安全复用仍是独立后续问题，不计入本票已完成范围。
