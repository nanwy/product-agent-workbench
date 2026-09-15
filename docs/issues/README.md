# 本地 Issues

以下问题来自执行票 `TICKET-01` 的真实运行记录。代码候选实现只有一行文案变更，定向 ESLint、类型检查和 diff 检查均通过；暴露的问题集中在工作台传递的 Skill 真值和验证基础设施。

| Issue | 类型 | 依赖 | 目标 |
| --- | --- | --- | --- |
| [0001](./0001-align-executable-skills-with-codex-capabilities.md) | AFK | 无 | 只把执行 Agent 实际可用的 Skill 声明为生效 Skill |
| [0002](./0002-run-declared-validation-on-the-workbench-host.md) | AFK | 无 | 由工作台宿主运行确定性的类型检查与构建命令 |
| [0003](./0003-verify-pages-through-the-managed-preview-runtime.md) | AFK | 无 | 通过工作台托管的目标项目进程和预览网关验收页面 |
| [0004](./0004-record-an-honest-verification-matrix.md) | AFK | 0002、0003 | 区分实现失败、验证失败和因环境未运行 |

已解决的 Codex CLI `-a` 参数兼容问题不列为待办；其回归测试已随修复落地。
