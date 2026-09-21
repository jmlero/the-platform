# Decision 0004: Milestone 1 work-control safety protocol

- Status: Accepted
- Date: 2026-08-12
- Scope: Deck–Marina lossless handoff, authority, commands, and projection

## Context

Claiming work spans two independent SQLite ledgers and an HTTP call. Deck can commit while Marina loses the response, either process can restart before the next local commit, a lease can be reaped during recovery, and operator cancellation can race delayed worker writes. Treating the call as exactly once or identifying authority only by agent ID would permit duplicate runs or stale mutation.

## Decision

Deck is the authority for each run's monotonically increasing resource version and ownership fence. A claim allocates a new fence epoch and freezes the task version plus acceptance-criteria digest. Every active-run service mutation carries a durable idempotency key, stable logical agent ID, exact run version, and current fence token. Lease reaping and authority-reducing operator commands advance the fence before old work can continue.

Marina persists a stable installation and logical-agent identity, a versioned project binding, and an acquisition intent before calling claim. Recovery first resolves the original claim key or assigned-run listing and rechecks the current claim immediately before binding; it never issues a fresh blind claim after an ambiguous response. One provider run maps to one durable execution.

Operator commands are ordered durable resources. Marina persists a command cursor and publishes acknowledgements through a priority outbox so cancellation and authority reduction pre-empt ordinary projections. Other projections retain per-execution order, use stable producer identities and sequences, and cannot overtake an unpublished predecessor. Deck deduplicates both the producer event ID and execution sequence.

Before sending an outbox mutation, Marina persists its agent, resource version, and fence. Retries replay that original request even after a heartbeat or restart; events without an explicit occurrence time use the durable enqueue time. Only after Deck definitively rejects the original request may Marina retry with a newer resource version, and only for the same run, work item, agent, and fence. These retries remain bounded. The [September 2026 regression evidence](../validation/review-fixes-2026-09-21.md) covers lost requests and responses for events and command acknowledgements.

External reconciliation is time-bounded and cancellation-aware. Retry attempt, next eligibility, failure class, and exhaustion reason are persisted. Exhausted or permanent reconciliation failures become classified terminal transitions and enter the same outbox. The controller refuses overlapping cycles and does not bind work whose lease is inside the safety window.

Milestone 1 enables automatic control-plane claims only. Runtime, source-control, artifact, and delivery bindings remain disabled until their own fenced, restart-safe adapters exist.

## Consequences

- A lost request or response can be replayed or looked up without creating a second run.
- A reaped, cancelled, or otherwise fenced worker cannot regain authority with an older response snapshot.
- Deck and Marina may be temporarily out of sync, but both retain enough durable identity and ordering information to converge without timestamp-based last-write-wins repair.
- Command acknowledgements may overtake ordinary projections by design; ordinary projections cannot overtake each other.
- The initial deployment remains single-controller and single-writer SQLite. Optimistic versions and idempotency still protect accidental overlap, but active-active deployment requires a later storage decision.

## Verification

Product tests cover crash boundaries, lease-reaper races, stable identity, task snapshots, outbox ordering/exhaustion, concurrency conflicts, classified retry, and restart persistence. `make conformance` exercises the authenticated contract against a real Deck process, including claim recovery, Marina restart, deduplicated projection, cancellation, budget stop, provider lease failure, and stale-fence rejection.
