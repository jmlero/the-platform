# P1 and P2 review fixes — 2026-09-21

Scope: the three P1 findings and one P2 finding from the repository review. Verification was performed on the working tree before commit. No deployment was performed for this task. Other pending changes were preserved and included in the operator-requested commit of all repository changes.

| Finding | Corrected behavior | Regression evidence |
| --- | --- | --- |
| P1: blocking an old terminal run could corrupt a reclaimed task | Reject a new block request unless exactly one active run changed. Replaying the original successful request remains harmless. | [Store regression](../../deck/internal/store/block_run_test.go) |
| P1: lost outbox responses could permanently stall publication after lease synchronization | Persist the request guard before sending; replay it across restart. Refresh a rejected stale version only within the same ownership fence. Missing occurrence times use the persisted enqueue time. | [Restart and revoked-fence tests](../../marina/internal/reconcile/outbox_test.go), [real Deck HTTP tests](../../marina/internal/deck/real_outbox_test.go) |
| P1: partial model usage could release a reservation after a truncated stream | Require terminal response status and both valid usage counters before settling reserved tokens and cost. Unknown usage stays reserved across restart. | [Model usage regression](../../marina/internal/workerapi/model_usage_test.go) |
| P2: saving a task could shift its deadline | Display and parse local time; retain the exact original instant when unchanged, including seconds and the repeated daylight-saving hour. | [Deadline regression](../../deck/web/src/lib/deadline.test.ts) |

Verification passed:

- `make check`: installation-script tests, CLI tests and packaging checks, Deck and Marina Go tests and vet, frontend tests, and Svelte checks (zero errors or warnings).
- `make conformance`: CLI operator session, Deck provider contract, repository workflow, and all four lost-outbox scenarios against a real temporary Deck process with a separate SQLite store. Requests and responses were deliberately dropped; model and source-provider behavior remained deterministic test fixtures.
- Focused old-run and outbox tests passed again after adding idempotent-replay and revoked-fence assertions.
- `git diff --check` and relative links in the changed documentation were checked.

[Migration 010](../../marina/internal/journal/sqlite/migrations/010_outbox_guard.sql) adds the persisted outbox guard. It does not reconstruct requests already sent by older binaries or reset previously exhausted entries. No existing installation data was modified. This evidence does not claim a Kubernetes rollout or live model acceptance for these fixes.
