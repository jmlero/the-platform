# The Platform Build TODO

This is the prioritized implementation backlog for the first laptop-off vertical slice. It records build order, not a permanent architecture. Product direction remains in the [AppDeck vision](VISION_DECK.md) and [Marina vision](VISION.md); cross-product boundaries, protocols, and failure semantics are defined in [ARCHITECTURE.md](ARCHITECTURE.md).

Work is tagged by its primary product module or ownership boundary:

- **[AppDeck]** — the human-facing work-control product.
- **[Marina]** — the autonomous factory engine.
- **[CLI]** — the standalone operator command-line client for AppDeck's public API.
- **[TUI]** — the later standalone terminal interface, sequenced after the CLI.
- **[Joint]** — contract, deployment, or end-to-end evidence that must exercise both real products.

A joint item is not complete when only mocks pass. It requires compatible contracts, deterministic tests in each product, and at least one paired test using real AppDeck and Marina behavior over their public protocol. Recovery behavior and truthful operator-visible state are part of the acceptance criteria, not follow-up polish.

The Milestone 1 safety gate is complete. Marina may automatically claim control-only work through its durable handoff, but no runtime or source-control mutation is enabled.

## Now — establish the executable foundation

- [x] Define the product vision and dark-factory meaning.
- [x] Add repository-wide agent working instructions.
- [x] Record the initial Go core decision and its reversal criteria.
- [x] Create a buildable Marina service with truthful liveness and readiness.
- [x] Define the first narrow work-control port.
- [x] Implement and test AppDeck health, agent registration, ready-work discovery, and atomic claim calls.
- [x] Add local build, test, formatting, and vet commands.
- [x] Document what the first executable increment does and does not do.
- [x] Publish the joint AppDeck–Marina target architecture and source-of-truth boundaries.
- [x] Adopt a product-boundary monorepo with separate modules, deployables, and one real-process conformance path.

## Milestone 1 — lossless work-control handoff and durable remote worker

### Durable execution foundation

- [x] [Marina] Define internal execution, checkpoint, lease, budget, and evidence records.
- [x] [Marina] Select SQLite as the initial durable journal after comparing it with PostgreSQL and custom files.
- [x] [Marina] Persist guarded transitions, idempotent checkpoints, source-bound evidence, and append-only events.
- [x] [Marina] Reconstruct deterministic recovery intents after a journal restart without external side effects.
- [x] [Marina] Verify exact AppDeck task, run, and agent ownership before confirming or renewing a recovered lease.
- [x] [Marina] Continuously renew only journaled, still-owned AppDeck runs while their execution budget remains valid.
- [x] [Marina] Converge provider failed, blocked, and cancelled states without bypassing completion evidence.
- [x] [Marina] Block the exact provider run and record a classified failure when an execution budget expires.
- [x] [Marina] Implement restart-safe reconciliation of AppDeck tasks already assigned to Marina.
- [x] [Marina] Renew active leases and recover safely after missed heartbeats.
- [x] [Joint] Prove across the journal, planner, AppDeck adapter, and controller that restart recovery renews the original run without issuing a claim.

### Work-control protocol safety

