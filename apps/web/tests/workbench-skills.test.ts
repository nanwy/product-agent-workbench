import { access, readFile } from 'node:fs/promises'

import { describe, expect, it } from 'vitest'

import { assertWorkbenchSkillBundle, resolveWorkbenchSkillPath, workbenchSkillNames } from '../src/server/workbench-skills'

describe('workbench Skill bundle', () => {
  it('ships every workflow and directly referenced Skill with the repository', async () => {
    await expect(assertWorkbenchSkillBundle()).resolves.toBeUndefined()
    await Promise.all(workbenchSkillNames.map((name) => access(resolveWorkbenchSkillPath(name))))
  })

  it('keeps planning local and leaves the stable commit to the workbench', async () => {
    const [toSpec, toTickets, implement] = await Promise.all(['to-spec', 'to-tickets', 'implement'].map((name) => readFile(resolveWorkbenchSkillPath(name as 'to-spec' | 'to-tickets' | 'implement'), 'utf8')))

    expect(toSpec).toContain('Remote issues, labels, comments, pull requests, and trackers are outside')
    expect(toTickets).toContain('Never publish tickets or relationships to an external service')
    expect(implement).toContain('Do not commit, push, create a pull request')
  })
})
