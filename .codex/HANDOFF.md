# GopherMind project handoff

Updated: 2026-09-18T09:22:31+08:00
Workspace: `C:\Users\Huangsirui\OneDrive\Desktop\GopherMind`
Repository: `nanjiek/GopherMind`
Branch: `codex/p1-step1-postgres-foundation`
P1 node 1 code commit: `eba5a534b42cd4854a9a0816ab1b26b09f564ab6`

## Objective

Upgrade GopherMind according to the saved V3 architecture plan. P0 is complete. P1 is in progress: PostgreSQL is the only relational database, followed by the Event Log/Projection/Surface loop and Redis recovery proof.

## User decisions and standing constraints

- Use a fresh PostgreSQL database. All old MySQL data is invalid; do not implement migration, dual writes, CDC, backfill, or reconciliation.
- Complete P1 without redoing P0.
- Each completed node must update this checkpoint, be committed, pushed, and submitted as a GitHub PR.
- Preserve unrelated work and exclude secrets, local `.env`, caches, and temporary output.

## Completed

- Original V3 architecture and implementation plans are in `docs/ai-architecture-v3.zh.md` and `docs/ai-upgrade-plan-v3.zh.md`.
- P0 build baseline, architecture decisions, Gold Set, smoke matrix, and P1 design are complete through PRs #4, #5, and #6.
- PR #6 is merged into `codex/p0-step2-decisions`; its merge commit is `105b44c521c1aa6b423d085e2f378e162289fa54`.
- P1 node 1 is implemented:
  - runtime repositories, configuration, both entrypoints, compose, dependencies, and docs use PostgreSQL only;
  - startup no longer runs GORM AutoMigrate;
  - `cmd/migrate` applies embedded versioned SQL to an empty schema and refuses unknown non-empty schemas;
  - schema verification checks version and required tables;
  - the initial schema includes existing business tables plus Event Log, projection, outbox, and reserved agent tables;
  - PostgreSQL integration tests cover repeatable initialization, unknown-schema rejection, incomplete-schema rejection, transactions, uniqueness, and ownership scope;
  - CI now starts PostgreSQL and runs the integration tests.
- P1 node 1 is committed and pushed in `eba5a53`; PR #7 is open: `codex/p1-step1-postgres-foundation` → `codex/p0-step2-decisions`.

## Current state

- Working tree: clean before this checkpoint metadata update.
- Current branch is based on the merged P0 state in `origin/codex/p0-step2-decisions`.
- PR #3 remains open as a draft in the older stack; PRs #4, #5, and #6 are merged.
- Docker Desktop recovered after a transient Ubuntu WSL integration failure.
- A disposable local PostgreSQL 16 container named `gophermind-p1-postgres` is running on `127.0.0.1:55432` for P1 integration tests.

## Validation

- `GOTOOLCHAIN=go1.23.12 POSTGRES_TEST_DSN=... go test ./...` — passed against real PostgreSQL 16.
- `GOTOOLCHAIN=go1.23.12 go build ./cmd/...` — passed, including `cmd/migrate`.
- `GOTOOLCHAIN=go1.23.12 go vet ./...` — passed.
- `go run ./cmd/migrate` twice against the disposable PostgreSQL database — passed; the second run reported no drift.
- `docker compose config --quiet` — passed.
- In `frontend`, `npm ci --no-audit --no-fund` and `npm run build` — passed.
- `git diff --check` — passed.

## Next actions

1. Start `codex/p1-step2-event-surface` from the node 1 branch.
2. Implement typed Event Log append/read with transactionally coupled session/message writes, stream sequence, stable idempotency replay, and ownership enforcement.
3. Add Projection/Surface types and Redis versioned cache; prove cache deletion rebuilds the same recent-message Surface from PostgreSQL.
4. Cover concurrent append, response-loss retry, replay, scope rejection, and cache recovery against real PostgreSQL/Redis; then commit, push, and create the next PR.

## Blockers and risks

- The PR history is still stacked on intermediate branches rather than integrated into `develop`.
- P1 node 1 creates Event Log tables but does not write events yet; node 2 must complete the functional loop.
- Docker Desktop's WSL integration failed once during image startup and recovered after restart; re-check it before real-dependency tests.
- LangGraph PostgreSQL recovery and Qdrant/RAG degradation remain later P1/P4 and P1/P6 work.

## Important files

- `internal/repo/postgres/migrations/000001_initial.up.sql` — authoritative initial PostgreSQL schema.
- `internal/repo/postgres/migrate.go` — migration runner and schema verifier.
- `internal/repo/postgres/*_integration_test.go` — real PostgreSQL coverage.
- `cmd/migrate/main.go` — explicit migration entrypoint.
- `docs/design/p1-event-contract.zh.md` — Event/Projection/Surface contract and P1 acceptance.
- `docs/p0-smoke-matrix.zh.md` — remaining real-dependency checks.
- `docs/adr/0001-postgresql-clean-cutover.zh.md` — no-migration database decision.
