# Product Agent Workbench 面试讲解手册

## 一句话定位

Product Agent Workbench 不是一个套壳聊天页面，而是本地的规格驱动开发控制平面：它把产品意图、仓库规则、执行状态、真实预览和验证证据放进同一条可审计流程。

## 为什么值得做

传统协作中，产品先把需求交给前端，前端理解后再转述给 Agent。工作台减少这次重复翻译，同时保留前端对生产合并的最终权力。系统解决的重点不是“AI 能写代码”，而是让意图、约束、执行、证据和人类责任保持一致。

## MVP 执行票形成的架构

```text
浏览器（产品可见）
  ├─ 产品需求与 SDD 阶段
  ├─ 执行证据时间线
  └─ 真实页面 iframe
             │ purpose-specific Server Functions
             ▼
TanStack Start / Node 控制服务
  ├─ Requirement 聚合与 JSON 原子持久化
  ├─ Grilling 共同理解门禁
  ├─ 固定目标项目配置
  ├─ AGENTS.md / Skill 发现与解析
  ├─ Git 需求分支与 Codex JSONL/SSE 适配
  ├─ 目标开发进程组生命周期
  └─ localhost-only 预览网关
             │
             ▼
目标项目 Mysterious-Realm-v2 / front
```

浏览器没有文件系统、Git 或子进程权限。所有高权限能力都留在 Node 服务，浏览器只能调用语义明确的接口，例如“建立产品需求”“启动目标项目”和“确认 Spec”。

## 核心模型

`Requirement` 是聚合根，保存：

- 产品需求身份、标题、目标页面和期望结果；
- 当前 SDD 阶段；
- 候选 Spec、执行票和边界确认项；
- 可用/生效规则与 Skills；
- 创建需求时的目标配置快照与最近一次进程事实；
- 每个阶段的输入、动作、输出和时间证据。

状态先写临时文件再原子 rename。页面刷新后重新读取聚合，不依赖浏览器内存恢复流程。

## 五个关键设计取舍

### 1. 显式配置，不自动猜测

目标仓库、命令、健康地址、预览地址和拟修改范围来自受控配置。产品输入永远不会变成 Shell 命令。这避免了 monorepo 端口猜错，也缩小了命令注入面。

### 2. 进程所有权，而不是“帮用户 kill 端口”

工作台把目标服务作为独立进程组启动并记录 PID，只终止自己拥有的进程组。它不会扫描端口后杀死未知进程。

### 3. 规则解析，而不是把整个仓库塞进上下文

根 `AGENTS.md` 始终生效；拟修改范围内只选择最近的目录规则。Skills 探索阶段只读取 frontmatter 元数据，只有产品点名、仓库规则要求或确定性需求类型匹配时才生效。UI 展示来源、范围和激活原因。

### 4. 连续会话，串行执行

每个 Requirement 保存规划与实现 Thread ID。默认后端使用项目固定版本的 Codex app-server 创建或恢复 Thread，多张票沿用同一个 implementation Thread 串行执行；Adapter 把上游通知归一化为稳定事件，文字 delta 合批后通过 SSE 推送给浏览器。取消只终止当前 Turn，不删除 Thread。

### 5. 分支隔离不等于工作区安全

创建分支前同时记录 base branch、base revision 和完整 working-tree 状态。MVP 遇到任何既有修改或 Git 操作都会阻止执行，绝不 stash、reset 或偷偷纳入需求。每轮 Codex 完成后再次验证分支、HEAD 与修改范围。

## 预览网关为什么使用独立 localhost 端口

如果把目标页面挂在工作台的子路径下，页面里的 `/assets/...` 等绝对路径会错误指向工作台。独立端口保留目标页面的 origin 路径语义，同时仍运行在同一个 Node 进程中。网关只删除阻止开发期 iframe 的 `X-Frame-Options` 和 CSP `frame-ancestors`，其他响应头与内容保持不变，并支持 WebSocket upgrade。

## 测试策略

主验收边界是浏览器：Playwright 启动真实工作台，工作台再启动一个一次性目标项目 fixture。测试从产品需求输入开始，覆盖刷新恢复、进程启动、网关预览、响应头保留、规则解析、Skill 激活和 Spec 确认。

小型 Vitest 契约测试只覆盖浏览器不适合精确断言的边界，例如 CSP 指令过滤和最近目录规则解析。

## 代码阅读地图

建议按下面的顺序读，而不是从组件一路跳进工具函数：

