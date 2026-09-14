---
id: SPEC-20260914-001-product-agent-workbench-mvp
title: 建立规格驱动的 Product Agent Workbench 最小闭环
status: ready-for-agent
created: 2026-09-14
updated: 2026-09-14
owner: 产品
labels:
  - ready-for-agent
---

# 建立规格驱动的 Product Agent Workbench 最小闭环

## Problem Statement

产品需求通常先被写成文档或原型，再由产品与前端反复解释页面、状态和交互。当前前端最终也会与 Agent 沟通并让 Agent 修改代码，因此这段前置转译造成了重复沟通：产品描述一次，前端理解一次，前端再向 Agent 描述一次。静态文档与最终代码之间还会逐渐偏离，产品只能在较晚阶段看到真实运行结果。

现有 Praxis 证明了“打开仓库、运行项目、与 Agent 对话、实时预览”可以组合成应用，但其 Electron、多项目、多会话、worktree、自动 URL 猜测和发布能力超过本项目首版所需。它也没有把 Spec、执行票、仓库规则、Skill、验证证据和产品验收作为一条对产品可见的规格驱动开发流程。

产品需要一个更小、更明确的本地工作台：产品直接与 Codex 对齐通用代码需求，Codex 在真实目标项目的规则和需求分支中形成候选实现，产品通过真实页面返修和验收，最后把代码差异与证据交给前端审查。前端保留生产合并权，但不再承担把产品意图重复翻译给 Agent 的工作。

## Solution

提供一个运行在本机的 Web 工作台。Node.js 服务负责目标项目适配、规则和 Skill 解析、需求状态、Git 分支、Codex 会话、开发服务器、预览代理、验证记录及交付生成；浏览器页面同时展示需求阶段、Codex 对话和目标页面预览。

第一版固定连接一个目标项目，但领域模型支持通用产品需求和多个串行执行票，不把实现写死为文案替换。产品提交需求后，系统先使用 `grilling` 按设计树追问（简单需求可一轮完成）并由产品确认共同理解，再进行只读探索，加载目标项目根规则、相关目录规则和命中的 Skill，形成可追溯的生效规则；随后生成 Spec 和执行票，等待产品确认。确认后系统创建需求分支，Codex 按执行票串行修改代码，并将过程、预览、验证矩阵和收益时间点呈现给产品。

所有执行票完成后，产品可以继续返修或点击验收。验收通过后系统创建一个稳定提交并生成交付卡与前端交付 Handoff，但不 push、不创建 PR、不合并主分支。需要把工作转交给另一个 Agent 会话时，使用本地 `$handoff` Skill 在系统临时目录生成会话转接文档，这与目标项目中的前端交付 Handoff 是两个不同概念。

## User Stories

