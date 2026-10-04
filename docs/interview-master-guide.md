# Product Agent Workbench 面试作战手册

这份手册按当前代码整理。先把主线说顺，再准备追问。不要一上来报技术栈，也不要把所有功能平铺一遍。

## 先记住一个模型

这个项目是本地研发控制面，不是聊天套壳。Agent 负责生成候选内容，工作台负责决定它什么时候可以继续、可以改哪里、结果由什么证据证明。

一条需求经过四道门：

```text
意图门禁               执行门禁                 验证门禁              交付门禁
Grilling → Spec/Ticket → Git/权限/Codex → 命令/页面/产品判断 → 候选提交/Handoff
     人确认                 系统约束                  证据判断               前端决定
```

每道门都在防一种具体错误：

| 门禁 | 防的问题 | 真实依据 |
| --- | --- | --- |
| 意图门禁 | Agent 带着错误假设直接写代码 | 结构化共同理解、版本化 Spec、产品确认 |
| 执行门禁 | 改错仓库、覆盖已有工作、越出范围 | 固定配置、干净工作区、独立分支、沙箱与 Git diff |
| 验证门禁 | Agent 说“完成”就被当成真的完成 | 宿主命令、运行时健康、真实路由、浏览器证据、产品验收 |
| 交付门禁 | 产品验收被误当成生产批准 | 稳定候选提交和 Handoff，不 push、不 merge、不部署 |

面试官无论从哪里追问，都可以回到这四道门。

## 用一个需求推演全链路

假设产品说：“把 `/competition` 页的空状态文案改清楚，但不要影响后台和其他列表页。”

### 1. 创建 Requirement

产品只提供标题、目标站内路由和期望结果，不提供 shell 命令、仓库路径或实现方案。仓库、修改范围、启动命令、健康地址和验证命令来自受信任的 `config/target-project.json`。

这样做是为了切断产品输入到本地命令执行之间的通路。否则一句自然语言可能被错误解释成命令或写入范围。

### 2. Grilling 只对齐需要人决定的事

Agent 先读事实，再追问目标结果、修改边界和验收信号。它必须输出结构化 understanding，并完成 Turn；产品点击确认后，服务端才允许进入 Spec。

如果只检查回复里有没有“已确认”，Agent 的自然语言就会变成权限系统。现在自然语言负责沟通，结构化字段和状态机负责授权。

### 3. Spec 有版本，Ticket 对应可观察结果

Spec 不是覆盖写。共同理解发生变化后，旧草稿标记为 `superseded`，新版本重新生成，产品只能确认当前版本。确认后再拆 Ticket，每张票都应该能在真实页面独立观察和验证。

这解决了长会话里“需求已经变了，但 Agent 仍按旧结论实现”的问题。

### 4. 探索项目规则和代码位置

工作台读取根 `AGENTS.md` 和修改范围最近一层的 `AGENTS.md`。目标项目 Skill 先只读 frontmatter；只有产品点名、规则要求或明确类型匹配时才激活，执行时再把入口路径交给 Codex 自己读取。

它还会扫描范围内的源码，结合路由和需求关键词给出候选位置。这个结果是探索证据，不是强行指定最终文件。

### 5. 建立执行边界

执行前检查当前分支、HEAD、working tree，以及 merge、rebase、cherry-pick 等状态。只要已有修改或 Git 操作未完成，就停止，不自动 stash，也不 reset。

检查通过后，从固定 base revision 创建 `product/<spec-id>-<slug>` 分支。Codex 的工作目录和 workspace-write 根都收窄到确认范围。每个 Turn 后再用 Git 检查 tracked、untracked、分支和 HEAD；Prompt 里的“不要越界”只是提醒，真正的边界来自权限和事实检查。

### 6. 连续执行 Ticket

多张 Ticket 串行执行，同一时间只有一张 active。规划和实现可以使用不同 Thread；同一 Requirement 的实现阶段会续接原 implementation Thread。取消只中止当前 Turn，Requirement、需求分支和 Thread ID 保留，之后可以继续。

页面操作都带幂等键，避免双击或网络重试重复建分支、启动进程或提交。执行期间每五秒刷新持久化心跳；Node 重启后，如果磁盘还写着 `running`，但进程和心跳已经消失，系统把当前票恢复为可重试的 blocked，并保留已经完成的票、分支和 Thread。

