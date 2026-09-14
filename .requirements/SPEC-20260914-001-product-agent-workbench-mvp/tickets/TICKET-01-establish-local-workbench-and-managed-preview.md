# TICKET-01 建立本地工作台并托管真实目标项目预览

**Type:** AFK  
**Status:** Partial

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

建立 TanStack Start 本地 Web 工作台，把浏览器 UI 与 Node 高权限能力分开。目标项目必须由受信任配置明确声明仓库根目录、唯一可写范围、启动命令、健康地址和预览地址；产品输入不能修改这些执行参数。

Node 服务负责启动并拥有目标项目进程组、等待健康检查、暴露运行事实，并通过仅监听 localhost 的预览网关显示真实页面。网关只移除开发期 iframe 嵌入限制，保留其他响应头、路径语义和 WebSocket 能力；停止操作只能终止工作台自己创建的进程。

## User stories covered

- US-7～US-14：显式目标配置、进程生命周期、运行事实和真实预览。
- US-64、US-65：产品文本不是命令，控制服务默认限制在本机。

## Acceptance criteria

- [ ] TanStack Start 页面与 Node 控制服务可以本地启动，浏览器没有直接文件系统、Git 或子进程能力。
- [ ] 目标配置明确包含仓库根目录、修改范围、启动命令、健康地址、预览地址、网关端口和启动超时。
- [ ] 工作台启动目标项目后展示命令、PID、健康状态和预览地址，并可显式停止。
- [ ] 启动命令不经过 shell，产品输入不能参与命令或路径拼接。
- [ ] 预览网关只允许配置的 localhost 上游，保留 URL 路径语义并支持 iframe、刷新和外部打开。
- [ ] 网关只移除 `X-Frame-Options` 与 CSP `frame-ancestors`，其他安全头和响应内容保持不变。
- [ ] 停止和进程退出清理只作用于工作台拥有的进程组，不扫描端口或终止未知进程。
- [ ] 自动化测试覆盖配置边界、预览头处理、上游锁定、健康就绪、启动失败和进程回收。

## Explicitly excluded

- 不自动探测 monorepo 应用、启动命令或预览端口。
- 不支持多个目标项目、远程运行环境或 Electron 打包。

## Audit result · 2026-09-15

主功能已实现：固定配置、进程所有权、健康检查、localhost 网关、iframe/外部打开和停止操作均有代码与部分测试。

仍缺：

- 没有 target-runtime 的启动失败、健康超时、异常退出和整个进程组回收测试。
- 没有自动化测试证明配置路径越界与非 localhost URL 会被拒绝。
- E2E 只覆盖 ready 和手动 stop，尚未覆盖失败、重试及服务退出清理。

