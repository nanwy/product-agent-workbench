import { createServerFn } from '@tanstack/react-start'

import { assertCreateRequirementInput } from '../domain/workbench'
import {
  analyzeCurrentRequirement,
  confirmCurrentBoundary,
  confirmCurrentGrilling,
  confirmCurrentPlan,
  createRequirement,
  getWorkbenchSnapshot,
  startConfiguredTarget,
  stopConfiguredTarget,
} from './workbench-service'
import {
  cancelRequirementExecution,
  startRequirementExecution,
} from './workbench-execution'

export const getWorkbench = createServerFn({ method: 'GET' }).handler(() =>
  getWorkbenchSnapshot(),
)

export const submitRequirement = createServerFn({ method: 'POST' })
  .validator(assertCreateRequirementInput)
  .handler(({ data }) => createRequirement(data))

export const analyzeRequirement = createServerFn({ method: 'POST' }).handler(() =>
  analyzeCurrentRequirement(),
)

export const confirmGrilling = createServerFn({ method: 'POST' })
  .validator((answers: Record<string, string>) => answers)
  .handler(({ data }) => confirmCurrentGrilling(data))

export const confirmPlan = createServerFn({ method: 'POST' }).handler(() =>
  confirmCurrentPlan(),
)

export const confirmBoundary = createServerFn({ method: 'POST' }).handler(() =>
  confirmCurrentBoundary(),
)

export const startTarget = createServerFn({ method: 'POST' }).handler(() =>
  startConfiguredTarget(),
)

export const stopTarget = createServerFn({ method: 'POST' }).handler(() =>
  stopConfiguredTarget(),
)

export const executeRequirement = createServerFn({ method: 'POST' }).handler(async () => {
  await startRequirementExecution()
  return getWorkbenchSnapshot()
})

export const cancelExecution = createServerFn({ method: 'POST' }).handler(async () => {
  await cancelRequirementExecution()
  return getWorkbenchSnapshot()
})