1. `src/domain/workbench.ts`：先认识 Requirement 聚合、阶段状态机和 WorkbenchSnapshot。
2. `src/routes/index.tsx`：看浏览器如何取得快照、订阅变化，并把动作交给 Server Functions。
3. `src/server/functions.ts`：看浏览器可调用的最小能力白名单，以及每个写动作的幂等边界。
4. `src/server/workbench-service.ts`：看需求创建、Grilling、Spec 确认、边界确认和产品验收如何迁移状态。
5. `src/server/exploration.ts`：看只读探索如何解析 AGENTS.md、Skills、候选代码位置和执行票。
6. `src/server/workbench-execution.ts`：看分支创建、串行票队列、Codex turn 和自动验证如何接力。
7. `src/server/codex-adapter.ts` 与 `src/server/git-workspace.ts`：看两个高风险外部边界如何被收窄。
8. `src/server/verification.ts`：看命令、Git 范围、运行时和产品判断如何汇入同一验证矩阵。
9. `src/server/target-runtime.ts` 与 `src/server/preview-gateway.ts`：看目标进程的所有权和真实预览。
10. `src/server/requirement-store.ts`、`idempotency.ts`、`agent-events.ts`：最后理解持久化、重复请求和实时通知。
11. `src/components/workbench-panels.tsx`：此时再读 UI，会发现它基本只是状态投影和事件入口。

### 一次点击如何穿过系统

以“创建分支并执行”为例：

```text
AgentConsole.onExecute
  → index.tsx/runAction（生成幂等键）
  → functions.ts/executeRequirement（浏览器能力边界）
  → workbench-execution.ts/startRequirementExecution（创建或复用后台任务）
  → git-workspace.ts（检查干净工作区并创建需求分支）
  → codex-adapter.ts（逐票执行或 resume 同一个 Codex session）
  → requirement-store.ts（持久化票、事件、改动文件与阶段）
  → agent-events.ts → /api/events（通知浏览器重新拉取快照）
  → verification.ts（所有票完成后生成验证矩阵）
```

这里有两个刻意的异步边界：按钮请求只负责可靠地启动后台队列，不等待整轮 Codex 完成；SSE 也只通知“有变化”，完整状态始终重新读取 JSON 聚合。这样断线、刷新和长任务都不会让浏览器内存成为唯一真相。

### 三类状态分别存在哪里

- 产品与执行事实：`WORKBENCH_DATA_DIR/requirement.json`，通过临时文件加原子 rename 写入。
- 幂等记录：同一数据目录下的幂等存储，用于抵抗双击和网络重试。
- 活进程句柄、后台 Promise、SSE 订阅者：只存在 Node 进程内存；页面读取的是它们投影出来的快照。

目标项目源码和 Git 分支仍存放在配置指向的目标仓库中，工作台不会把代码复制进自己的 JSON。

## 三分钟现场演示

1. 打开工作台，说明三个区域分别对应意图、执行上下文和真实结果。
2. 填写一个文案需求，强调输入是产品结果而不是实现指令。
3. 刷新页面，证明 Requirement 已持久恢复。
4. 启动目标项目，指出实际命令、PID、健康地址和 iframe 预览。
5. 开始只读探索，指出实际检查的文件数、候选代码位置，再展开根规则和 `apps/front/AGENTS.md`。
6. 对比“可用 Skill”和“生效 Skill”，解释激活原因。
7. 展示生成的候选 Spec、验收条件和执行票，然后确认。
8. 创建需求分支，观察两张票串行变化和同一个 Codex session 的事件流。
9. 展示验证矩阵：命令退出码、Git 修改范围、目标路由状态和待人工确认项。
10. 点击产品验收，展示稳定 commit 与 Frontend Delivery Handoff，并强调它不等于生产批准。

## 常见追问

**为什么不用 Electron？** 目前需要的是本地 Node 能力和浏览器 UI，不需要桌面打包与原生视图。Web 形态减少了进程间状态同步和发布复杂度。

**为什么不是让 Agent 直接改代码？** 因为产品意图、仓库规则和修改边界必须先可见并确认，执行只是中间环节。

**为什么用 JSON 而不是数据库？** MVP 只有一个活动产品需求。JSON 能验证恢复语义，又避免过早引入数据库生命周期；聚合边界已经独立，未来可以替换存储 adapter。

**TanStack Start RC 有什么风险？** Server Function 与 Nitro/Vite 集成仍可能变化。项目把领域模型、进程管理和规则解析留在普通 TypeScript 模块中，降低框架迁移成本。

**产品验收为什么还要生成 commit？** 验收前的 working tree 仍是可变候选实现；验收后的稳定 revision 才能让前端基于同一个对象做审查。工作台不 push、不建 PR、不合并。

**验证矩阵为什么保留 `not-run`？** 因为“没有证据”不等于“已验证”。自动命令、Git 边界和运行时可访问性由系统记录，最终的产品感知由产品显式点击验收补齐。
