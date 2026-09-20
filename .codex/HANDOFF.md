# GopherMind project handoff

Updated: 2026-09-19T00:53:31+08:00
Workspace: `C:\Users\Huangsirui\OneDrive\Desktop\GopherMind`
Repository: `nanjiek/GopherMind`
Branch: `codex/p4-step4-static-workflow-recovery`
Tool/Skill executor implementation commit: `ff1c4b9eec112313514fc213dc9431a78bc92389`
HTTP executor implementation commit: `cb368c8e1aa2b5a8acd7c1c7d3ddfe4dd1cca97f`
MCP Gateway implementation commit: `bd8ffad1b1116e4ec7105a110e9d81de6ae6cda0`
P4 fixed workflow implementation commit: `1ebfa7fd0a6760edbd0e9dec76b2ad64a113754c`
P4 fixed workflow runner implementation commit: `38d6e0c880ac390bbc6546bdfb36f4e932b89b64`
P4 workflow checkpoint implementation commit: `bbea5dfa4b5491db55fa912f4ed2e88ae8cb3c61`
P4 static workflow recovery implementation commit: `5317b104c99846c7de75d89af2ca68b8bceafa4c`

## Objective

Upgrade GopherMind according to the V3 architecture plan. P0–P3 are complete. P4 includes the fixed graph, lifecycle, checkpoint authority, pure-node recovery, static Task DAG, Task Board CAS/fencing/dependency timeout, durable Mailbox coordination, and has a final fixed multi-Agent execution branch pending the reliable-coordination merge.

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
- P4 Step 2 is limited to the in-memory protocol that transitions a Run through the closed P4 Step 1 graph and commits its response as one `return_result`. Do not add retries, recovery, persistence, publication, Queue/Task/Mailbox semantics, QueryService/StreamService integration, or dynamic delegation.
- P4 Step 3 is limited to scope-bound PostgreSQL checkpoint state and revision CAS. Do not claim LangGraph integration, connect checkpointing to execution/recovery, or add Task DAG/Mailbox, lease, fencing token, queue, retry, dynamic delegation, or external side effects.
- P4 Step 4 resumes only pure in-process fixed graph nodes from checkpointed structured handoffs. Do not use it for side effects or add Durable Action/Task, Task DAG/Mailbox, lease/fencing, queue, retry, dynamic delegation, or LangGraph integration.
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
- P4 Step 2 fixed workflow runner is committed in `38d6e0c880ac390bbc6546bdfb36f4e932b89b64`:
  - `FixedWorkflowRunner` creates one in-memory Run, transitions through `loading_context → routing → running` via the existing Hook-wrapped Controller, then executes the closed P4 Step 1 graph.
  - A successful response becomes the sole `return_result` Action and completes the Run; no response is published externally.
  - After Run creation, lifecycle, node, or completion errors are recorded as a stable classified terminal failure before the original error is returned. Context cancellation and deadline expiry map to context and timeout failures; no retry occurs.
  - Focused tests cover success, single result action, node failure, cancellation, deadline, Hook visibility, and invalid runner/spec rejection.
- No durable Task DAG/Mailbox, dynamic delegation, connection recovery, PostgreSQL persistence, migration, queue semantics, response publication, model/Tool/Skill/HTTP/MCP execution, or schema behavior changed in P4 Step 2.
- P4 Step 3 workflow checkpoint authority is committed in `bbea5dfa4b5491db55fa912f4ed2e88ae8cb3c61`:
  - `runtime.Checkpoint` validates trusted tenant/user/patient/session scope, workflow identity, lifecycle status, revision, failure information, and bounded JSON-object state; hidden model reasoning and large tool results are excluded by contract.
  - PostgreSQL migration 2 extends the unused `agent_runs` canonical row and adds immutable `agent_run_checkpoints` history.
  - `WorkflowCheckpointStore` creates revision 1, reads only within exact trusted scope, and advances current state plus history atomically through revision CAS; stale writers are rejected.
  - Focused runtime tests cover checkpoint validation, bounds, and defensive copies. PostgreSQL migration/store integration tests are present but skipped locally because `POSTGRES_TEST_DSN` is unset.
