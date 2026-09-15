# 01 — 使用 app-server 完成可恢复的 Codex 执行

**What to build:** 将规划、Grilling、Spec、执行和返修使用的默认 Codex 后端切换为每 Turn 一个 app-server 进程，让产品可以继续既有 Thread、看到完整 Agent 结果、取消错误执行，并在启动、协议或进程失败后得到可恢复的明确状态。旧 TypeScript SDK 只作为显式选择的临时回退，不允许静默降级。

**Blocked by:** None — can start immediately

**Status:** ready-for-agent

- [ ] app-server 完成初始化、Thread 启动或恢复、Turn 启动，并通过现有 Codex Turn 接口返回完整 Agent 消息和可持久化 Thread 身份。
- [ ] 规划与实现保持现有沙箱、目标项目目录、禁用网络和全局单活动 Turn 约束。
- [ ] 已有规划或实现 Thread 可以恢复并继续下一 Turn。
- [ ] 用户取消时保留已经收到的内容，向当前 Turn 发送中断，并在限定时间后清理未退出的子进程。
- [ ] 初始化失败、非法 JSON-RPC、Turn 失败和子进程异常退出不会留下永久 running 状态。
- [ ] 项目默认使用固定的本地 Codex 运行时，同时保留受控命令覆盖和显式 SDK 回退。
- [ ] fake app-server 合同测试覆盖启动、恢复、完整消息、失败和取消，原有 Grilling、Spec 与执行行为不退化。