1. As a 产品经理, I want to enter a product requirement in natural language, so that I can express intent without translating it into implementation instructions.
2. As a 产品经理, I want to provide a requirement title, target page and requested outcome, so that Codex has the minimum information needed to start read-only exploration.
3. As a 产品经理, I want the workbench to support general code requirements, so that the product is not limited to copy changes.
4. As a 产品经理, I want copy changes to remain a valid minimal use case, so that the first end-to-end demonstration stays small and reliable.
5. As a 产品经理, I want to see the current SDD stage, so that I know what the system is doing and what decision is expected from me.
6. As a 产品经理, I want each SDD stage to show its input, action, output and evidence, so that a completed marker is backed by observable work.
7. As a 产品经理, I want the target project configuration to be explicit, so that the workbench does not guess the wrong application or preview port in a monorepo.
8. As a 产品经理, I want the target development server to start from the workbench, so that I do not need a separate terminal workflow.
9. As a 产品经理, I want to see the actual start command, PID, health state and preview URL, so that runtime ownership is transparent.
10. As a 产品经理, I want the workbench to stop the development process it owns, so that closing or restarting the workflow does not leave orphan processes.
11. As a 产品经理, I want to view the target page beside the Codex conversation, so that I can compare intent and behavior without switching tools.
12. As a 产品经理, I want to refresh the embedded preview, so that I can force a reload when hot module replacement is insufficient.
13. As a 产品经理, I want to open the preview in a separate browser window, so that I have a fallback for pages that cannot be reliably embedded.
14. As a 产品经理, I want local preview proxying to handle development-only frame restrictions, so that protected response headers do not make the workbench appear broken.
15. As a 产品经理, I want to see which root `AGENTS.md` was loaded, so that I know the target project's general engineering rules apply.
16. As a 产品经理, I want relevant directory-level `AGENTS.md` rules to appear before files in that scope are changed, so that local engineering conventions apply at the correct boundary.
17. As a 产品经理, I want to see every Skill discovered in the target project, so that repository capabilities are visible.
18. As a 产品经理, I want discovered Skills to be distinguished from active Skills, so that the interface does not imply every Skill was injected into Codex.
19. As a 产品经理, I want to see why a Skill became active, so that user selection, repository requirements and automatic matching are distinguishable.
20. As a 产品经理, I want active rules to show their source and applicable scope, so that I can inspect why Codex must follow a constraint.
21. As a 产品经理, I want rule summaries by default and expandable source text, so that the interface remains readable without losing auditability.
22. As a 产品经理, I want workflow steps and target-project Skills shown separately, so that SDD orchestration is not confused with repository-specific engineering capabilities.
23. As a 产品经理, I want Spec to define desired behavior while `AGENTS.md` defines safe repository execution, so that product intent and engineering constraints are both preserved.
24. As a 产品经理, I want incompatible intent and engineering constraints to stop at boundary confirmation, so that Codex does not silently choose one side.
25. As a 产品经理, I want read-only exploration before implementation, so that the target route, code location, existing behavior and likely change scope are grounded in repository facts.
26. As a 产品经理, I want a generated Spec that reflects the explored repository, so that the confirmed requirement can drive implementation and acceptance.
27. As a 产品经理, I want execution tickets generated from the Spec, so that larger requirements are divided into independently demonstrable slices.
28. As a 产品经理, I want simple requirements to create one ticket, so that small work is not burdened by unnecessary ceremony.
29. As a 产品经理, I want complex requirements to support multiple tickets, so that the workflow is not structurally limited to copy changes.
30. As a 产品经理, I want only one Active Ticket at a time, so that the first version remains serial and avoids worktree or concurrent-state complexity.
31. As a 产品经理, I want to review and explicitly confirm the Spec and tickets, so that Codex cannot modify business code before intent is agreed.
32. As a 产品经理, I want high-risk changes to request boundary confirmation before implementation, so that database, authentication, shared contract, shared UI, cross-application and delivery changes are deliberate.
33. As a 产品经理, I want a dedicated requirement branch, so that Codex changes do not enter the target project's main branch directly.
34. As a 产品经理, I want the base branch, base revision and starting working-tree state recorded, so that the origin of the candidate implementation is reviewable.
35. As a 产品经理, I want the workbench to protect unrelated existing changes, so that starting a requirement never discards or silently includes another person's work.
36. As a 产品经理, I want one continuous Codex session per requirement, so that later corrections retain the agreed product and repository context.
37. As a 产品经理, I want Codex to operate with the target repository as its working directory, so that repository instructions and file boundaries are effective.
38. As a 产品经理, I want Codex messages and important execution events streamed into the page, so that I can distinguish active work, completion, failure and cancellation.
39. As a 产品经理, I want to stop a running Codex turn, so that an incorrect or unnecessary execution does not have to finish.
40. As a 产品经理, I want each completed Agent adjustment counted as a modification round, so that collaboration cost can be measured without equating it to raw message count.
41. As a 产品经理, I want to continue the same conversation when the preview is wrong, so that small refinements remain in the same requirement and branch.
42. As a 产品经理, I want the current Ticket and changed files to be visible during implementation, so that I can see which slice Codex is working on.
43. As a 产品经理, I want validation commands and their exit codes recorded, so that “verified” does not rely on an Agent's unsupported claim.
44. As a 产品经理, I want each acceptance condition classified as `verified`, `failed` or `not-run`, so that incomplete evidence is explicit.
45. As a 产品经理, I want a reason recorded for every `not-run` condition, so that the frontend can judge remaining risk.
46. As a 产品经理, I want failed validation to block acceptance, so that known-broken work cannot be marked accepted.
47. As a 产品经理, I want runtime evidence tied to the actual target route and relevant viewport, so that type checking is not mistaken for page acceptance.
48. As a 产品经理, I want to see whether new browser console errors appeared, so that visible success does not hide an obvious runtime regression.
49. As a 产品经理, I want to explicitly click product acceptance, so that acceptance is a product decision rather than an Agent inference.
50. As a 产品经理, I want the interface to state that product acceptance is not production approval, so that engineering ownership remains clear.
51. As a 前端工程师, I want one stable commit after product acceptance, so that I can review an immutable candidate rather than a moving working tree.
52. As a 前端工程师, I want a delivery card containing branch, revision, changed files, diff statistics, validation and risk, so that I can assess the candidate efficiently.
53. As a 前端工程师, I want the final Spec, tickets, implementation and Handoff to describe the same behavior, so that product intent is not lost during execution.
54. As a 前端工程师, I want unresolved and unverified items carried into the Handoff, so that product acceptance does not conceal engineering work.
55. As a 前端工程师, I want the system to avoid automatic push, PR creation and merge, so that production integration remains under engineering control.
56. As a 产品经理, I want requirement start, first-preview and acceptance timestamps captured, so that lead time can be measured.
57. As a 产品经理, I want unavailable metrics shown as uncollected instead of estimated, so that resume claims are based on evidence.
58. As a 求职者, I want the workflow history to demonstrate product intent, Agent constraints, code output and human ownership, so that I can explain the project as more than a chat interface.
59. As a 使用者, I want the current requirement restored after refreshing the workbench, so that an accidental page reload does not erase the visible SDD state.
60. As a 使用者, I want clear failure and retry states for project startup, Codex execution and validation, so that recoverable failures do not require recreating the requirement.
61. As a 使用者, I want only one active requirement in the first version, so that branch, process, preview and Agent ownership remain unambiguous.
62. As a 使用者, I want session transfer to invoke the local `$handoff` Skill, so that another Agent can continue from a compact temporary document.
63. As a 使用者, I want session transfer and frontend delivery Handoff labeled differently, so that temporary Agent context is not confused with tracked engineering delivery.
64. As a 系统维护者, I want user-entered product text to remain data rather than executable shell input, so that a product requirement cannot inject a command.
65. As a 系统维护者, I want the service to listen locally by default and redact sensitive data from visible logs, so that the workbench does not expose repository operations or credentials.