### 7. 实时显示，但以服务端快照为真值

默认后端是 Codex app-server。Adapter 把 `item/agentMessage/delta` 作为文字快路，把工具、文件和 completed 等完整事件作为权威事件处理。

```text
Codex app-server
  → Adapter 归一化并给 item 加 Turn 作用域
  → 50ms / 4KB 合成 SSE batch
  → React Query 投影当前消息

同一批 delta
  → 500ms / 32KB 保存累计全文
  → Requirement 权威快照
```

SSE batch 带连续 sequence 和 revision。重复批次丢弃；发现缺口就停止猜测并 refetch 快照；旧 revision 不能覆盖新缓存。SSE 断线后用三秒轮询兜底，重连时先 refetch，再恢复实时投影。`item.completed` 到达时，用同一个 item ID 的最终全文替换累计正文，不是再追加一次。

两套节奏解决的是不同问题：SSE 快路让页面及时显示，持久化稳路让刷新、断线和服务重启后能恢复。EventSource 重连只恢复连接；当前没有事件日志和 replay，所以状态完整性仍靠权威快照。

### 8. 自动运行真实项目

工作台从受信配置执行 `pnpm --filter front dev`，不经过 shell。它先检查健康端点：如果现有服务返回项目专属标记，可以复用；如果端口被身份不明的进程占用，只报告错误，不杀进程。

自己启动的服务会记录 ChildProcess 和进程组，停止时只回收自己拥有的进程。执行 build 前会暂时停止自己拥有的 dev server，避免两个 Next.js 进程同时写 `.next`，结束后恢复预览。

预览网关使用独立 localhost 端口，同时代理 HTTP 和 WebSocket。它只移除开发期 iframe 必须移除的 `X-Frame-Options` 与 CSP `frame-ancestors`，其余响应头和业务行为保留。

当前边界要说准：项目还不会为陌生仓库自动安装依赖，也不会直接猜包管理器、启动命令和目标 URL。理想方案是先检测 lockfile、workspace、scripts 和路由，再把证据交给工程师确认，确认后才写入受信配置。

### 9. 验证和交付

Agent 退出码为 0 只代表 Turn 结束。工作台宿主按配置运行 typecheck、测试和 production build；随后检查 Git 范围、健康端点和目标路由，再用 `agent-browser` 保存固定 viewport、Accessibility Tree、语义引用、console error 和 page error。

验证结果只有三种：

- `verified`：有正向证据；
- `failed`：有反向证据；
- `not-run`：没有证据，必须写原因。

必要机器检查不是 `verified` 时，服务端不会开放产品验收。产品只判断页面语义是否符合需求，前端仍负责代码质量和生产合并。

产品验收后，工作台再次检查分支和范围，写入最终 Spec、Tickets、验证矩阵和 Frontend Delivery Handoff，只暂存允许目录并形成稳定候选提交。系统到这里停止。

## 三分钟 STAR 版本

### Situation

我在使用 AI 做前端需求时发现一个重复沟通：产品先把页面需求解释给前端，前端补齐文案、状态和边界后，又要把同一件事重新组织成 Prompt 交给 Agent。代码生成变快了，但需求对齐、项目上下文和验收并没有消失。相反，Agent 在信息不完整时很容易自信地猜。

### Task

我想把这套协作做成一个本地工作台，让产品可以直接和 Agent 对齐页面需求，但不能一句话就触发改代码。系统需要把需求变成可确认的 Spec 和 Ticket，让 Agent 只在真实仓库的确认范围内工作，并且把运行、验证和交付证据留给前端审查。产品拥有需求和页面验收权，前端保留生产合并权。

### Action

我先把流程做成显式状态机。产品创建 Requirement 后进入只读 Grilling，Agent 只问影响实现的业务决策，输出目标结果、修改边界和验收信号。产品确认后才生成版本化 Spec，再拆成能独立观察的 Ticket。服务端只认结构化协议和当前版本，不根据 Agent 的一句“完成了”推进状态。

实现前，工作台解析目标仓库的 `AGENTS.md` 和项目 Skill，检查 working tree、HEAD 与进行中的 Git 操作，从固定 revision 创建独立需求分支。Codex 的 workspace-write 只覆盖确认目录，每个 Turn 前后继续用 Git 检查越界。多张 Ticket 串行执行，取消和服务重启后保留分支、Thread 和已完成结果。

