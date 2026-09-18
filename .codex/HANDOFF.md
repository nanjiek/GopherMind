# GopherMind project handoff

Updated: 2026-09-18T20:48:53+08:00
Workspace: `C:\Users\Huangsirui\OneDrive\Desktop\GopherMind`
Repository: `nanjiek/GopherMind`
Branch: `codex/p4-step1-fixed-workflow-graph`
Tool/Skill executor implementation commit: `ff1c4b9eec112313514fc213dc9431a78bc92389`
HTTP executor implementation commit: `cb368c8e1aa2b5a8acd7c1c7d3ddfe4dd1cca97f`
MCP Gateway implementation commit: `bd8ffad1b1116e4ec7105a110e9d81de6ae6cda0`
P4 fixed workflow implementation commit: `1ebfa7fd0a6760edbd0e9dec76b2ad64a113754c`

## Objective

Upgrade GopherMind according to the V3 architecture plan. P0–P3 are complete. P4 Step 1 adds only the fixed in-memory Intake → risk routing → Evidence → Safety → Response graph; durable recovery and multi-agent work remain later P4 nodes.

## User decisions and standing constraints

- Use a fresh PostgreSQL database. Old MySQL data is invalid; do not implement migration, dual writes, CDC, backfill, or reconciliation.
- Complete one reviewable node at a time; each completed node updates this checkpoint, is committed, pushed, and submitted as a GitHub PR.
- P2 completed only in-process revision/CAS, failure/progress controls, Hook Pipeline, and a minimal synchronous QueryService wrapper. PostgreSQL Run persistence, Event Log writes, and streaming integration remain explicitly deferred.
- P3 Step 1 is limited to trusted risk/task routing. Do not connect HTTP, QueryService, StreamService, Provider execution, Capability policy, Skill/Tool/MCP execution, budgets, or persistence in this node.
- P3 Step 2 is limited to static Capability policy. Do not execute Tool/Skill/MCP, connect HTTP/providers, or add budgets, audit persistence, or schema changes.
- P3 Step 3 executes only registered in-process Tool/Skill handlers after immediate policy authorization. Do not start MCP/HTTP transports or add schema, budgets, audit persistence, retry, or circuit breaking.
- P3 Step 4 executes only registered static HTTP endpoints. Invocation input must not select URL, method, or headers; authorization must run immediately before `client.Do`. Do not start MCP, add credentials, dynamic endpoints, persistence, budgets, audit, retries, circuit breaking, providers, or schema changes.
- P3 Step 5 adapts only pre-connected, registered MCP peers with fixed remote tool names. Authorization must run immediately before both `Ping` and `CallTool`. Do not create connections, enumerate remote tools, inject credentials, add persistence, budgets, audit, retries, circuit breaking, async Task/Mailbox, providers, or schema changes.
- P4 Step 1 is limited to the fixed Intake → risk routing → Evidence → Safety → Response in-process graph. Do not implement durable Task DAG/Mailbox, dynamic multi-agent delegation, connection recovery, PostgreSQL state persistence, or queue semantics.
- Any future P4 node with an external side effect must use the existing executor boundary and re-authorize Capability immediately before that effect.
- Preserve unrelated work and exclude secrets, local `.env`, caches, and temporary output.

## Completed

- P0 is complete through merged PRs #4, #5, and #6.
- P1 PostgreSQL foundation is in `eba5a53`; PR #7 merged at `f00fc3316bda0677935b08741d6e71f55be896fb`.
- P1 Event/Projection/Surface work is in `fbad478`; PR #8 merged at `33febb840c7393a1e6110adcb9717db64905fef6`.
- P2 Step 1 lifecycle code is committed in `d34218dfed190d8e244e66a4202495e45e838a94`:
  - `Scope` installs trusted request metadata (`tenant`, `user`, `patient`, `session`, `run`, `request`, and trace IDs) in a derived context and propagates parent cancellation.
  - `Scope` supports idempotent, LIFO resource cleanup; `Effect` handles close/setup races; `Initialize` rolls back registered resources after an initialization error.
  - `TaskGroup` bounds active task concurrency, propagates cancellation, and cancels remaining/queued work after the first task error.
  - Focused tests cover metadata/context propagation, LIFO cleanup, initialization rollback, repeated close, first-error cancellation, and concurrency limits.
