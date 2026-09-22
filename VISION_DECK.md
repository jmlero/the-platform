# Deck vision

## Purpose

Deck is the operator surface and authoritative human-facing ledger for work
performed by Marina or another compatible execution system. It keeps outcomes,
budgets, plans, decisions, commands, progress, artifacts, and evidence durable
and understandable.

Deck also owns preparing work: managing the backlog, refining broad outcomes into
small focused tasks, defining their dependencies and acceptance criteria, and
selecting the right context for each agent. This is accepted product direction;
the decomposition and readiness flow is planned in
[decision 0018](docs/decisions/0018-deck-work-preparation.md).

Deck does not schedule Pods, check out repositories, call models, or maintain an
execution journal. Its public contract lets replaceable workers participate
without gaining direct database access.

## Experience

Deck should present three connected views of the same work:

- a board for prioritizing outcomes and preparing focused, verifiable child tasks;
- project conversations for developing intent and delegating deliberately; and
- an inbox for decisions and unreviewed results.

A task and a run are different records. The task preserves intended outcome and
acceptance criteria; each run preserves one attempt, its plan, authority, events,
decisions, and evidence. Conversation references the same records rather than
copying their state.

The interface should emphasize information that changes an operator decision:
work awaiting approval, active work, blockers, and reviewable results. Detailed
events remain available without turning routine heartbeats into activity theater.

## Current release

Deck currently provides:

- SQLite-backed projects, tasks, budgets, runs, plans, decisions, commands,
  events, channels, artifacts, and structured evidence;
- authenticated operator sessions and scoped service identities;
- atomic claims, leases, versions, fence tokens, and idempotent mutation replay;
- immutable task and context selections for active runs;
- explicit repository association through Marina;
- an attention/active/results overview and a detailed task drawer;
- authenticated, digest-checked access to Marina-owned artifacts;
- server-sent event updates, paginated channel history, and deterministic
  project export/import; and
- a versioned OpenAPI contract consumed by Marina and platformctl.

The supported repository workflow ends at a verified PR for human review or a
blocker. Marina implements cancellation and rejects pause/resume, replan, retry,
and authority-reduction commands for this workflow. The ledger may store command
types that a particular execution provider does not support.

Deck also exposes installed workload-profile discovery and idempotent invocation.
The current second profile is a pinned diagnostic analysis workload, not arbitrary
agent hosting.

## Product principles

1. **The board is a view.** Precise tasks, runs, leases, plan revisions,
   decisions, commands, and evidence form the data model.
2. **Status is derived from durable facts.** Running, waiting, blocked, stale,
   failed, cancelled, and completed must have distinct meanings.
3. **Steering is explicit.** Operator actions become versioned commands or
   decisions with acknowledgement and history.
4. **Completion requires evidence.** Results stay tied to acceptance criteria,
   source or input revisions, and current verification.
5. **Authority is bounded.** Time, attempts, tokens/cost, concurrency, and
   permissions are visible and auditable.
6. **Agents are replaceable.** The API and contract remain independent of one
   harness or model.
7. **Portable by design.** One process and SQLite remain easy to inspect, move,
   back up, and restart until measured needs justify a different store.

## Near-term direction

Add a work-preparation flow from backlog outcome to a versioned breakdown,
per-child context, bounded authorization, and an eligible task queue. Deck owns
the proposed and accepted work; Marina runs planning assistance and assembles
immutable execution packages. Begin with manual preparation and sequential work,
then add agent-generated proposals through the selected kagent integration.
The [implementation design](docs/work-preparation.md) covers shared budgets,
dependencies, review capacity, and evidence-based parent acceptance.

The next Deck work links conversations to tasks, adds explicit agent invocation,
persists clarification and result replies, and tracks reviewed/unread results
across browser sessions. Existing channels, project context, tasks, decisions,
and evidence should be extended rather than replaced.

Automatic repair loops, independent semantic review, multi-user permissions,
federated identity, HA, merge/deploy authority, and general project-management
expansion remain later decisions. [TODO.md](TODO.md) is the active backlog;
[ARCHITECTURE.md](ARCHITECTURE.md) defines the product boundary.

Deck succeeds when an operator can tell what work was authorized, what is
happening, why it stopped, and whether the result is supported by current
evidence.
