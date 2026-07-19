# 0002: Use SQLite for the Initial Execution Journal

- Status: Accepted for the single-operator, single-replica stage
- Date: 2026-07-17

## Context

Marina needs durable internal execution state before it may claim real work. The journal must support atomic transitions, optimistic concurrency, unique external-run ownership, leases, checkpoints, evidence gates, append-only events, restart recovery, and deterministic local tests.

The initial product has one operator and one control-service replica. High availability, multiple writers, and multi-tenant isolation are not current requirements.

## Options considered

### SQLite

Provides transactions, constraints, indexes, migrations, and a self-contained deployment. It works locally and in a single Kubernetes replica with a persistent volume. Its main boundary is that the database file must not be shared by active replicas across nodes.

### PostgreSQL

Provides strong multi-writer and high-availability paths, but adds another service, credentials, lifecycle, backup, and operational failure mode before Marina needs those capabilities.

### Custom append-only files

Would keep the dependency surface small but require Marina to implement transactional updates, indexes, compaction, concurrency control, corruption handling, and query semantics that an embedded database already provides.

## Decision

Use SQLite in WAL mode as Marina's initial internal execution journal.

- Run one Marina control-service replica against each database file.
- Store the file on a single-writer persistent volume in Kubernetes.
- Keep the journal behind an internal contract and migrations.
- Use uniqueness constraints and guarded versions for idempotency and concurrency.
- Keep AppDeck and Marina persistence separate; never share or directly query AppDeck's database.
- Use the pure-Go `modernc.org/sqlite` driver so the Marina binary does not require CGO.

## Consequences

- The first deployment is intentionally not an active-active control plane.
- Backup and restore can operate on one self-contained database and its documented volume.
- Runtime workloads may scale independently; only Marina's journal writer remains single-replica.
- The schema and journal behavior, not SQLite-specific details, define the internal storage contract.

## Move to PostgreSQL when

Revisit this decision when Marina needs more than one active control-service replica, concurrent writers on different nodes, high-availability failover without a single-writer restart, multi-operator tenancy, or workload volume that exceeds measured SQLite limits.

Migration must be driven by those observed requirements, not by anticipated scale.