## Implementation Decisions

- Build one local TypeScript application with a React browser interface and a Node.js control service. The service owns all filesystem, Git, subprocess and Codex capabilities; the browser receives only purpose-specific APIs.
- Keep the first version single-target and single-requirement. The configured target contains a repository root, start command, health/preview URL, optional browser-facing proxy URL and future cloud-IDE fields. Product input cannot modify executable commands.
- Use a three-region workbench: requirement and SDD status, one Codex conversation, and live preview. On narrow screens, the preview may move below the workflow rather than preserving three columns.
- Model SDD as explicit persisted states: project adaptation, intent drafting, awaiting Spec confirmation, optional boundary confirmation, implementation, verification, awaiting product acceptance, accepted, failed and cancelled. Retrying resumes the failed stage rather than recreating the requirement.
- Model tickets separately with queued, active, completed and blocked states. Only one ticket may be active; all tickets must be completed before verification can finish.
- Persist the current Requirement aggregate as local JSON in the first version. It contains identity, target snapshot, Spec and ticket metadata, stage, branch facts, Codex session identity, process facts, timestamps, modification rounds, validation records and delivery data. Chat delta persistence and historical requirement browsing are not required.
- Treat the repository's tracked requirement package as the product/engineering source of truth. The workbench writes the confirmed Spec, ticket artifacts and final frontend delivery Handoff beneath one stable Spec identifier.
- Discover repository rules from the root and nested `AGENTS.md` files. Root rules are always candidates; the closest applicable nested rule becomes active for a proposed modification scope. Each resolved rule carries source, scope, summary and activation reason.
- Discover target-project Skills from repository-standard Skill manifests. Store metadata for all available Skills but load full instructions only when the product names a Skill, an active repository rule requires it, or deterministic requirement matching selects it.
- Model workflow steps separately from target-project Skills. The SDD workflow uses `grilling`, `to-spec`, `to-tickets` and `implement`; these names explain orchestration state and must never be advertised as executable Codex capabilities merely because the names match local Skills. Repository Skills constrain domain-specific execution. A text-only requirement in the initial target activates its existing product-copy workflow.
- Do not reduce Spec and repository constraints to a misleading linear precedence. Spec owns intended behavior, repository rules own safe engineering boundaries, and Skills own working methods. An irreconcilable constraint creates an explicit boundary-confirmation item.
- Use Codex CLI as the first execution provider behind an adapter that supports starting a requirement session, continuing it, streaming structured events, cancelling a turn and recording a resumable session identity. The rest of the application must not depend on Codex-specific event shapes.
- Use the target repository as Codex's working directory. Stage prompts include the confirmed Spec, active ticket, resolved rule manifest and active Skill references; they never claim that merely discovered Skills are active.
- Enforce a read-only planning stage before business-code mutation. The proposed modification scope from that stage triggers final nested-rule resolution and any required boundary confirmation.
- Create one `product/<spec-id>-<slug>` requirement branch from the recorded base revision after Spec/ticket confirmation. Do not use worktrees in the first version and do not run concurrent requirements.
- Refuse to start implementation when Git is in a merge/rebase operation or when unrelated changes cannot be safely separated. Never reset, discard or silently include pre-existing user changes.
- Spawn the configured development command as an owned process group, capture output, poll the configured health URL, expose ready/failed/stopped state, and terminate the entire owned group on stop or application shutdown.
- Do not infer the correct application from the first URL printed by a monorepo. The configured preview URL is authoritative; process output is evidence, not routing configuration.
- Provide a localhost-only preview gateway for development. It proxies the configured target and adjusts only the embedding restriction required for the workbench preview. It must not become a general remote proxy and must expose an external-browser fallback.
- Stream Agent events to the browser over Server-Sent Events. Commands that mutate workflow state use ordinary request/response APIs with idempotency protection, while reconnecting to the event stream must not repeat mutations.
- Represent each validation item with criterion, result, evidence source, evidence detail, command when relevant, exit code, start/end time and `not-run` reason. A failed item blocks product acceptance.
- Require explicit product acceptance. Acceptance freezes the requirement, creates one candidate commit, reconciles tracked Spec/tickets, generates the delivery card and frontend delivery Handoff, and records acceptance time.
- Keep production integration out of the first version. The accepted result remains on the requirement branch without push, PR creation, merge or deployment.
- Invoke the user's local `$handoff` Skill for Agent session transfer and store that document in the operating system's temporary directory. Do not commit it or substitute it for the tracked frontend delivery Handoff.
- Record requirement start, first usable preview, product acceptance and modification rounds. Display missing measurements as uncollected and calculate no improvement percentage without a comparison baseline.
- Bind the control service to loopback by default. Validate all target paths against the configured repository root, redact credentials and tokens from streamed logs, and never construct shell commands from product requirement text or Agent output.
- Reserve configuration fields for a future cloud IDE, mapped domain and remote preview endpoint, but implement no remote execution or provisioning in this version.

