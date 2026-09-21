# Work-control contract map

The shared contract has two deliberately separate owners inside the monorepo:

- Deck's provider-specific OpenAPI and canonical version-1 JSON fixtures live in [`deck/api/`](../deck/api/).
- Marina's provider-neutral capabilities and reusable conformance suite live in [`marina/internal/workcontrol/`](../marina/internal/workcontrol/).

The fixture manifest is [`deck/api/fixtures/v1/manifest.json`](../deck/api/fixtures/v1/manifest.json). Planned fixture schemas describe the intended additive version-1 shapes but do not imply runtime availability. Deck advertises only implemented capabilities through `GET /api/v1/provider`.

Run `make conformance` from the repository root to build a real Deck binary, start it with an isolated SQLite database and ephemeral scoped service credential, and exercise it through Marina's consumer ports. The runner generates the credential at runtime, gives Deck only its SHA-256 fingerprint, gives Marina only a token-file reference, and removes both on exit.

The initial stable-identity, project-scope, capability-scope, and rotation semantics are recorded in [service-authentication decision 0003](../docs/decisions/0003-service-authentication.md). Authentication failures are part of the canonical version-1 error fixture set.

Claims and every run mutation use the version-1 `Idempotency-Key` convention. Deck atomically stores the scoped key, a request digest, and the original successful result; exact retries replay, while a changed payload uses the canonical non-retryable `conflict` error. Active-run mutations also carry the stable logical agent ID, exact run version, and current fence token.

The Milestone 1 real-process suite proves claim replay and recovery lookup, assigned-run discovery, task-snapshot binding, guarded projections, producer-event deduplication, durable cancellation and acknowledgement, budget stopping, lease reaping, stale-fence rejection, and Marina journal restart against a real Deck binary.

Do not create a second copy of the OpenAPI document or fixture corpus under this directory. This file is a stable map to the authoritative producer and consumer locations.

The context extension adds immutable source/message references to Task, freezes
selection per Run, and exposes exact-version resolution via `context.read`.
Repository association uses operator intent plus Marina's safe versioned
confirmation (`repository.read` / `repository.write`). Credential references stay
in Marina. Identical source/request revision retries replay; stale concurrent
edits and confirmations conflict. Canonical `context.json` and `repository.json`
fixtures cover these additive v1 shapes. Real-process conformance demonstrates
approved decision reuse, revision isolation, revocation, and repository changes
while original authorized work retains its binding.