验证没有交给 Agent 自己宣布。工作台宿主运行 typecheck、测试和 build，Runtime 管理开发服务，预览网关把真实页面嵌入工作台，`agent-browser` 记录页面结构与控制台证据。每条验证明确写 `verified`、`failed` 或带原因的 `not-run`。产品验收后，系统生成包含 Spec、Tickets、风险和验证矩阵的 Handoff 与候选提交，但不 push、不 merge。

后来为了让长任务真正可观察，我把 Codex 后端换成 app-server，接入真实文字 delta。后端做 50ms/4KB 的 SSE 合批和 500ms/32KB 的快照持久化，前端统一用 React Query 管理服务端状态；重复批次去重，序号缺口和断线回到权威快照，最终再用 completed 全文校准。

### Result

我用真实的 `Mysterious-Realm-v2 / front` 跑通过一条完整需求。系统创建了独立需求分支并生成稳定候选提交，当次留下 12 项 `verified` 记录；真实页面检查保存了 68 个语义引用，没有新增 console error 或 page error。交付物能追溯产品确认的需求、Agent 实际读取的规则、代码差异、验证结果和未运行项。

我没有编造提效百分比，因为缺少改造前的同口径基线。这个项目能证明的结果是：原来散落在聊天、终端和人工习惯里的研发步骤，被收敛成了一条可恢复、可审查的流程。

## 九十秒版本

我做了一个本地 Product Agent Workbench，解决产品需求需要先解释给前端、再由前端转述给 Agent 的重复沟通。

产品先给出目标页面和预期结果，Agent 进入只读 Grilling，只补齐会影响实现的业务决策。确认后生成版本化 Spec，再拆成可独立验证的 Ticket；在这之前服务端不允许修改业务代码。

执行阶段会读取目标仓库的 `AGENTS.md` 和项目 Skill，检查干净工作区，从固定 revision 创建需求分支，并把 Codex 的写入范围限制在确认目录。每个 Turn 后再检查分支、HEAD 和 diff，发现越界立即停止。

代码完成后，工作台宿主运行 typecheck、测试和 build，管理真实开发服务，并用 `agent-browser` 检查目标路由。验证结果只能是 `verified`、`failed` 或有原因的 `not-run`。产品验收后生成候选提交和 Handoff，但不 push、不 merge，前端仍然做最终 Review。

进度通过 app-server 文字 delta、后端 SSE 合批和 React Query 实时展示；断线、重复和序号缺口都回到服务端快照恢复。我在真实项目上跑通过完整需求，当次有 12 项验证通过，留下 68 个页面语义引用，没有新增控制台错误。

## 三十秒版本

我做了一个本地 AI 研发工作台，让产品直接和 Agent 对齐页面需求，同时保留正常研发门禁。需求先经过 Grilling、版本化 Spec 和 Ticket，确认后 Codex 才能在真实仓库的独立分支与限定目录里修改代码。工作台负责项目运行、Git 越界检查、构建和真实页面证据，产品负责页面验收，前端决定是否合并。进度用 app-server、SSE 和 React Query 展示，断线后靠服务端快照恢复。

## 最难的五个点

### 1. 用确定性系统约束概率性 Agent

难点不是解析一段 JSON，而是任何授权都不能依赖 Agent 自述。解决办法是把自然语言和授权状态分开：Agent 可以讨论，服务端只根据合法结构、当前版本、用户确认和状态机迁移开放下一步。

不这样做的后果：半截 Spec 被当成正式规格；旧版本被确认；Agent 在回复里说“已完成”就触发执行或验收。

### 2. 跨生命周期恢复一致状态

Requirement、Spec、Ticket 和验证证据要跨刷新保存；Codex Turn、SSE、后台 Promise 和开发进程只存在于内存。再叠加双击、网络重试和服务热重载，很容易产生重复副作用或永远卡在 running。

解决办法是：Requirement 聚合根、串行更新、临时文件加 rename、幂等键、执行心跳，以及重启后的显式 reconcile。持久事实和活进程事实不混为一谈。

### 3. 让 Agent 安全修改真实仓库

分支隔离只解决提交历史，不保护用户未提交代码，也不阻止越界写文件。项目同时用了干净工作区门禁、固定 base revision、需求分支、可写根限制、Turn 后 diff 检查和验收前再次检查。

