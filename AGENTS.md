# Repository Guidelines

## Project Structure & Module Organization

This is a pnpm/Go monorepo. `apps/web/` contains the React 19 + TanStack Start UI, browser API clients in `src/api/`, UI components in `src/components/`, Node compatibility code in `src/server/`, and Vitest/Playwright tests in `tests/`. `apps/server/` contains the Go control plane: executable wiring in `cmd/workbench/`, domain and application logic under `internal/`, SQLite migrations in `migrations/`, and the embedded SPA output in `internal/webui/dist/`. Repository-wide configuration lives in `config/`; architecture decisions live in `docs/adr/`. Read `CONTEXT.md` before changing domain terminology.

## Go Migration Steps

Before implementing or describing a numbered migration step, read `docs/go-migration-roadmap.md`. It is the
single source of truth for step names, scope, order, and completion criteria. Match the requested number to that
file and current code before editing. Update the roadmap first when the user explicitly changes the plan; do not
invent intermediate step numbers from conversation memory.

## Build, Test, and Development Commands

- `pnpm install`: install workspace dependencies (Node 24, pnpm 10).
- `pnpm dev`: run the original TanStack product UI on port 3000.
- `pnpm workbench`: build the SPA and run the Go service on port 8080.
- `pnpm typecheck`: run TypeScript checks.
- `pnpm test`: run web Vitest suites.
- `pnpm test:server`: run all Go tests.
- `pnpm test:e2e`: run Playwright product-flow tests.
- `pnpm build:standalone`: build the embedded SPA and Go binary.
- `pnpm check`: run the repository’s main verification chain.

Run commands from the repository root so relative configuration paths remain valid.

## Coding Style & Naming Conventions

Use `gofmt` and idiomatic Go naming: exported identifiers use `PascalCase`, local identifiers use `camelCase`, and packages are short lowercase names. TypeScript uses two-space indentation, `camelCase` values, and `PascalCase` React components/types. Keep API DTOs in `apps/web/src/api/contracts.ts`; do not duplicate server state in component state. Chinese comments should explain learning-relevant intent and invariants, not restate syntax.

Preserve `apps/web/src/routes/index.tsx` product layout and interaction model during API migrations. Replace data boundaries (`getWorkbench`, actions, SSE) rather than substituting a new page unless the task explicitly changes UX.

## Testing Guidelines

Go tests use `*_test.go`; web unit tests use `*.test.ts`; browser tests live in `tests/e2e/*.spec.ts`. Add table-driven Go tests for state transitions and contract-focused Vitest tests for clients. Run the narrow suite while iterating, then `pnpm check` before handoff. Tests that bind localhost may require normal host permissions.

## Commit & Pull Request Guidelines

History favors imperative subjects and optional Conventional Commit scopes, for example `feat(webui): implement embedded web UI`. Keep commits focused. Pull requests should describe behavior, architecture impact, verification commands, linked issues, migration/schema effects, and screenshots for visible UI changes. Never commit `.data/`, generated build output, credentials, or machine-specific paths.
