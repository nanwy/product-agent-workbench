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

function mutationInput(input: MutationInput) {
  if (!input.idempotencyKey?.trim()) throw new Error('状态变更缺少 idempotencyKey')
  return input
}

export const getWorkbench = createServerFn({ method: 'GET' }).handler(() => getWorkbenchSnapshot())

export const submitRequirement = createServerFn({ method: 'POST' })
  .validator((input: RequirementMutationInput) => ({ ...mutationInput(input), requirement: assertCreateRequirementInput(input.requirement) }))
  .handler(({ data }) => runIdempotentMutation('submit-requirement', data.idempotencyKey, getWorkbenchSnapshot, () => createRequirement(data.requirement)))

export const confirmGrilling = createServerFn({ method: 'POST' })
  .validator((input: GrillingMutationInput) => ({ ...mutationInput(input), answers: input.answers }))
  .handler(({ data }) => runIdempotentMutation('confirm-grilling', data.idempotencyKey, getWorkbenchSnapshot, () => confirmCurrentGrilling(data.answers)))

export const analyzeRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('analyze-requirement', data.idempotencyKey, getWorkbenchSnapshot, analyzeCurrentRequirement),
)
export const confirmPlan = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('confirm-plan', data.idempotencyKey, getWorkbenchSnapshot, confirmCurrentPlan),
)
export const confirmBoundary = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('confirm-boundary', data.idempotencyKey, getWorkbenchSnapshot, confirmCurrentBoundary),
)
export const startTarget = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('start-target', data.idempotencyKey, getWorkbenchSnapshot, startConfiguredTarget),
)
export const stopTarget = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('stop-target', data.idempotencyKey, getWorkbenchSnapshot, stopConfiguredTarget),
)
export const executeRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('execute-requirement', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await startRequirementExecution()
    return getWorkbenchSnapshot()
  }),
)
export const cancelExecution = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('cancel-execution', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await cancelRequirementExecution()
    return getWorkbenchSnapshot()
  }),
)
export const rerunVerification = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('rerun-verification', data.idempotencyKey, getWorkbenchSnapshot, async () => {
    await verifyCurrentRequirement()
    return getWorkbenchSnapshot()
  }),
)
export const acceptRequirement = createServerFn({ method: 'POST' }).validator(mutationInput).handler(({ data }) =>
  runIdempotentMutation('accept-requirement', data.idempotencyKey, getWorkbenchSnapshot, acceptCurrentRequirement),
)