不这样做的后果：自动 stash 覆盖用户上下文；Agent 修改 `apps/admin`；用户手动切分支后系统仍继续提交；未跟踪文件绕过普通 diff。

### 4. 区分代码失败、环境失败和产品不接受

真实试跑中出现过代码只改一行且类型检查通过，但 Codex 沙箱不能监听端口或访问宿主代理，导致预览和字体构建失败。把这些都显示为“实现失败”，用户得不到可操作结论。

因此候选实现、宿主验证、Runtime、浏览器证据和产品判断分别执行。`not-run` 是一等状态，不允许被“整体通过”掩盖。

### 5. 流式体验和最终正确性同时成立

delta 很快，但会重、会漏，不能承担恢复；完整快照能恢复，但每个片段都持久化和全量 refetch 太重。项目用 SSE 批次做快路、Requirement 快照做稳路，再用 sequence、revision 和 completed 全文解决重复、缺口、慢响应倒退和上游最终修订。

## 高频追问与回答

### 产品与流程

#### 为什么不直接让产品用 Codex？

Codex 能执行，但不知道什么时候需求已经确认、目标仓库有什么隐含规则，也不能替产品和前端分配责任。工作台补的是控制面：状态机、权限、项目运行、验证和交付证据。

#### 为什么需要 Spec，聊天记录不够吗？

聊天按时间组织，后面的决定可能推翻前面的假设；刷新、压缩上下文或换 Thread 后也不适合作为稳定输入。Spec 把当前有效意图冻结成版本，Ticket 和验收条件都引用这个版本。

#### 为什么 Grilling 每次只问一个重要问题？

一次列十几个问题会让产品只回答容易的部分，也让 Agent 在大量未确认分支上继续展开。单问最影响实现的决策，可以尽快消除错误假设。事实问题不占用产品时间，Agent自己查仓库。

#### 为什么产品输入不能提供启动命令？

自然语言输入属于低信任数据，本地命令属于高权限能力。命令来自版本控制中的受信配置，并通过 `spawn(command, args)` 执行，不经过 shell。

#### 产品验收和前端验收有什么区别？

产品确认页面结果是否符合需求；前端判断代码结构、长期维护成本、生产风险和是否合并。工作台只产出候选提交，不把产品点击验收解释成可上线。

### 状态与恢复

#### 为什么用一个 Requirement 聚合根？

阶段、Spec、Ticket、分支、执行和验证之间有强不变量。分散存储会让每个文件拥有自己的状态机，容易出现 Ticket 完成但 Requirement 仍在 implementation。MVP 的单活动需求适合用一个聚合做原子更新。

#### JSON 会不会太简陋？

它适合本地单用户 MVP，并且已经隔离在 Store 后面。临时文件加 rename 避免半写文件，串行队列避免进程内写竞争。多人、多实例时会换数据库和带版本的并发控制，但领域模型不用推倒。

#### 幂等键和串行队列分别解决什么？

幂等键解决同一个副作用请求被双击或重试；串行队列解决不同更新同时读到旧状态后互相覆盖。一个防重复执行，一个防并发丢更新。

#### 服务重启时 running 怎么处理？

磁盘状态不能证明旧进程还活着。执行期间持续写心跳；新进程发现状态为 running、内存中没有任务，并且心跳超过宽限期，就把 active Ticket 标为 blocked，保留分支、session 和已完成票，允许继续。

### Git 与执行安全

#### 已经有需求分支，为什么还要求干净工作区？

切分支不会自动保护未提交修改。已有改动可能被带到新分支、被 Agent 误改，最后混进候选提交。MVP 选择阻止执行，避免自动 stash/reset 替用户处理代码。

#### Prompt 里写“不要越界”不够吗？

Prompt 是软约束。项目还把 Codex 工作目录和可写根收窄到确认目录，Turn 后用 Git 同时检查 tracked 和 untracked 文件，验收前再检查并只暂存允许目录。

#### 为什么检查 HEAD 没变？Agent 正常改代码不会改变 HEAD 吗？

普通 working-tree 修改不会改变 HEAD。HEAD 变化说明有外部提交、reset 或切换历史发生，原先记录的 base revision 已经不再是可靠比较基线，所以要停止自动执行。

#### 为什么不用 Git worktree？

MVP 面向一个本地目标仓库，干净工作区加需求分支已经能验证核心流程，成本更低。缺点是占用用户当前 checkout。多人并行或长任务场景下，worktree 会是下一步。