- No database schema, migration, query-path, stream-path, or external-service behavior changed in this node.
- PR #9 merged into `codex/p1-step2-event-surface` at `afdb90c340ed69cc4f53cc80bfa5918ca52b1a4b`; its GitHub `go` and `frontend` checks passed before merge.
- P2 Step 2 component startup is committed in `7a1da37506d88a4f6245b9171ad2177d686b1c96`:
  - `ComponentSpec` declares a component name, required capabilities, provided capabilities, and its scoped startup function.
  - `StartComponents` rejects invalid/duplicate contracts, resolves dependencies in deterministic order, and never starts a graph with unavailable dependencies.
  - Component startup is one Scope lifecycle transaction: a startup error rolls back resources registered by the failing and already-started components in LIFO order.
  - Tests cover dependency ordering, unavailable dependencies, duplicate providers, and startup rollback.
- No database schema, migration, query-path, stream-path, state machine, or external-service behavior changed in P2 Step 2.
- PR #10 merged into `codex/p1-step2-event-surface` at `55baae147517ad0a30b1d71faf59d91a586db02d`; its GitHub `go` and `frontend` checks passed before merge.
- PR #11 merged into `codex/p1-step2-event-surface` at `60444175ec45f6331b351b6c001e0a0c618de83b`; it delivers the in-memory Run/Action/Observation state machine.
- P2 Step 3 Run/Action/Observation state machine is committed in `0e90d678a0e1973ae62a4665e645869cd2545a0e`:
  - Run states cover `created`, context loading, routing, running, all four wait states, validating, completion, retry, failure, cancellation, and expiry.
  - Structured actions are restricted to the six V3 action types and deterministically enter their matching wait/validation state; an observation for the pending action resumes the run.
  - Terminal states reject further transitions, actions, and observations; maximum step count and JSON payload shape are validated.
  - State and snapshots are mutex-protected and snapshot JSON buffers are copied; this node has no revision/CAS or persistence contract.
  - Tests cover success, all action wait-state mappings, observation resume, invalid transitions/observations, maximum steps, and terminal failure behavior.
- No database schema, migration, query-path, stream-path, Hook Pipeline, revision/CAS, or external-service behavior changed in P2 Step 3.
- P2 Step 4 completion is committed in `6982eeb7bf25790a6280a58069865b4136fddcd2`:
  - Run snapshots carry monotonically increasing revisions; CAS variants prevent stale transitions, actions, observations, completion, and current-node changes.
  - Run validates deadlines, maximum steps, structured failure classes, and repeated action fingerprints; configured no-progress limits terminate with a stable `no_progress` failure.
  - `Pipeline` provides ordered Before/reverse After hooks, rejection, error propagation, and Scope-bound automatic unregistration. `Controller` applies the pipeline around Run mutations.
  - The existing synchronous `QueryService` now executes its existing model call and final answer through the minimal in-memory single-agent Runtime protocol, without changing its external API or persisting Run records.
- No PostgreSQL Run schema, revision persistence, Event Log writes, StreamService integration, or new external dependency is introduced by P2 Step 4.
- PR #12 merged into `codex/p1-step2-event-surface` at `73f71b0e06bdec73a15c385b9a1e0784ffe06c5f`; GitHub `go` and `frontend` checks passed before merge. P2 is complete at this commit.
- P3 Step 1 Gateway routing is committed in `508ded0659b9f53bb11abebd175a0c5465f080bb`:
  - trusted L0–L3 risk and bounded task types route deterministically to versioned workflows and configured logical model aliases;
  - L3/red-flag requests select `manual-escalation` and no model route;
  - invalid requests and model-route configuration are rejected;
  - tests cover low/high/red-flag routes, version defaults, and invalid input.
