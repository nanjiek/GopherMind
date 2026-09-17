# GopherMind project handoff

Updated: 2026-09-17T22:45:17+08:00

Workspace: `C:\Users\Huangsirui\OneDrive\Desktop\GopherMind`

Repository: `nanjiek/GopherMind`

Branch: `codex/p0-step3-readiness`

Code HEAD before handoff-only commits: `d731ec854ed91141af29a0e580efcec011c784a7`

## Objective

Upgrade GopherMind according to the saved V3 architecture plan. P0 is complete in three reviewable steps. The next phase is P1: replace the runtime MySQL path with a fresh PostgreSQL database, implement Event Log/Projection/Surface foundations, and prove Redis cache recovery.

## User decisions and standing constraints

- The new architecture uses PostgreSQL as its only relational database.
- All old MySQL data is invalid and must be discarded. Do not implement data migration, dual writes, CDC, backfill, or old/new reconciliation.
- Each completed node must be saved in Git, pushed to a remote branch, and submitted as a GitHub PR (the user calls it an MR).
- Preserve existing work. Do not include `.env`, secrets, caches, temporary files, or model hidden reasoning.
- Work was requested in explicit stages. Report actual verification and do not present planned production capabilities as already implemented.

## Completed

- Saved the original V3 plan and implementation plan in `docs/ai-architecture-v3.zh.md` and `docs/ai-upgrade-plan-v3.zh.md`.
- Saved the pre-V3 workspace snapshot in commit `27adfc5` and PR #3.
- P0 step 1 restored the build baseline in commit `01a5dfb`, PR #4 (merged): Go dependency/constructor fixes, CI, frontend lockfile, cache and streaming regression tests.
- P0 step 2 froze the architecture decisions in commit `3f49114`, PR #5 (merged): PostgreSQL clean cutover, Go/LangGraph boundary, Python+Qdrant RAG primary path, and a LangGraph checkpoint reopen spike.
- P0 step 3 completed P1 readiness in commits `9519d71` and `d731ec8`, PR #6 (open and mergeable): 12-case medical Gold Set, validation test, real-dependency smoke matrix, P1 Event/Surface design, and completion report.
- Installed and validated personal Skill `$project-handoff` at `C:\Users\Huangsirui\.codex\skills\project-handoff`. Use it to resume and refresh this file.

## Current state

- Worktree was clean before adding this handoff file.
- `origin/develop` is `29c3a4395f3e88c7f9dd83b9c40c6a735058ef3f` and currently contains PR #1 only.
- PR #1 is merged into `develop`.
- PR #2 is merged into `codex/v3-upgrade-plan`.
- PR #3 is still open as a draft: `codex/pre-v3-workspace` → `codex/v3-p0-baseline`.
- PR #4 is merged into `codex/pre-v3-workspace`.
- PR #5 is merged into `codex/p0-step1-build`.
- PR #6 is open and mergeable: `codex/p0-step3-readiness` → `codex/p0-step2-decisions`.
- The PRs are stacked onto intermediate branches. Before starting P1, verify their current state and decide whether to keep stacking or create one consolidation PR from the latest P0 branch to `develop`.
- Docker CLI exists, but Docker Desktop engine was not running during P0. No real PostgreSQL, Redis, RabbitMQ, Qdrant, or model integration test was claimed.

## Validation

- `go test ./...` — passed, including `test/evaluation` Gold Set validation.
- `go build ./cmd/...` — passed for API and MCP entrypoints.
- `go vet ./...` — passed.
- In `frontend`: `npm ci --no-audit --no-fund` and `npm run build` — passed.
- `python/workflow_service/checkpoint_smoke.py` with the P0 virtual environment — checkpoint survived SQLite checkpointer close/reopen.
- Skill validation with `PYTHONUTF8=1 quick_validate.py` — passed.
- Not run: real service integration matrix in `docs/p0-smoke-matrix.zh.md`, because Docker engine and full Python RAG dependencies were unavailable.
- GitHub reported no commit status contexts for HEAD when last checked; do not assume remote CI ran.

## Next actions

1. Invoke `$project-handoff`, read this file, and verify Git/PR state before editing. Do not redo P0.
2. Check whether PR #6 has been merged. If the user wants the stack integrated, create or update a consolidation path to `develop`; do not merge without current authorization.
3. Start `codex/p1-step1-postgres-foundation` from the verified latest P0 state.
4. Implement versioned PostgreSQL SQL migrations and replace MySQL config, driver, repositories, compose service, and both entrypoint wiring. Do not add an old-data import path.
5. Add real PostgreSQL integration tests for clean initialization, unique constraints, transactions, idempotency, and ownership scope. Docker availability is a prerequisite.
6. Complete the first functional loop: append session/message Event in the same PostgreSQL transaction, read/replay it, clear Redis Surface cache, and rebuild the same recent-message Surface.
7. Run the checks in `docs/p0-smoke-matrix.zh.md`, update this handoff, commit, push, and create a P1 node PR.

## Blockers and risks

- The PR graph is stacked across intermediate branches and is not fully integrated into `develop`.
- PostgreSQL schema and repository code are designed but not implemented.
- Current runtime still imports and wires `internal/repo/mysql`; P1 must remove this runtime dependency.
- The Go `internal/vector/pinecone` package is an in-memory implementation, not a remote Pinecone client.
- The online RAG path is Python + Qdrant; failures currently can degrade to empty results or a fake embedding and must become explicit in a later node.
- LangGraph SQLite validation is only a spike. Production PostgreSQL checkpoint recovery remains unverified.

## Important files

- `docs/p0-completion.zh.md` — P0 outcome and P1 first loop.
- `docs/design/p1-event-contract.zh.md` — Event envelope, append protocol, schema inventory, Surface rules, and P1 acceptance.
- `docs/adr/0001-postgresql-clean-cutover.zh.md` — authoritative no-data-migration database decision.
- `docs/adr/0002-go-langgraph-boundary.zh.md` — runtime ownership and checkpoint boundary.
- `docs/adr/0003-rag-primary-path.zh.md` — current RAG truth and DeepSeek integration boundary.
- `docs/p0-smoke-matrix.zh.md` — remaining real-dependency checks.
- `testdata/evaluation/p0_gold.jsonl` — initial 12-case medical evaluation set.
- `internal/repo/mysql/` and `cmd/*/main.go` — current PostgreSQL replacement surface.
