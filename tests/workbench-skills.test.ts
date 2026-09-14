import { access } from 'node:fs/promises'

import { describe, expect, it } from 'vitest'

import { assertWorkbenchSkillBundle, resolveWorkbenchSkillPath, workbenchSkillNames } from '../src/server/workbench-skills'

describe('workbench Skill bundle', () => {
  it('ships every workflow and directly referenced Skill with the repository', async () => {
    await expect(assertWorkbenchSkillBundle()).resolves.toBeUndefined()
    await Promise.all(workbenchSkillNames.map((name) => access(resolveWorkbenchSkillPath(name))))
  })
})
