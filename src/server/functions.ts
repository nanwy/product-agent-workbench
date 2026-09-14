import { createServerFn } from '@tanstack/react-start'

import { assertCreateRequirementInput, type CreateRequirementInput } from '../domain/workbench'
import { runIdempotentMutation } from './idempotency'
import {
  analyzeCurrentRequirement,
  acceptCurrentRequirement,
  confirmCurrentBoundary,
  confirmCurrentGrilling,
  confirmCurrentPlan,
  createRequirement,
  getWorkbenchSnapshot,
  startConfiguredTarget,
  stopConfiguredTarget,
} from './workbench-service'
import { cancelRequirementExecution, startRequirementExecution } from './workbench-execution'
import { verifyCurrentRequirement } from './verification'

type MutationInput = { idempotencyKey: string }
type RequirementMutationInput = MutationInput & { requirement: CreateRequirementInput }
type GrillingMutationInput = MutationInput & { answers: Record<string, string> }

/**
 * 校验所有写请求共有的输入。
 *
 * `idempotencyKey` 由浏览器为每次点击生成，服务端用它识别重复提交。
 * 这里是浏览器与 Node 控制服务之间的第一层能力边界：页面只能表达业务意图，
 * 不能传入任意文件路径、Shell 命令或目标仓库地址。
 *
 * @throws 缺少幂等键时拒绝执行，避免无法安全去重的状态变更。
 */
function mutationInput(input: MutationInput) {
  if (!input.idempotencyKey?.trim()) throw new Error('状态变更缺少 idempotencyKey')
  return input
}

/**
 * 读取工作台当前的完整快照。
 *
 * 内部调用 `getWorkbenchSnapshot()`，并行读取持久化的 Requirement、目标项目配置，
 * 再合并当前内存中的目标进程状态。它是只读 GET，不经过幂等层，也不触发任何流程。
 *
 * @returns 页面三个面板共同使用的 `WorkbenchSnapshot`。
 */
export const getWorkbench = createServerFn({ method: 'GET' }).handler(() => getWorkbenchSnapshot())

/**
 * 建立一条新的产品需求。
 *
 * validator 先用 `assertCreateRequirementInput()` 校验标题、目标页面和期望结果；
 * handler 再通过 `runIdempotentMutation()` 去重，最终调用 `createRequirement()` 创建
 * Requirement 聚合、Grilling 问题、初始证据和目标项目快照。
 *
 * @returns 创建完成后的最新工作台快照。
 */
export const submitRequirement = createServerFn({ method: 'POST' })
  .validator((input: RequirementMutationInput) => ({ ...mutationInput(input), requirement: assertCreateRequirementInput(input.requirement) }))
  .handler(({ data }) => runIdempotentMutation('submit-requirement', data.idempotencyKey, getWorkbenchSnapshot, () => createRequirement(data.requirement)))

/**
 * 提交并确认 Grilling 阶段的全部回答。
 *
 * `confirmCurrentGrilling()` 会检查每道题都有答案，并验证“修改边界”只能等于或收窄
 * 项目配置中的授权范围；成功后激活相应工作流 Skill，需求才允许进入只读探索。
 * 重复请求由 `runIdempotentMutation()` 直接返回当前快照，不会重复追加证据。
 *
 * @returns Grilling 已确认的最新工作台快照。
 */
export const confirmGrilling = createServerFn({ method: 'POST' })
  .validator((input: GrillingMutationInput) => ({ ...mutationInput(input), answers: input.answers }))
  .handler(({ data }) => runIdempotentMutation('confirm-grilling', data.idempotencyKey, getWorkbenchSnapshot, () => confirmCurrentGrilling(data.answers)))

/**
 * 对当前需求执行只读项目探索并生成候选 Spec。
 *
 * `analyzeCurrentRequirement()` 要求 Grilling 已确认，然后委托 exploration 模块检查
 * 代码位置、AGENTS.md、Skills 和边界影响，生成 Spec、执行票与规则清单。
 * 此动作只读取目标仓库，不创建分支、不启动 Codex，也不修改目标代码。
 *
 * @returns 阶段进入 `awaiting-spec-confirmation` 后的工作台快照。
 */
export const analyzeRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('analyze-requirement', data.idempotencyKey, getWorkbenchSnapshot, analyzeCurrentRequirement),
)