### 项目运行和预览

#### 系统会自动安装依赖吗？

当前不会。它运行受信配置中的启动和验证命令，并检查健康状态。理想版本会识别 lockfile、Node 版本和 workspace，先展示检测依据，由工程师确认后再安装普通依赖；系统级依赖仍不会静默修改。

#### 怎样识别目标 URL？

当前 URL 来自受信配置，运行时用健康检查和真实路由请求验证，不是从终端第一条输出猜。理想版本会把路由文件、脚本、服务输出和健康探测交叉验证，再让人确认候选 URL。

#### 端口被占用怎么办？

只有健康正文命中项目专属标记时才复用现有服务。无法证明身份就报错，不 kill。停止操作也只终止工作台保存了句柄的进程组。

#### 为什么要预览网关，直接 iframe 不行吗？

目标项目可能有阻止嵌入的响应头，HMR 还需要 WebSocket。网关保留独立 origin 路径语义，代理 HTTP 与 upgrade，并只移除开发期嵌入所需的两类头。

#### 网关会不会变成开放代理？

上游来自受信的 loopback 配置，不接受用户传入任意主机；请求目标被限制在该上游，protocol-relative 外部地址也会拒绝。

### Codex 与流式链路

#### `@openai/codex` 和 `@openai/codex-sdk` 分别做什么？

前者提供项目固定版本的 Codex CLI/app-server 可执行文件，后者保留旧 SDK 回退。默认路径直接驱动 app-server，只有显式设置 `WORKBENCH_CODEX_BACKEND=sdk` 才使用 SDK，不会失败后偷偷换后端。

#### 为什么从 SDK 换成 app-server？

旧 `runStreamed()` 能流式给出生命周期事件，但 assistant 正文通常在 `item.completed` 一次返回。app-server 提供 `item/agentMessage/delta`，才能忠实显示上游实际文字增量。

#### 为什么 SSE，不用 WebSocket？

这里的实时方向是服务端到浏览器，用户操作仍走普通 mutation。SSE 足够、协议简单，也有 EventSource 自动重连。状态恢复依赖快照，不依赖传输协议，所以 WebSocket 不会自动解决正确性问题。

#### 为什么后端还要合批？

模型可能产生大量小 delta。每个片段都穿过 SSE 和 React Query 会放大序列化、网络事件和状态更新。50ms 或 4KB 限制等待时间与批次大小，保持可感知的实时性。

#### 为什么已经有 SSE delta，还保存累计全文？

SSE 是瞬时通知，没有持久 replay。刷新或断线可能漏批次；累计全文进入 Requirement 后，客户端随时可以用完整快照恢复。快路负责体验，稳路负责正确性。

#### 为什么还有 `item.completed`？

累计全文是 Adapter 根据收到的 delta 拼出来的中间状态；上游可能在完成时修订正文，客户端也可能漏过 delta。completed 是上游对该 item 的最终全文，必须按同 ID 替换临时内容。

#### EventSource 会重连，为什么还轮询？

自动重连只建立新连接。当前服务端没有 SSE `id:`、持久事件日志和 `Last-Event-ID` replay，断线期间漏掉的 seq 3 不会自动补回。重连时先 refetch；长时间断线用三秒轮询拿权威快照。

#### sequence 和 revision 有什么区别？

sequence 判断同一 Turn 的流式批次是否重复或缺失；revision 比较 HTTP 快照和实时投影谁更新，防止较慢的旧请求覆盖新缓存。一个处理事件连续性，一个处理跨通道新旧关系。

#### React Query 为什么适合，Zustand 为什么没删？

Requirement 是服务端状态，需要 fetch、mutation、invalidate、refetch 和缓存收敛，React Query 是唯一服务端状态源。Zustand 依赖保留，未来仍可放弹窗、筛选和未提交草稿等纯客户端状态，但不能再保存第二份 Requirement。

#### 这算逐字流式吗？

这是 app-server 提供的真实文字 delta，但不承诺逐 token 或逐字。一次 delta 可能是一个字，也可能是一整段。项目不伪造打字机动画。

### 验证与交付

#### 为什么验证要由宿主运行？

Codex 沙箱的网络、代理和监听权限与真实开发机不同。让 Agent 自己运行并宣布结果，会把环境限制误认为代码问题。工作台宿主掌握目标项目配置和进程所有权，更适合运行确定性命令。

