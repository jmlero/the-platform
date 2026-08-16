# Decision 0006: Make plans, evidence, channels, and operational state first-class AppDeck ledger records

- Status: accepted
- Date: 2026-08-16
- Scope: AppDeck work-control ledger and additive version-1 service capabilities

## Context

AppDeck's first control-system proof stored one mutable Markdown plan and used a Markdown completion blob as the verification gate. That was sufficient to prove claims, leases, fencing, commands, projection ordering, and restart recovery, but it could not safely represent revision loops, stale decisions, evidence freshness, provider synchronization, or delivery observation. Chat would also become a dangerous second work queue if a message could silently grant execution authority.

The next AppDeck milestone needs structured budgets, immutable plan history, source-bound evidence, focused decisions, project conversation, provider-neutral operational feedback, and portable export without importing Marina's execution internals into AppDeck.

## Decision

AppDeck extends its canonical SQLite ledger and version-1 HTTP contract with the following additive records and capabilities:

- Tasks retain `budget_minutes` for version-1 compatibility and also store a structured budget containing time, deadline, attempts, tokens, cost, currency, concurrency, and explicit authority identifiers.
- Every submitted plan creates an immutable, monotonically numbered `PlanRevision`. The run points at one active revision; the compatibility Markdown `plan` field is a projection of that revision and is never the source of plan history.
- Plan decisions bind to an exact run, plan-revision ID, and optional expiry. AppDeck rejects expired or superseded answers. A change request returns the same fenced run to planning; the agent must submit the next numbered revision rather than editing or deleting the rejected revision.
- Completion requires IDs of current structured evidence records. Each record binds producer, verifier, subject digest, source revision, command, outcome, provenance URI and digest, observation and expiry times, artifact URI and digest, plan revision, and policy version. AppDeck renders the human-readable Markdown verification report from the admitted records. The in-process demo uses the same evidence gate; no Markdown-only completion path remains.
- Project channels contain human, agent, and system messages with validated references to canonical task, run, decision, artifact, and evidence IDs. A message grants no run authority. Promotion is an explicit operator mutation that creates a new version-1 task with acceptance criteria and a structured budget, then records the resulting task ID on the message.
- Provider connectivity, projection lag, stale and rejected projection counts, degraded dependencies, and active lease expiry are derived from canonical heartbeats, runs, and projection-admission records. Unknown data is displayed as unknown rather than inferred activity.
- Provider-neutral operational records describe environment, rollout, observation window, rollback readiness, source revision, and policy version. AppDeck does not model Pods, Jobs, cloud deployment IDs, or provider control operations as product state.
- Task detail exposes a deterministic trail joining signals, decisions, changes, delivery, observation, verification, and outcomes. Display state is derived from ledger facts and can distinguish planning, waiting, running, retrying, verifying, syncing, stale evidence, blocked, and completed work.
- Project export uses versioned deterministic JSON. It contains the project plus agents needed by its runs, tasks, runs, plan revisions, decisions, commands, events, artifact metadata, evidence, channels, messages, and operational state in stable order. Artifact content and credentials are excluded. Import is transactional and requires an empty target project so it cannot merge two authorities by timestamp.

The new service capabilities are additive. Marina and other consumers continue to require only the capabilities needed for behavior they enable. AppDeck owns the provider-specific OpenAPI and fixtures; Marina remains provider-neutral and does not import AppDeck packages or read its database.

## Consequences

- AppDeck can explain what changed, which plan and source were verified, who produced and verified the evidence, what the operator decided, and what delivery state was observed without overwriting history.
- Completion requests now require an evidence-record step before the terminal mutation. Stable evidence IDs, run guards, and idempotency keys make that step safely replayable.
- A requested change is a revision loop rather than a blocked terminal state. Existing lease and fence semantics still protect the run while it replans.
- Project chat remains useful for collaboration without becoming a second scheduler or ambient permission channel.
- Export is suitable for audit, backup handoff, and deterministic tests, but it is deliberately not a last-write-wins merge format. Restoring into a non-empty project requires an explicit future reconciliation design.
- Provider health counters are operational observations. Historical stale or rejected counts remain visible; a later accepted projection clears the current error state but not the audit totals.

## Follow-up boundaries

Marina must publish evidence and operational state only after its corresponding producer, verifier, policy, source, and observation semantics exist. AppDeck must not advertise future provider capabilities merely because their schemas are planned. A later incompatible evidence or import semantic requires a new transport major version rather than silently changing `/api/v1`.