- No LangGraph runtime claim, automatic recovery, Task DAG/Mailbox, lease, fencing token, queue semantics, dynamic delegation, model/Tool/Skill/HTTP/MCP execution, old-MySQL migration, dual write, CDC, backfill, or schema behavior outside migration 2 changed in P4 Step 3.
- P4 Step 4 static workflow recovery is committed in `5317b104c99846c7de75d89af2ca68b8bceafa4c`: `StaticWorkflowRecoveryRunner` saves every completed fixed pure node by checkpoint CAS and resumes from the stored next node; tests prove recovery at Evidence skips Intake/routing and failures persist a failed checkpoint. No external side effect, Task DAG/Mailbox, lease/fencing, queue semantics, LangGraph integration, old-MySQL migration, dual write, CDC, or backfill changed.
- P4 Step 5 durable Task/DAG minimum contract is committed in `e75a6e7556e91ef2c515f699005dbfa7cedabda2`: trusted Run-bound task identity, static dependency validation, cycle rejection, and PostgreSQL task/edge tables. `TaskDAGStore` persists or loads a whole validated graph only through exact trusted scope; roots are `ready`, dependent tasks are `blocked`.
- P4 reliable coordination is committed in `d8d32ae193501118c3ef0dfb589be5703c3560d3`: Task state CAS, deadline expiry, lease/fencing, durable Mailbox, at-least-once logical queue delivery, and restart recovery. Task success remains the only completion authority; Mailbox acknowledgement only records delivery. The implementation deliberately has no actual worker execution, dynamic delegation, external broker, connection recovery, LangGraph SDK claim, or external side effect.
- P4 final fixed multi-Agent execution is merged in PR #24 at `6badaa9`: Lead chooses only trusted static `simple` (Evidence → Response), `standard` (Intake/Triage → Evidence → Safety → Response), or `human_escalation` (Triage only) DAGs; pure fixed workers claim Task/Mailbox work, receive only dependency-allowed structured data, persist results, and resume without re-executing succeeded Tasks. No model-driven delegation, external effect, or dynamic topology is introduced. P4 is complete at this merge.
- The P5–P7 plan is revised in `docs/ai-upgrade-plan-v3.zh.md`: P5 first connects trusted routing, fixed Team execution, safe response commit/replay, Durable Action/Event-Outbox and its database/race gates; Compaction follows that live execution boundary. P6 remains authority-first memory/knowledge governance. P7 retains capacity, evaluation and release governance rather than deferring basic correctness tests.
- P5.1 route-to-Team bridge is merged in PR #25 at `3c812f9`: P3 `single-agent-query`, `clinical-review`, and `manual-escalation` Decisions map only to P4 `simple`, `standard`, and `human_escalation` paths respectively; contradictory or forged Decisions are rejected before any Team execution.
- P5.2 trusted routed-Team Query entry is merged in PR #26 at `2ff6301`: it requires trusted P3 risk/task attributes and trusted tenant/user scope, reroutes them and starts only the derived P4 path. Its result is explicitly uncommitted; a human handoff is never published as an assistant response. No HTTP caller, model, or payload can choose a Decision/path, and this node neither runs legacy Query effects nor introduces a response-commit barrier, Outbox, replay, or PostgreSQL wiring.
- P5.3 trusted deterministic Query policy is merged in PR #27 at `55f8e99`: only configured, validated phrase rules can construct P5.2 route input from authenticated tenant/user/session scope and ordinary question content. Unknown or equal-priority conflicting matches fail closed; no client body or model output can set risk, task, scope, Decision, or Team path. The node has no built-in clinical phrase set and makes no external call.
- P5.4 response commit barrier is implemented on `codex/p5-step4-response-commit-barrier`: a Team result is uncommitted until a trusted review verifier accepts its complete answer schema, and Capability is reauthorized immediately before the sole committer invocation. Human escalation, invalid output, failed review, and revoked Capability do not call the committer.

## Current state