/**
 * 确认候选 Spec 和执行票。
 *
 * `confirmCurrentPlan()` 根据探索结果决定下一阶段：没有未确认边界时进入
 * `implementation`；存在鉴权、数据迁移等边界影响时进入
 * `awaiting-boundary-confirmation`，此时仍不能启动 Codex。
 *
 * @returns 完成规格确认后的最新工作台快照。
 */
export const confirmPlan = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('confirm-plan', data.idempotencyKey, getWorkbenchSnapshot, confirmCurrentPlan),
)

/**
 * 确认探索阶段发现的工程边界影响。
 *
 * `confirmCurrentBoundary()` 把所有边界项标记为已确认，激活 implement Skill，
 * 并将需求推进到 `implementation`。它只确认风险，不执行代码修改。
 *
 * @returns 已开放候选实现阶段的工作台快照。
 */
export const confirmBoundary = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('confirm-boundary', data.idempotencyKey, getWorkbenchSnapshot, confirmCurrentBoundary),
)

/**
 * 启动配置中指定的目标项目开发服务和预览网关。
 *
 * `startConfiguredTarget()` 读取受信任的 target-project 配置，再由 target-runtime
 * 创建工作台拥有的子进程、轮询健康地址并启动 localhost 预览网关。
 * 用户输入不会参与命令拼接。
 *
 * @returns 包含最新 PID、健康状态和网关地址的工作台快照。
 */
export const startTarget = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('start-target', data.idempotencyKey, getWorkbenchSnapshot, startConfiguredTarget),
)

/**
 * 停止由工作台启动的目标项目及其预览网关。
 *
 * `stopConfiguredTarget()` 只回收 target-runtime 自己持有的进程组，不会根据端口
 * 猜测并终止用户手动启动的其他进程；停止事实会同步到 Requirement 证据流。
 *
 * @returns 目标运行态变为 stopped 后的工作台快照。
 */
export const stopTarget = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('stop-target', data.idempotencyKey, getWorkbenchSnapshot, stopConfiguredTarget),
)

/**
 * 为已确认需求创建或恢复安全分支，并启动后台 Codex 执行队列。
 *
 * `startRequirementExecution()` 会检查阶段和 Git 工作区，首次执行时创建需求分支，
 * 然后按票串行运行 Codex；已有 session 时使用 resume。该函数只等待后台任务可靠
 * 启动，不等待全部票完成，后续进度由 JSON 快照和 SSE 推送给页面。
 *
 * @returns 已记录分支及 running/failed 启动状态的最新工作台快照。
 */
export const executeRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('execute-requirement', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await startRequirementExecution()
    return getWorkbenchSnapshot()
  }),
)

/**
 * 取消当前正在运行的 Codex turn。
 *
 * `cancelRequirementExecution()` 同时触发 AbortSignal 并终止适配器拥有的 CLI 进程，
 * 随后持久化 cancelled 事件。它保留 Requirement、需求分支和 session ID，因而之后
 * 可以从当前需求继续执行，而不是从头重建上下文。
 *
 * @returns 取消动作完全落盘后的最新工作台快照。
 */
export const cancelExecution = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('cancel-execution', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await cancelRequirementExecution()
    return getWorkbenchSnapshot()
  }),
)

/**
 * 单独重跑当前候选实现的验证矩阵。
 *
 * `verifyCurrentRequirement()` 会重新执行配置中的验证命令，并检查 Git 修改范围、
 * 目标页面运行态和待产品确认项。它不会重放 Codex turn、重新建分支或修改代码；
 * 存在 failed 项时阶段停留在 verification，否则进入产品验收阶段。
 *
 * @returns 包含最新验证记录的工作台快照。
 */
export const rerunVerification = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('rerun-verification', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await verifyCurrentRequirement()
    return getWorkbenchSnapshot()
  }),
)

/**
 * 对已通过自动门禁的候选实现执行产品验收。
 *
 * `acceptCurrentRequirement()` 会在服务端再次检查阶段和失败项，重新核对 Git 分支与
 * 修改范围，随后创建稳定 commit 并生成 Frontend Delivery Handoff。它不会 push、
 * 合并分支或发布生产环境，生产责任仍留给前端工程师。
 *
 * @returns 阶段为 accepted、包含稳定 revision 和交付卡的工作台快照。
 */
export const acceptRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('accept-requirement', data.idempotencyKey, getWorkbenchSnapshot, acceptCurrentRequirement),
)
