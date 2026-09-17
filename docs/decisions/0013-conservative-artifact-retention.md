# 0013 — Conservative offline artifact retention

- Status: Accepted for the single-writer first release
- Date: 2026-09-17
- Scope: OPS-05; Marina-owned artifact bytes

Marina's content-addressed store can accumulate uploads that never reached the
journal and temporary publication blobs. Removing old files by age alone would
destroy evidence still referenced by Deck or needed for recovery.

Provide an explicit offline `marina artifact-retention` command. It defaults to a
JSON dry run. Applying requires that reviewed report, no older than 24 hours,
and a fresh scan that yields the identical candidate set. Default minimum age is
seven days; the permitted minimum is 24 hours. Each batch removes at most 100
files by default, with an explicit upper bound of 1,000.

Keep every artifact referenced by Marina's runtime records, checkpoints, evidence,
context manifests, events or provider outbox, including terminal executions and
already-published history. There is no task-history expiry in this release.
Marina journals result references before exposing their evidence to Deck, so
retaining those references also protects the evidence it has published. Deck
retains its own ledger and does not grant the collector access to its database.
Never run collection against a subset/export/stale copy of Marina's journal.

The serving workflow and collector hold the same exclusive artifact-directory
lock. The collector also rejects active executions and a directory not bound in
the journal. Drain or cancel work, stop Marina, then run maintenance on its real
volume. Disabling intake alone does not stop the controller. Install the updated
binary before relying on the lock; older binaries do not participate in it.

Only regular files named by valid content hashes are eligible. Verify contents
before listing a candidate. Leave symlinks, unknown names, pending writes,
recent files and referenced artifacts untouched. Changed references or file
metadata invalidate the plan before any deletion. A failed deletion reports the
number already removed; a fresh dry run is required after partial application.

This deliberately avoids an online collector, an additional database, an
automatic deletion schedule, and a second authoritative reference ledger.
The tradeoff is a maintenance window and indefinite retention of referenced
history. Revisit when explicit task-history expiry or measured storage pressure
requires a broader policy. Retention is not a backup or restore mechanism.