## Testing Decisions

- The primary seam is a browser-level acceptance harness running the real control service against a disposable Git fixture repository, a deterministic fake Codex adapter and a fixture development server. It drives the public workflow from requirement creation through rule discovery, Spec/ticket confirmation, branch creation, streamed implementation, preview, validation, product acceptance and delivery generation.
- The primary test asserts observable behavior: visible SDD stages, active versus available Skills, rule provenance, Git branch and commit results, validation states, preview availability, persisted Requirement recovery and generated delivery artifacts. It does not assert React component structure or private helper calls.
- The acceptance harness covers both a one-ticket copy-like requirement and a multi-ticket general feature while confirming that tickets execute serially.
- The disposable Git fixture begins with an unrelated change in one scenario. The system must refuse unsafe implementation without deleting, resetting or including that change.
- A high-risk proposed scope fixture must enter boundary confirmation before Codex receives a mutating implementation prompt.
- A conflicting Spec/repository-rule fixture must display the conflict and stop rather than resolving it silently.
- The preview fixture returns an embedding restriction. The gateway test verifies that the page is visible through the local preview surface while unrelated security headers and response behavior remain intact.
- Process-lifecycle coverage verifies readiness polling, timeout, cancellation, retry and whole-process-group termination. No test may kill processes it did not start.
- Codex adapter contract tests feed recorded, redacted structured event fixtures through the streaming parser and assert stable application events for messages, tool activity, file changes, completion, failure and cancellation.
- Instruction-resolution coverage uses nested fixture directories to verify root-rule inclusion, closest-directory activation, Skill discovery, deterministic activation reasons and absence of unrelated Skill contents from the active context.
- Validation-matrix coverage verifies that `failed` blocks acceptance, `not-run` requires a reason, and accepted delivery preserves all unverified items.
- Persistence coverage reloads the browser and restarts the local service to verify that the current Requirement aggregate, stage, branch facts and evidence are recovered without repeating state-changing actions.
- The first real-target smoke test uses the configured `Mysterious-Realm-v2` front application and an intentionally small product requirement. It verifies actual Codex execution, repository instructions, one repository Skill, the configured port and product acceptance. Because real Codex output and authenticated browser state are nondeterministic external boundaries, this smoke test complements rather than replaces the deterministic acceptance harness.
- Existing Praxis tests are reference material only. This new repository has no prior application test seam, so the single browser-level acceptance harness is the preferred new seam, with small contract tests only where subprocess, Git and rule-resolution boundaries cannot be made deterministic through the browser alone.

