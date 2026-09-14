import { createServerFn } from '@tanstack/react-start'

import { assertCreateRequirementInput } from '../domain/workbench'
import {
  analyzeCurrentRequirement,
  confirmCurrentBoundary,
  confirmCurrentPlan,
  createRequirement,
  getWorkbenchSnapshot,
  startConfiguredTarget,
  stopConfiguredTarget,
} from './workbench-service'

export const getWorkbench = createServerFn({ method: 'GET' }).handler(() =>
  getWorkbenchSnapshot(),
)

export const submitRequirement = createServerFn({ method: 'POST' })
  .validator(assertCreateRequirementInput)
  .handler(({ data }) => createRequirement(data))

export const analyzeRequirement = createServerFn({ method: 'POST' }).handler(() =>
  analyzeCurrentRequirement(),
)

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
