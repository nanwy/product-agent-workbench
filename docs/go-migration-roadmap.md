# Go 全栈迁移路线图

> 本文件是 Go 迁移阶段编号、范围和完成标准的唯一事实源。
> 开始任何“步骤 N / 练习 N”任务前，必须先读取本文件，再检查对应代码现状。
> 除非用户明确修改计划，否则不得在对话中重新编号、插入或改名阶段。

## 范围

目标是让 `apps/web/src/routes/index.tsx` 的原 MVP 页面在关闭 Node 服务后，仍可通过 Go
服务完成原有流程。页面布局、组件组合和交互模型保持不变；`apps/web/src/server/**` 保留作学习
对照，不删除。

本轮不包含进程重启后的 Conversation 自动恢复、额外监控、性能优化或新调度能力。这些属于
增强项，不占用 23G、23H 等编号；当前计划中不存在 23G、23H。

## 已完成阶段

- 1～20：Go 服务、SQLite、Scheduler/Worker、Codex Runtime、SSE、嵌入式 SPA。
- 21A～21D：Grilling 确认、Spec、Tickets、Target Runtime 的独立 Go API。
- 21E：仅完成 Target Runtime 在原页面中的局部切流；完整切流并入步骤 25。
- 22：Conversation、Turn、Message 持久化基础。
- 23A：Conversation 领域状态机。
- 23B：Conversation 跨表原子写入。
- 23C：创建 Conversation 与开始 Turn 的应用编排。
- 23D：Codex 事件折叠及 Turn 成功、失败、取消收口。
- 23E：只读 Codex Conversation Runtime 与 Thread 续接。
- 23F：Grilling 回复、失败/取消重试和当前 Turn 中断。

步骤 23 到 23F 结束。下一步固定为 **24A**。

## 步骤 24：原 MVP 的 Go Workbench API

### 24A：Workbench 读取模型

实现 Go 应用层聚合快照，读取当前 Requirement、历史 Requirement、Conversation 消息、Spec、
Tickets、TaskRun、Verification 与目标运行态，并转换成原页面需要的稳定 DTO。

完成标准：Go HTTP 能返回足够渲染原三栏页面的快照；浏览器不需要猜 TicketVersion ID，也不从
多个响应拼出业务真值。

### 24B：Conversation HTTP

暴露初始 Grilling、读取消息、回复、重试和取消接口；接入 23E/23F，并统一 revision、错误码和
请求 DTO。创建新 Requirement 后必须能进入第一轮真实 Grilling。

完成标准：只使用 HTTP 客户端即可走完 Grilling 的开始、回复、重试、取消和确认。

### 24C：Spec 与 Tickets 对话动作

补齐原页面已有的 Spec/Tickets 修订、失败重试和运行中取消，并复用已有的生成、版本确认与 Ticket
DAG 发布能力。正式产物必须由专用生成用例保存，不能把聊天正文直接当成版本。

完成标准：原页面两个规划阶段的所有按钮都有等价 Go 用例和 HTTP 入口。

### 24D：执行后的原 MVP 动作

补齐聚合 API 尚缺的执行状态读取、产品验收、继续返修和交付 Handoff；复用现有 TaskRun、
Verification 与 Target Runtime，不增加新的产品功能。

完成标准：从确认 Tickets 到执行、验证、产品验收、返修或交付的原页面动作均有 Go 入口。

## 步骤 25：原页面完整切流

### 25A：客户端契约与适配

在 `apps/web/src/api/` 建立与步骤 24 对应的 DTO、client、query 和 mutation；服务端状态只进入
React Query，纯 UI 状态继续留在组件中。

### 25B：替换页面数据边界

保持 `apps/web/src/routes/index.tsx` 的布局和组件树，只替换 `getWorkbench`、动作函数及其调用方式。
旧 Node Server Functions 源码继续保留，但原页面不再调用它们。

### 25C：切换 SSE

把原页面的 `/api/events` 切到 Go `/api/v1/events`，用持久化游标更新或失效聚合快照。

完成标准：停止 Node 服务后，原页面所有 MVP 操作仍可用；`index.tsx` 不再导入或调用
`src/server/functions.ts`，视觉布局和交互没有被替换。

## 步骤 26：迁移验收

- Playwright 覆盖创建需求、Grilling、Spec、Tickets、执行、验证、验收和返修主路径。
- `pnpm check` 与单二进制构建通过。
- 使用 `pnpm workbench` 启动后，只访问 Go 服务即可完成流程。
- 更新故障演示、benchmark 和面试材料；这些文档工作不改变 MVP 行为。

完成标准：上述检查全部通过，才称为“原 Node MVP 已迁移到 Go”。