- No external behavior, model call, schema, capability, or execution path changed in P3 Step 1.
- PR #13 merged into `codex/p1-step2-event-surface` at `ec01c14f33efeb1d684fdfdec6904a0a9167cdd4`; GitHub `go` and `frontend` checks passed before merge.
- P3 Step 2 Capability policy is committed in `8fc1c181ed6db2e29f6fb2e20161581e2312cd0a`:
  - immutable explicit Agent/Capability grants default-deny unregistered access;
  - patient-bound grants require trusted tenant, user, and patient scope;
  - policy tests cover explicit allow, default deny, patient scope, and duplicate grant rejection.
- No Tool/Skill/MCP execution, HTTP/provider integration, budget, audit, persistence, or schema behavior changed in P3 Step 2.
- PR #14 merged into `codex/p1-step2-event-surface` at `41db04ccd8d6ea16c49529f81fc92abbb80f5963`; GitHub `go` and `frontend` checks passed before merge.
- P3 Step 3 Tool/Skill executor is committed in `ff1c4b9eec112313514fc213dc9431a78bc92389`:
  - registered Tool/Skill manifests declare an exact Capability and output cap;
  - invocations validate structured input, then authorize immediately before the handler runs;
  - output must remain within its declared cap and be valid JSON;
  - tests prove denial prevents handler execution, patient-bound authorization, invalid input, and oversized output rejection.
- No MCP/HTTP transport, budget, audit persistence, retry, circuit breaking, provider integration, or schema behavior changed in P3 Step 3.
- P3 Step 4 constrained HTTP executor is committed in `cb368c8e1aa2b5a8acd7c1c7d3ddfe4dd1cca97f`:
  - registered manifests fix an HTTP/HTTPS URL, GET/POST method, capability, request/response size limits, and timeout;
  - invocation input can supply only a JSON body; it cannot select a target, method, or headers;
  - each request completes validation and endpoint lookup, then calls Capability policy authorization immediately before `client.Do`;
  - redirects are disabled, parent cancellation propagates, and non-2xx, oversized, and non-JSON responses are rejected;
  - tests cover successful registered calls, denial preventing a request, request/response bounds, JSON validation, redirect rejection, and parent cancellation.
- No MCP transport, dynamic destination, credentials, budgets, audit persistence, retry, circuit breaking, provider integration, or schema behavior changed in P3 Step 4.
- P3 Step 5 constrained MCP Gateway is committed in `bd8ffad1b1116e4ec7105a110e9d81de6ae6cda0`:
  - registered manifests fix a pre-connected MCP peer, remote tool, Capability, timeout, and input/output bounds;
  - `Call` accepts only bounded JSON-object arguments and never discovers peers or tool names dynamically;
  - both `CallTool` and `Ping` re-authorize the exact Capability immediately before the MCP SDK call;
  - parent cancellation propagates; invalid, oversized, and absent results are rejected;
  - tests cover static tool adaptation, denial preventing CallTool/Ping, patient scope, bounds, invalid arguments, and parent cancellation.
- No MCP connection creation, dynamic discovery, credential injection, persistence, budgets, audit, retry, circuit breaking, async Task/Mailbox, provider integration, or schema behavior changed in P3 Step 5.
- P4 Step 1 fixed workflow graph is committed in `1ebfa7fd0a6760edbd0e9dec76b2ad64a113754c`:
  - `runtime.FixedWorkflow` requires five distinct typed nodes and always executes them in the closed order Intake → risk routing → Evidence → Safety → Response.
  - Each node handoff is structured JSON-object data copied at the boundary; routing has a distinct trusted input/output contract and records an explicit versioned workflow decision.
  - Execution writes the current fixed node into the existing in-memory `Run`, fails fast at the first node error, and does not transition lifecycle state, publish output, or produce an external side effect.
  - `gateway.FixedWorkflowRiskRoutingNode` adapts the existing deterministic `gateway.Router` without a runtime-to-gateway import cycle; it neither calls a model nor creates a connection.
  - Focused tests cover fixed ordering/data handoff, short-circuiting, invalid contracts/data, defensive copies, and Gateway route adaptation.