## Out of Scope

- Multiple simultaneously active requirements.
- Multiple target projects, project switching, recent-project lists or warm-server LRU management.
- Git worktrees or parallel ticket execution.
- Providers other than Codex, provider selection or model-selection UI.
- Multiple chats per requirement, background Agents or Agent delegation.
- Automatic framework, package-manager, start-command or preview-URL detection.
- Arbitrary user-defined shell commands.
- Electron packaging or native `WebContentsView` integration.
- DOM element selection, visual style editing, design-token editing, annotation pins or in-preview source mapping.
- Automatic push, GitHub connection, PR creation, merge, release or deployment.
- Cloud IDE provisioning, remote repository execution or mapped-domain management.
- Production-grade authentication, organizations, roles or multi-user collaboration.
- Durable historical requirement search, analytics dashboards or cross-requirement reporting.
- Replacing frontend engineering's code-review and production integration authority.
- Guaranteeing completion of work that requires unavailable credentials, external permissions or unsupported production operations.

## Further Notes

- The first demonstration should be deliberately small, but the implementation must exercise the same generic Requirement, Ticket, rule-resolution, Codex, validation and delivery interfaces that later requirements will use.
- The target project's existing text-change requirement package is useful as a realistic fixture because it already contains aligned Spec, ticket, runtime evidence and frontend delivery Handoff.
- “Product acceptance” means the candidate matches product intent. It does not mean production approval, merge approval or deployment completion.
- The most defensible resume value is not “AI can write code”; it is that the application makes intent, repository constraints, execution, evidence and ownership one observable workflow. Quantitative benefit claims must wait for collected baseline and workbench measurements.
- The local issue state is `ready-for-agent`. The next step is to split this Spec into tracer-bullet implementation tickets before writing application code.
