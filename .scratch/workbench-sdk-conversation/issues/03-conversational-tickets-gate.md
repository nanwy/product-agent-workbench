# 03 — 对话式 Tickets 门禁

**What to build:** 用户确认 Spec 后，在同一只读规划对话中进入 `to-tickets`。系统自动加载适配后的 Skill，将当前 Spec 拆成可独立执行的 tracer-bullet Tickets，明确 blocking edges，并以本地一票一文件和对话卡片呈现。用户确认当前版本后才开放 Implement。

**Blocked by:** 02 — 对话式 Spec 门禁.

**Status:** completed

- [x] 本地 `to-tickets` Skill 明确只发布工作台本地文件，禁止创建外部 Issue、标签或远程 blocking 关系。
- [x] “确认 Spec”只推进到 `to-tickets`；每次 Tickets Turn 都明确加载对应 Skill并保持目标项目只读。
- [x] Tickets 使用 tracer-bullet 纵向切片，每票能独立演示或验证，并声明真实阻塞边。
- [x] 当前 Tickets 版本按 Requirement 存储为一票一文件，文件按依赖顺序编号且不写入目标项目。
- [x] 对话卡片展示票名、阻塞关系、交付行为、验收条件和版本状态；详情不在对话外重复。
- [x] 用户通过 composer 调整粒度、依赖、拆分或合并时生成新版本，并失效旧版本。
- [x] Spec 发生修订时全部下游 Tickets 自动失效，工作流返回 `to-tickets`。
- [x] 只有当前有效 Tickets 版本可以确认；重复确认幂等，过期版本和错误阶段的确认在服务端被拒绝。
- [x] SDK 失败、取消和刷新恢复保留当前草稿、对话与重试能力。
- [x] 服务端状态机、本地一票一文件、blocking edges 和浏览器内 Tickets 审阅/确认流程通过测试。