- No durable Task DAG/Mailbox, dynamic delegation, connection recovery, PostgreSQL persistence, migration, queue semantics, model/Tool/Skill/HTTP/MCP execution, or schema behavior changed in P4 Step 1.

## Current state

- Working tree: clean after the P4 Step 1 implementation commit; this checkpoint update is the only pending tracked change until committed.
- PR chain: #7 and #8 are merged (verified through GitHub API on 2026-09-18). PR #3 remains an older draft.
- PR #9 is merged: `codex/p2-step1-runtime-lifecycle` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/9.
- PR #10 is merged: `codex/p2-step2-component-startup` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/10.
- PR #11 is merged: `codex/p2-step3-run-state-machine` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/11.
- PR #12 is merged: `codex/p2-step4-runtime-completion` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/12.
- PR #13 is merged: `codex/p3-step1-gateway-routing` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/13.
- PR #14 is merged: `codex/p3-step2-capability-policy` -> `codex/p1-step2-event-surface` — https://github.com/nanjiek/GopherMind/pull/14.
- PR #15 is merged: `codex/p3-step3-tool-skill-executor` -> `codex/p1-step2-event-surface` at `64107b467b8351e381baeaf696af263398a53ab7`; GitHub `go` and `frontend` checks passed before merge — https://github.com/nanjiek/GopherMind/pull/15.
- PR #16 is merged: `codex/p3-step4-http-executor` -> `codex/p1-step2-event-surface` at `5dd3841a80269a236df4667657db674bc6cc6fb0`; GitHub `go` and `frontend` checks passed before merge — https://github.com/nanjiek/GopherMind/pull/16.
- PR #17 is merged: `codex/p3-step5-mcp-gateway` -> `codex/p1-step2-event-surface` at `2eb8b49ef60907d45d8e1fba254fd69e74627758`; GitHub `go` and `frontend` checks passed before merge — https://github.com/nanjiek/GopherMind/pull/17. P3 is complete.
- P4 Step 1 branch: `codex/p4-step1-fixed-workflow-graph` -> `codex/p1-step2-event-surface`; PR creation is the next authorized action.

## Validation