- Working tree: clean after P5.4 response commit barrier is committed and pushed.
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
- PR #18 is merged: `codex/p4-step1-fixed-workflow-graph` -> `codex/p1-step2-event-surface` at `c53d9e25b2cc04b3886bd40a9371ee121c9ffd39`; GitHub `go` and `frontend` checks passed before merge — https://github.com/nanjiek/GopherMind/pull/18.
- PR #19 is merged: `codex/p4-step2-fixed-workflow-runner` -> `codex/p1-step2-event-surface` at `c336f3f8010effe7e6e55b70ef6800d450c0d409`; GitHub `go` and `frontend` checks passed before merge — https://github.com/nanjiek/GopherMind/pull/19.
- PR #20 is merged: `codex/p4-step3-workflow-checkpoint` -> `codex/p1-step2-event-surface` at `ac7d8709c7abe1a103e4f6ead2064537162ca3e6`; GitHub `go` and `frontend` checks passed after checkpoint test fixes — https://github.com/nanjiek/GopherMind/pull/20.
- PR #21 is merged: `codex/p4-step4-static-workflow-recovery` -> `codex/p1-step2-event-surface` at `340ab3608edf9abd402f2f8e502ef1b1dbcd4ae8` — https://github.com/nanjiek/GopherMind/pull/21.
- PR #22 is merged: `codex/p4-step5-task-dag-contract` -> `codex/p1-step2-event-surface` at `69020192e88b561e568406127db7fe1627bce100`; GitHub `go` and `frontend` checks passed — https://github.com/nanjiek/GopherMind/pull/22.
- PR #23 is merged: `codex/p4-complete-reliable-coordination` -> `codex/p1-step2-event-surface` at `9a2ba1ceb6e26eb01da97cae448d16e583d091ab`; GitHub `go` and `frontend` checks passed — https://github.com/nanjiek/GopherMind/pull/23.
- PR #24 is merged: `codex/p4-final-fixed-team-execution` -> `codex/p1-step2-event-surface` at `6badaa97f207236440530669c7e0e4d51a95c9c4`; GitHub `go` and `frontend` checks passed — https://github.com/nanjiek/GopherMind/pull/24.
- PR #25 is merged: `codex/p5-step1-route-team-bridge` -> `codex/p1-step2-event-surface` at `3c812f90e89b8943a21bb7410cf980c7ec6eb34e` — https://github.com/nanjiek/GopherMind/pull/25.
- PR #26 is merged: `codex/p5-step2-query-team-entry` -> `codex/p1-step2-event-surface` at `2ff6301090ca21c70dbd87c59315dce700d213ff`; GitHub `go` and `frontend` checks passed — https://github.com/nanjiek/GopherMind/pull/26.
- PR #27 is merged: `codex/p5-step3-trusted-query-policy` -> `codex/p1-step2-event-surface` at `55f8e99fa32756920d7008b6184cf2131e2b95f3`; GitHub `go` and `frontend` checks passed — https://github.com/nanjiek/GopherMind/pull/27.

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
- `go test ./internal/agent/runtime ./internal/agent/gateway` — passed after P4 Step 2.
- `go test ./...` — passed after P4 Step 2.
- `go build ./cmd/...` — passed after P4 Step 2.
- `go vet ./...` — passed after P4 Step 2.
- `git diff --check` — passed after P4 Step 2.
- `go test ./internal/agent/runtime ./internal/repo/postgres` — passed after P4 Step 3.
- `go test ./...` — passed after P4 Step 3.
- `go build ./cmd/...` — passed after P4 Step 3.
- `go vet ./...` — passed after P4 Step 3.
- `git diff --check` — passed after P4 Step 3.
- `go test ./internal/repo/postgres -run 'Test(MigrationsInitializeEmptySchemaAndAreRepeatable|WorkflowCheckpointStoreCreatesLoadsAndUsesCAS)$' -count=1 -v` — passed with both PostgreSQL integration tests skipped because `POSTGRES_TEST_DSN` is unset.
- `git diff --check` — passed before the lifecycle commit.
- `go test -race ./internal/agent/runtime` — not runnable in this workstation environment: Go reports `-race requires cgo; enable cgo by setting CGO_ENABLED=1`; `go env` reports `CGO_ENABLED=0` and no `gcc`, `clang`, or `cl` executable is installed. No toolchain installation was attempted because it is outside this node's scope.
- `go test ./internal/agent/runtime ./internal/repo/postgres` — passed after P4 Step 5. PostgreSQL integration tests remain skipped locally because `POSTGRES_TEST_DSN` is unset.
- `go test ./...`, `go build ./cmd/...`, `go vet ./...`, and `git diff --check` — passed after P4 reliable coordination. PostgreSQL integration tests remain skipped locally because `POSTGRES_TEST_DSN` is unset.
- `go test ./internal/core/service ./internal/agent/gateway ./internal/agent/runtime`, `go test ./...`, `go vet ./...`, and `git diff --check` — passed after P5 Step 2.
- `go build ./cmd/...` — passed after P5 Step 2. `go test -race ./...` remains blocked locally before tests start because `CGO_ENABLED=0` and no C toolchain is installed; no toolchain installation was attempted.
- `go test ./internal/core/service ./internal/agent/gateway`, `go test ./...`, `go build ./cmd/...`, `go vet ./...`, and `git diff --check` — passed after P5 Step 3. The local `-race` limitation is unchanged.
- `go test ./internal/core/service ./internal/agent/gateway`, `go test ./...`, `go build ./cmd/...`, `go vet ./...`, and `git diff --check` — passed after P5 Step 4. The local `-race` limitation is unchanged.