- [x] [Joint] Specify the first compatible work-control protocol version in AppDeck's OpenAPI document, including stable error codes, correlation fields, pagination, time representation, and additive-version rules.
- [x] [AppDeck] Add a provider-information endpoint exposing protocol version, capability identifiers, server time, and readiness.
- [x] [Marina] Validate required provider capabilities at startup and report missing capabilities precisely without disabling unrelated read-only behavior.
- [x] [Joint] Define shared JSON fixtures for tasks, runs, leases, plans, commands, events, budgets, evidence, and every classified error response.
- [x] [Marina] Split the implemented work-control boundary into focused provider-information, registry, discovery, acquisition, planning, run-control, and event capabilities; add command and evidence ports only when those operations exist.
- [x] [Joint] Add a reusable Marina work-control conformance suite and run it against a real AppDeck binary in CI.
  - [x] The reusable suite, isolated real-AppDeck runner, and monorepo CI job are implemented.
  - [x] The first published monorepo run passed on `main`: [GitHub Actions run 29690709563](https://github.com/jmlero/the-platform/actions/runs/29690709563).
- [x] [Joint] Declare and test the supported AppDeck protocol range for every Marina release.
- [x] [Joint] Select and record the first Marina-to-AppDeck service-authentication mechanism and project-scoped authorization model in [decision 0003](docs/decisions/0003-service-authentication.md).
- [x] [AppDeck] Authenticate Marina as a stable service identity and authorize every service API operation by project and capability before enabling automatic claims.
- [x] [Marina] Load the AppDeck credential through a file secret reference, rotate it without losing run identity, and never persist or log its value.

### Idempotent acquisition and fenced authority

- [x] [AppDeck] Persist idempotency keys and original results for claims and all run mutations; reject reuse with a different payload.
- [x] [AppDeck] Add guarded resource versions and a monotonically changing lease fence token to runs.
- [x] [AppDeck] Require the current run version, fence token, logical agent identity, and idempotency key for plans, progress, transitions, commands, evidence, and terminal updates.
- [x] [AppDeck] Store the claimed task version and acceptance-criteria digest on each run so later task edits cannot silently change active work.
- [x] [AppDeck] Add claim lookup by idempotency key and paginated discovery of active runs assigned to a stable logical agent and project.
- [x] [AppDeck] Make registration idempotent for a stable Marina installation identity and distinguish controller presence from ephemeral runtime attempts.
- [x] [Marina] Add versioned project bindings for AppDeck project ID, repository, runtime, policy, context, artifact, delivery, budget, and concurrency configuration.
- [x] [Marina] Persist a stable installation and logical-agent identity outside ephemeral process state.
- [x] [Marina] Add durable acquisition-intent records containing task version and claim key before making the external claim call.
- [x] [Marina] Implement a bounded work-intake controller that applies project concurrency and policy, claims idempotently, and binds the exact returned run before any runtime starts.
- [x] [Marina] Reconcile unresolved acquisition intents through claim lookup or assigned-run discovery; never recover by issuing a new blind claim.
- [x] [Joint] Inject crashes before the claim, after AppDeck commits, after a lost response, and before Marina binds the run; prove that exactly one provider run and one Marina execution exist.
- [x] [Joint] Race claim recovery against AppDeck's lease reaper and prove stale workers cannot regain authority with an older fence.

### Durable commands and projection

- [x] [AppDeck] Introduce a durable operator-command resource with monotonic sequence and pending, acknowledged, rejected, and completed states.
- [x] [AppDeck] Support at least pause, resume, cancel, replan, retry, and reduce-authority commands without directly editing active lifecycle columns from the UI.
- [x] [Marina] Persist a per-run command cursor, consume commands in order, and acknowledge each command idempotently.
- [x] [Marina] Make cancellation and authority reduction pre-empt ordinary progress and stop new side effects before lease expiry.
- [x] [Marina] Add a durable provider outbox for normalized progress, failure, usage, evidence, command acknowledgements, and terminal projections.
- [x] [AppDeck] Deduplicate projected events by producer event ID while preserving a monotonic per-execution sequence and correlation chain.
- [x] [Joint] Prove that an AppDeck outage accumulates a bounded Marina outbox and replay produces no duplicate timeline or lifecycle transitions.
- [x] [Joint] Prove that an operator cancellation during a simulated active execution prevents further controller side effects, invalidates stale authority, and converges both ledgers.

### Complete controller failure behavior

- [x] [Marina] Add bounded retry, timeout, cancellation, and failure classification to every external operation.
- [x] [Marina] Publish normalized progress and classified failure events to AppDeck through the outbox.
- [x] [Marina] Define the lease safety window in which no new mutating step may start if AppDeck cannot renew authority.
- [x] [Marina] Reconcile planning, waiting-decision, running, and verifying actions only after inspecting their exact external resource.
- [x] [Marina] Persist retry attempt, next eligible time, last classified error, and exhaustion reason rather than hiding backoff state in memory.
- [x] [Marina] Add controller-level concurrency guards and optimistic conflicts so overlapping cycles cannot perform the same side effect.
- [x] [Joint] Verify budget exhaustion, provider cancellation, provider failure, stale fence, and Marina restart against the real AppDeck API.

### Milestone 1 exit gate

- [x] One ready AppDeck task is claimed exactly once through a persisted acquisition intent.
- [x] Lost requests and responses at every handoff recover without a duplicate run or unjournaled active claim.
- [x] Progress and failure projections are durable, ordered per execution, and deduplicated.
- [x] Pause or cancel is durably acknowledged and stale controller writes are rejected.
- [x] Restarting AppDeck or Marina preserves the exact run, task snapshot, lease authority, budgets, and truthful visible state.
- [x] No runtime or source-control mutation is enabled yet; this gate proves the control system first.

## Milestone 2 — always-on pair and first Kubernetes runtime

### Always-on AppDeck and Marina

- [x] [Marina] Build a static, non-root control-service image for Linux AMD64 and ARM64.
- [x] [Marina] Add a single-replica Kubernetes base with `ReadWriteOnce` journal storage, `Recreate` rollout semantics, resource limits, and real probes.
- [x] [AppDeck] Add a non-root single-replica Kubernetes base with its own `ReadWriteOnce` SQLite volume, `Recreate` updates, startup/liveness/readiness probes, and demo mode disabled.
- [ ] [AppDeck] Add authenticated human sessions before exposing the always-on installation.
- [ ] [Joint] Deploy the selected service-authentication and run-fencing mechanisms using rotatable secret references.
- [ ] [Joint] Add a paired local environment that starts production AppDeck and Marina binaries with separate databases and no in-process shortcut.
- [ ] [Joint] Add a Kubernetes overlay with separate service accounts, PVCs, backups, network policies, resource limits, and an AppDeck-only authenticated ingress.
- [ ] [Joint] Verify backup and restore of both databases independently, then reconcile the exact cross-product run without timestamp-based last-write-wins repair.
- [ ] [AppDeck] Show provider connectivity, synchronization lag, lease expiry, degraded dependencies, and stale or rejected projections truthfully.
- [ ] [Joint] Select and deploy the paired control plane to the first always-on Kubernetes cluster.

### Operator clients

- [x] [CLI] Create a separate top-level `cli/` project with its own module, binary, tests, and configuration; communicate with AppDeck only through supported public HTTP/JSON APIs.
- [x] [CLI] Add independent packaging and release automation.
- [x] [CLI] Ship a small first workflow: configure the AppDeck endpoint and credentials, verify connectivity, create and list work, and inspect task and run state.
- [x] [CLI] Add pause, resume, cancel, replan, and retry workflows using AppDeck's durable command API.
- [ ] [TUI] Create a separate top-level `tui/` project after the CLI workflows and client boundary stabilize; use the public API or a reusable public client library, never AppDeck internals.

### Provider-neutral runtime lifecycle

- [ ] [Marina] Define the runtime adapter lifecycle: start, inspect, resume, cancel, collect artifacts, and cleanup.
- [ ] [Marina] Persist runtime-attempt ID, idempotency key, provider handle, state, heartbeat, usage, output references, deadline, and cleanup state.
- [ ] [Marina] Specify behavior for lost start responses, duplicate starts, unsupported resume, forced cancellation, provider disappearance, and orphan discovery.
- [ ] [Marina] Materialize and verify a pinned repository revision in an isolated workspace.
- [ ] [Marina] Create an immutable runtime specification containing context digest, harness version, limits, credentials references, network policy, and authority fence.
- [ ] [Marina] Implement the first Kubernetes Job runtime adapter without exposing Kubernetes details through the public runtime contract.
- [ ] [Marina] Run one real agent harness in an ephemeral Kubernetes Job.
- [ ] [Marina] Apply CPU, memory, ephemeral-storage, wall-time, process, network, and filesystem limits.
- [ ] [Marina] Give the Marina controller and agent Jobs separate service accounts and least-privilege RBAC.
- [ ] [Marina] Issue task-scoped, short-lived source-control and artifact credentials whose lifetime cannot exceed authority and budget.
- [ ] [Marina] Stream heartbeats and normalized events without exposing secrets or high-volume raw logs to AppDeck.
- [ ] [Marina] Persist checkpoints, artifacts, and evidence outside the Job with immutable digests.
- [ ] [Marina] Verify idempotent cleanup after success, failure, timeout, eviction, cancellation, and controller restart.
- [ ] [Joint] Show runtime attempts under the logical AppDeck run without presenting ephemeral Pods as durable agents.

### Milestone 2 exit gate

- [ ] AppDeck and Marina remain available in the selected cluster while the operator's laptop is offline.
- [ ] Restarting either Deployment preserves one exact run and truthful synchronization state.
- [ ] Marina can lose a Kubernetes start response, rediscover the exact Job, and avoid a duplicate attempt.
- [ ] Cancelling in AppDeck terminates or fences the Job within a measured bound.
- [ ] A checkpoint and artifact survive Job deletion and can be inspected through AppDeck metadata.
- [ ] No runtime retains usable credentials after its authority or budget ends.

## Milestone 3 — unattended merge-ready pull-request loop

### Intent, plans, and policy

- [ ] [AppDeck] Expand task budgets from minutes into structured time, attempts, tokens, cost, concurrency, and authority limits.
- [ ] [AppDeck] Store immutable, numbered plan revisions with context digest, risk class, verification strategy, expected side effects, and rollback approach.
- [ ] [AppDeck] Bind every decision request and answer to an exact run, plan revision, and expiry; reject stale answers.
- [ ] [AppDeck] Support change requests by creating a new plan revision rather than blocking the task or overwriting history.
- [ ] [AppDeck] Add project-scoped agent-native channels where humans and agents can converse, reference canonical tasks, runs, decisions, artifacts, and evidence, and promote a message into a bounded versioned task without making chat a second ledger or a source of ambient execution authority.
- [ ] [Marina] Evaluate versioned project policy before claim, runtime start, external mutation, completion, and any expansion of authority.
- [ ] [Joint] Display the active task, plan, policy, and authority versions in AppDeck for every consequential action.

### Context and implementation

- [ ] [Marina] Assemble an immutable, content-addressed context package with provenance, trust, scope, freshness, and expiry labels.
- [ ] [Marina] Include the frozen task snapshot, repository instructions, relevant architecture decisions, policy version, pinned source revision, and tool/harness versions.
- [ ] [Marina] Treat issue text, comments, retrieved documents, logs, and tool output as untrusted inputs and add prompt-injection boundary tests.
- [ ] [Marina] Persist decisions and unresolved state as checkpoints rather than relying on runtime conversation history.
- [ ] [Marina] Submit a versioned plan and reconcile approval or change-request decisions through the work-control contract.
- [ ] [Marina] Execute the approved plan through the runtime adapter without exceeding its frozen authority or budget.
- [ ] [Marina] Add a narrow source-control port for branch creation, guarded writes, pull-request creation, current-head inspection, and review/CI observations.

### Verification, evidence, and delivery

- [ ] [Marina] Run repository-defined deterministic tests and scanners against the produced source revision.
- [ ] [Marina] Add risk-appropriate independent review whose identity and context are distinct from the producing runtime.
- [ ] [Marina] Invalidate evidence automatically when the source, context, plan, policy, command, or observation window no longer matches.
- [ ] [AppDeck] Replace the completion Markdown blob as the gate with structured evidence records containing producer, verifier, subject digest, command, outcome, provenance, freshness, URI, and digest.
- [ ] [AppDeck] Render a concise Markdown verification report from structured evidence for human readability.
- [ ] [Joint] Define and test the minimum evidence set for a merge-ready pull request.
- [ ] [Marina] Create a pull request with source-bound evidence and a stable link to the AppDeck run.
- [ ] [Marina] Reconcile CI failures, review feedback, head changes, and merge conflicts through bounded revision loops.
- [ ] [Marina] Re-run only invalidated gates and retain superseded evidence without presenting it as current.
- [ ] [AppDeck] Show planning, waiting, running, retrying, verifying, syncing, stale-evidence, blocked, and completed states without activity theatre.
- [ ] [AppDeck] Present focused decisions with recommendation, risk, evidence, cost, and the consequence of waiting or rejecting.
- [ ] [Marina] Stop at a merge-ready pull request by default and refuse merge or release without an explicit policy capability.
- [ ] [Joint] Demonstrate the complete loop while the operator is offline, including at least one bounded CI or review revision.

### Milestone 3 exit gate

- [ ] One bounded task produces one merge-ready pull request at the pinned and currently reported head revision.
- [ ] AppDeck shows immutable plan history, commands, costs, artifacts, current evidence, and any superseded evidence.
- [ ] Restarting AppDeck, Marina, or the agent workload during planning, execution, verification, or completion does not duplicate external side effects.
- [ ] The producing runtime is not the sole verifier, and every required gate is current for the final revision.
- [ ] Completion after a lost AppDeck response replays idempotently and both products converge on the same terminal outcome.

## Milestone 4 — delivery and operational feedback

- [ ] [Marina] Integrate preview and non-production delivery without replacing the repository's existing CD system.
- [ ] [Joint] Define observation windows and admissible deployment-health evidence by risk class.
- [ ] [Marina] Add canary verification and automatic rollback within explicit versioned policy.
- [ ] [AppDeck] Present environment, rollout, observation-window, rollback, and policy state without exposing provider-specific control details as the product model.
- [ ] [Joint] Select the first production-feedback source.
- [ ] [Marina] Validate, correlate, classify, and deduplicate signals before creating or claiming diagnostic work.
- [ ] [Marina] Keep raw signal text untrusted and separate observation from diagnosis and mutation authority.
- [ ] [Joint] Turn one validated signal into bounded diagnostic work with an inspectable causal chain.
- [ ] [Joint] Demonstrate diagnosis, remediation proposal, canary verification, rollback or confirmation, and recovery evidence.
- [ ] [AppDeck] Show the operator one continuous evidence trail from signal through decision, change, delivery, observation, and final outcome.

## Cross-cutting factory controls

- [ ] [Marina] Version autonomy policy by project, change class, environment, and risk.
- [ ] [Marina] Enforce time, attempt, token, cost, concurrency, credential-lifetime, and blast-radius budgets.
- [ ] [Joint] Add identity, authorization, fencing, audit, and secret-redaction tests at every boundary.
- [ ] [Marina] Add context provenance, freshness, trust, and revocation controls.
- [ ] [Joint] Measure verified outcomes, human attention, intervention quality, retries, failures, synchronization lag, and cost.
- [ ] [Marina] Add entropy-control loops for documentation, dependencies, architecture, and dead code.
- [ ] [Joint] Add protocol compatibility, migration, rolling-upgrade, backup, restore, and downgrade tests.
- [ ] [Joint] Define artifact retention, integrity verification, garbage collection, and recovery without shared filesystems.
- [ ] [AppDeck] Add deterministic export and import for tasks, plans, decisions, commands, events, artifact metadata, and evidence.
- [ ] [Marina] Add journal inspection and redacted diagnostic export without making internal state a second work-control API.
- [ ] [Joint] Add load and backpressure tests for ready-work polling, event outbox replay, commands, heartbeats, and SSE clients.
- [ ] [Joint] Maintain a crash matrix that injects failure before and after every external call and durable commit in the critical path.
- [ ] [Joint] Document operator recovery for unavailable provider, corrupt or restored storage, expired credentials, stuck cancellation, and orphan runtime.

## Decisions still needed

- [ ] Choose where the first always-on Kubernetes installation runs.
- [x] Choose SQLite as Marina's durable journal for the single-replica stage.
- [ ] Choose human authentication for AppDeck.
- [x] Choose scoped opaque credentials for initial Marina-to-AppDeck service authentication.
- [ ] Choose the first artifact-store implementation, backup method, and retention policy.
- [ ] Choose the first real agent harness for the Kubernetes runtime.
- [ ] Choose the source-control credential broker or installation mechanism.
- [ ] Choose the initial policy representation and evaluator.
- [ ] Choose the runtime that later validates portability.
- [ ] Choose the first production-feedback source.
- [ ] Define actions that always require a human.
- [ ] Define the evidence required to expand autonomy beyond merge-ready pull requests.

## First-proof completion criteria

The first proof is complete only when the operator can create bounded work in AppDeck, disconnect, and later find one truthful merge-ready result or focused escalation with durable state, costs, artifacts, and source-bound evidence—even if AppDeck, Marina, or its agent workload restarted during execution.

The proof must additionally show that:

- one task revision produced one provider run and one bound Marina execution;
- lost responses and repeated deliveries caused no duplicate external side effects;
- pause, cancellation, budget exhaustion, and stale authority were enforced;
- all completion evidence matches the final pull-request head and active policy;
- the producer was not its only consequential verifier;
- no secret appeared in persisted context, logs, events, artifacts, or evidence;
- AppDeck explains what changed, why, what was verified, what failed, what it cost, and what still requires human judgment.