- `go test ./internal/agent/runtime -count=20` — passed.
- `go test ./...` — passed.
- `go build ./cmd/...` — passed.
- `go vet ./...` — passed.
- `go test ./internal/agent/runtime -count=20` — passed after P2 Step 2.
- `go test ./internal/agent/runtime -count=20` — passed after P2 Step 3.
- `go test ./internal/agent/runtime -count=20` — passed after P2 Step 4.
- `go test ./...` — passed after P2 Step 4.
- `go build ./cmd/...` — passed after P2 Step 4.
- `go vet ./...` — passed after P2 Step 4.
- `go test ./internal/agent/gateway` — passed after P3 Step 1.
- `go test ./...` — passed after P3 Step 1.
- `go build ./cmd/...` — passed after P3 Step 1.
- `go vet ./...` — passed after P3 Step 1.
- `go test ./internal/agent/gateway` — passed after P3 Step 2.
- `go test ./...` — passed after P3 Step 2.
- `go build ./cmd/...` — passed after P3 Step 2.
- `go vet ./...` — passed after P3 Step 2.
- `go test ./internal/agent/gateway -count=20` — passed after P3 Step 3.
- `go test ./...` — passed after P3 Step 3.
- `go build ./cmd/...` — passed after P3 Step 3.
- `go vet ./...` — passed after P3 Step 3.
- `go test ./internal/agent/gateway -count=20` — passed after P3 Step 4.
- `go test ./...` — passed after P3 Step 4.
- `go build ./cmd/...` — passed after P3 Step 4.
- `go vet ./...` — passed after P3 Step 4.
- `go test ./internal/agent/gateway -count=20` — passed after P3 Step 5.
- `go test ./...` — passed after P3 Step 5.
- `go build ./cmd/...` — passed after P3 Step 5.
- `go vet ./...` — passed after P3 Step 5.
- `go test ./internal/agent/runtime ./internal/agent/gateway` — passed after P4 Step 1.
- `go test ./...` — passed after P4 Step 1.
- `go build ./cmd/...` — passed after P4 Step 1.
- `go vet ./...` — passed after P4 Step 1.
- `git diff --check` — passed after P4 Step 1.
- `git diff --check` — passed before the lifecycle commit.
- `go test -race ./internal/agent/runtime` — not runnable in this workstation environment: Go reports `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; `go env` reports `CGO_ENABLED=0` and no `gcc`, `clang`, or `cl` executable is installed. No toolchain installation was attempted because it is outside this node's scope.

## Next actions

1. Push `codex/p4-step1-fixed-workflow-graph` and create its PR with base `codex/p1-step2-event-surface`; then record the PR URL and state in this checkpoint.
2. Select P4 Step 2 as a separate reviewable contract. Durable Task DAG/Mailbox, dynamic multi-agent delegation, connection recovery, PostgreSQL state persistence, and queue semantics all remain deferred.
3. Run the exact race command on a Windows runner with a supported C toolchain before treating race coverage as complete.

## Blockers and risks

- The required race test is blocked locally by the missing C toolchain; all non-race requested validation passed.
- P1 remains merged through intermediate stack branches rather than consolidated into `develop`.

## Important files

- `internal/agent/runtime/scope.go` — scope metadata/context lifecycle, disposer stack, and initialization rollback.
- `internal/agent/runtime/group.go` — bounded cancellation-aware task group.
- `internal/agent/runtime/component.go` — ComponentSpec validation, dependency resolution, and transactional startup.
- `internal/agent/runtime/run.go` — in-memory Run/Action/Observation state machine and protocol validation.
- `internal/agent/runtime/hook.go` — Scope-bound ordered Hook Pipeline.
- `internal/agent/runtime/controller.go` — Hook-wrapped in-process Run mutation boundary.
- `internal/core/service/query_service.go` — existing synchronous QA path's minimal Runtime wrapper.
- `internal/agent/gateway/router.go` — trusted Gateway model/workflow routing contract.
- `docs/p3-step1-gateway-routing.zh.md` — P3 Step 1 scope and acceptance.
- `internal/agent/gateway/capability.go` — static default-deny Capability policy.
- `docs/p3-step2-capability-policy.zh.md` — P3 Step 2 scope and acceptance.
- `internal/agent/gateway/executor.go` — immediate-authorization Tool/Skill execution boundary.
- `docs/p3-step3-tool-skill-executor.zh.md` — P3 Step 3 scope and acceptance.
- `internal/agent/gateway/http_executor.go` — static endpoint HTTP execution boundary with immediate re-authorization.
- `docs/p3-step4-http-executor.zh.md` — P3 Step 4 scope and acceptance.
- `internal/agent/gateway/mcp_gateway.go` — static MCP peer/tool adaptation and authorized health boundary.
- `docs/p3-step5-mcp-gateway.zh.md` — P3 Step 5 scope and acceptance.
- `internal/agent/runtime/fixed_workflow.go` — closed P4 Step 1 node graph, structured handoffs, and in-memory Run current-node recording.
- `internal/agent/gateway/fixed_workflow.go` — one-way adapter from the fixed runtime routing contract to the existing Gateway Router.
- `docs/p4-step1-fixed-workflow-graph.zh.md` — P4 Step 1 scope, exclusions, and acceptance.
- `internal/agent/runtime/*_test.go` — focused lifecycle tests.
- `docs/ai-architecture-v3.zh.md` — original Scope and lifecycle design rationale.
- `docs/ai-upgrade-plan-v3.zh.md` — P2 boundaries and acceptance plan.