## Next actions

1. Review and merge P5.4 after its required GitHub checks pass.
2. Start P5.5: persist Safety proof, committed response and replay state in PostgreSQL with CAS before public HTTP wiring.
3. Add Action/Event-Outbox before attaching queue or any other external publisher.
4. Run the exact race command on a Windows runner with a supported C toolchain before treating race coverage as complete.

## Blockers and risks

- The required race test is blocked locally by the missing C toolchain; all non-race requested validation passed.
- PostgreSQL integration coverage is blocked locally by absent `POSTGRES_TEST_DSN`; the migration/store tests exist and skip rather than simulate PostgreSQL behavior.
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
- `internal/agent/runtime/fixed_workflow_runner.go` — in-memory fixed graph Run lifecycle, failure classification, and final result action.
- `docs/p4-step2-fixed-workflow-runner.zh.md` — P4 Step 2 scope, exclusions, and acceptance.
- `internal/agent/runtime/checkpoint.go` — durable checkpoint contract, validation, scope requirements, and store interface.
- `internal/repo/postgres/checkpoint_store.go` — PostgreSQL checkpoint authority, exact scope reads, and revision-CAS writes.
- `internal/repo/postgres/migrations/000002_workflow_checkpoints.up.sql` — migration 2 canonical checkpoint and immutable history schema.
- `docs/p4-step3-workflow-checkpoint.zh.md` — P4 Step 3 scope, exclusions, and acceptance.
- `internal/agent/runtime/static_workflow_recovery.go` — checkpoint-CAS execution and pure static graph resume.
- `docs/p4-step4-static-workflow-recovery.zh.md` — P4 Step 4 scope, exclusions, and acceptance.
- `internal/agent/runtime/task_dag.go` — static trusted Task/DAG identity, dependency validation, initial state derivation, and cycle rejection.
- `internal/repo/postgres/task_dag_store.go` — transactional PostgreSQL Task/DAG authority scoped by the parent Run.
- `internal/repo/postgres/migrations/000003_task_dag.up.sql` — empty-PostgreSQL Task/DAG task and edge tables.
- `docs/p4-step5-task-dag-contract.zh.md` — P4 Step 5 scope, exclusions, and acceptance.
- `internal/agent/runtime/mailbox.go` — durable Mailbox identity, payload, delivery lease and acknowledgement contracts.
- `internal/agent/runtime/coordination_recovery.go` — post-restart expired lease and dependency-timeout recovery orchestration.
- `internal/repo/postgres/mailbox_store.go` — transactional PostgreSQL at-least-once Mailbox authority.
- `internal/repo/postgres/migrations/000004_task_coordination.up.sql` — Task lease/deadline fields and Mailbox logical-queue table.
- `docs/p4-reliable-coordination.zh.md` — P4 completion semantics, exclusions, and acceptance.
- `internal/agent/runtime/fixed_team.go` — closed Lead/worker multi-Agent execution and restart-resume coordinator.
- `docs/p4-fixed-team-execution.zh.md` — final P4 fixed multi-Agent topology, input isolation, and completion semantics.
- `internal/agent/runtime/*_test.go` — focused lifecycle tests.
- `docs/ai-architecture-v3.zh.md` — original Scope and lifecycle design rationale.
- `docs/ai-upgrade-plan-v3.zh.md` — P2 boundaries and acceptance plan.