#### `failed` 和 `not-run` 有什么区别？

`failed` 表示检查确实运行并得到反向证据；`not-run` 表示没有得到结论，例如浏览器运行时缺失或健康端点不可达。二者都会阻止必要门禁，但修复路径不同。

#### agent-browser 和 Playwright 为什么都要？

agent-browser 适合探索真实页面并保存 Accessibility Tree、语义引用和错误；Playwright 适合把已经稳定的行为固化成可重复回归。前者用于发现和留证，后者用于长期断言。

#### 为什么构建时清理代理和 `NODE_ENV`？

真实试跑遇到过非标准 `NODE_ENV` 改变 Next.js 行为，以及沙箱无法访问宿主 localhost 代理。验证命令声明环境和代理策略；auto 模式先探测本机代理，不可达就删除，避免把宿主偶然环境带入构建。

#### Handoff 里有什么？

最终 Spec、Tickets、验证矩阵、风险、未运行检查、候选分支、commit、改动文件和 diff 统计。它和业务代码进入同一个稳定候选提交，前端可以基于同一 revision 审查。

### 架构取舍与边界

#### 为什么是本地 Web，不是 Electron？

首版需要浏览器 UI 加本地 Node 能力，不需要桌面安装包或原生视图。Web 形态减少了打包和进程间状态同步；Node 仍能控制 Git、Codex 和开发服务。

#### 为什么没有数据库？

当前是本地单用户、单活动 Requirement。JSON 已经能验证状态恢复和审计语义。Store 是独立边界，未来替换数据库时不会把领域逻辑写进 ORM。

#### 这个项目现在最大的限制是什么？

它依赖事先确认的目标项目配置，暂不自动安装依赖；进程、SSE 订阅和序号在单 Node 进程内，不支持多实例 replay；Git 分支会占用当前 checkout；浏览器语义是否符合产品意图仍需人判断。

#### 如果继续做，你先补什么？

先做有证据的项目接入：检测 lockfile、workspace、scripts、路由和运行环境，把候选配置交给工程师确认。之后再用 worktree 隔离并行任务。只有出现多实例需求时，才引入数据库、持久序号和事件 replay。

#### 你怎么衡量价值？

现在能提供的是流程证据：首次预览时间、验收时间、修改轮次、验证项和返修历史。因为没有传统流程的同口径基线，我不会声称节省了多少百分比。下一步可以对同类需求比较首次可运行结果时间、来回澄清次数和前端接手后的返工率。

## 容易说错的地方

- 不要说“产品直接上线代码”。正确说法是产品确认意图和页面结果，前端决定合并与生产发布。
- 不要说“自动识别并安装所有依赖”。当前只执行受信配置；自动接入属于理想状态。
- 不要说“SSE 保证不丢消息”。当前通过 sequence 发现缺口，再用权威快照恢复。
- 不要说“逐字输出”。粒度由 app-server 决定。
- 不要说“Agent 验证通过”。工作台宿主和真实页面留下证据，产品做语义判断。
- 不要报没有基线的提效百分比。用真实 commit、验证记录和页面证据说话。

## 打开代码时的顺序

1. `src/domain/workbench.ts`：Requirement 聚合和状态机。
2. `src/server/workbench-service.ts`：Grilling、Spec 确认、返修与验收门禁。
3. `src/server/workbench-execution.ts`：后台队列、恢复、Codex Turn 和验证接力。
4. `src/server/git-workspace.ts`：需求分支与范围检查。
5. `src/server/target-runtime.ts`、`preview-gateway.ts`：进程所有权和真实预览。
6. `src/server/verification.ts`：验证矩阵与浏览器证据。
7. `src/server/codex-app-server.ts`、`codex-adapter.ts`：上游协议和事件归一化。
8. `src/server/agent-events.ts`、`src/store/workbench-query.ts`：SSE 批次与浏览器恢复。
9. `tests/e2e/workbench.spec.ts`：用户能观察到的完整闭环。

每打开一个文件，只回答三件事：它拥有什么真值、它保护什么不变量、失败后由谁恢复。

## 练习方法

第一遍只讲四道门，不讲技术名词。第二遍给每道门补一个失败案例。第三遍让别人随机追问，再从追问跳到对应文件。能在三十秒、九十秒和三分钟之间自由缩放，才算真的掌握。
