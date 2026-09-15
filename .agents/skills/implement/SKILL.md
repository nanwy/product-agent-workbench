---
name: implement
description: "Implement a piece of work based on a spec or set of tickets."
disable-model-invocation: true
---

Implement the work described by the user in the spec or tickets.

Use /tdd where possible, at pre-agreed seams.

Run typechecking regularly, single test files regularly, and the full test suite once at the end.

Once done, use /code-review to review the work.

Do not commit, push, create a pull request, merge, deploy, or publish. Leave the candidate changes in the worktree. The Product Agent Workbench owns the single stable commit after its verification matrix passes and the product user explicitly accepts the result.
