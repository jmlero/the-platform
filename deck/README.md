# Deck

Deck is the task authorization, decision and results surface for work carried
out by Marina. Its default overview separates work needing attention, active
work and reviewable results. Operators can approve a plan, cancel a run, inspect
the PR and download verification evidence.

Use the [paired installation guide](../deploy/kubernetes/first-release/README.md)
for the first repository workflow. It enables authenticated operator sessions,
TLS, separate service credentials and digest-checked access to Marina's artifact
store. Channel and operational APIs remain compatible; their expansion is deferred.

This directory contains the first working prototype. Read the root [Deck vision](../VISION_DECK.md) for the product direction and [ARCHITECTURE.md](../ARCHITECTURE.md) for its boundary with Marina.

## What the prototype demonstrates

- Persistent tasks backed by SQLite.
- Optional authenticated human sessions backed by SQLite with CSRF-protected browser mutations.
- Backlog refinement and a Kanban-style execution board.
- Atomic task claims with expiring leases.
- A durable distinction between a task and its execution runs.
- Structured task budgets covering time, deadline, attempts, tokens, cost, concurrency, and explicit authority.
- Agent registration, heartbeats, immutable numbered plan revisions, progress, and verification.
- Focused, expiring human decisions bound to one run and plan revision; change requests preserve history and reopen planning.
- Structured, source-bound evidence as the public completion gate, with a generated Markdown verification report.
- Project-scoped human and agent channels with canonical ledger references and explicit promotion into a bounded task.
- Append-only events streamed live to the browser with SSE.
- Verification artifacts and their immutable metadata attached to completed runs.
- Provider connectivity, synchronization, lease, dependency, and rejected-projection truth derived from ledger facts.
- Provider-neutral environment, rollout, observation-window, rollback, and policy records.
- A continuous operator trail from signal and decision through change, delivery, observation, verification, and outcome.
- Deterministic, versioned project export and transactional import into an empty target project.
- Optional service authentication with stable identities and explicit project and capability grants.
- Durable idempotency records for claims and every run mutation.
- Monotonic run versions and ownership fence tokens required on every service mutation.
- Frozen task-version and acceptance-criteria digests on claimed runs.
- Claim recovery and paginated active-run discovery for stable Marina controllers.
- Durable command records and ordered acknowledgements. The configured Marina repository workflow implements cancel and rejects pause/resume, replan, retry, and reduce-authority; recording a command does not establish runtime support.
- Producer-event and per-execution sequence deduplication for Marina projections.
- A restart-resumable demo agent named **Atlas**.
- A compiled frontend embedded in one Go application binary.

## Quick start

Requirements:

- Go 1.26+
- Node.js 24+
- npm 11+

Install, build, and run:

```bash
make setup
make run
```

Open [http://localhost:8080](http://localhost:8080).

On a new database Deck seeds a small demo workspace. Atlas claims the ready task and submits a plan. Open **Decisions**, approve the plan, and return to **Agent factory** to watch the durable lifecycle progress through execution, verification, and completion.

Runtime data is stored in `./data/deck.db` by default.

When updating an existing installation, follow the [Deck naming upgrade](../docs/decisions/0012-deck-component-name.md)
to retain its database, credentials and persistent volume.

## Development

Run the backend and frontend in separate terminals:

```bash
make dev-api
```

```bash
make dev-web
```

The Vite server runs at [http://localhost:5173](http://localhost:5173) and proxies API requests to the Go server on port 8080.

Useful commands:

```bash
make check    # Go vet and Svelte diagnostics
make test     # Go and frontend tests
make build    # Static frontend plus one Go binary
make image    # Build the Deck container image
make manifests # Render the Kubernetes deployment base
```

## Container

```bash
docker compose up --build
```

The image runs as an unprivileged user and stores its SQLite database in the `/data` volume.

For an initial Kubernetes deployment, run a single replica with a persistent volume mounted at `/data`. SQLite WAL mode is intentionally a single-host design. A future multi-replica mode should introduce PostgreSQL rather than share the SQLite file across hosts.

The hardened single-replica base is under [deploy/kubernetes/](deploy/kubernetes/). Render it with `make manifests`; use an environment overlay to select an immutable image, storage class, service-authentication Secret, encrypted transport, backup mechanism, and authenticated human ingress.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DECK_ADDR` | `:8080` | HTTP listen address |
| `DECK_DATA_DIR` | `./data` | Directory containing `deck.db` |
| `DECK_DEMO_AGENT` | `true` | Seed demo tasks and run Atlas |
| `DECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `DECK_SERVICE_AUTH_FILE` | unset | Version-1 service-identity policy for authenticated machine API operations |
| `DECK_OPERATOR_AUTH_FILE` | unset | Version-1 operator-token fingerprint policy used to create human sessions |
| `DECK_OPERATOR_COOKIE_SECURE` | `true` | Require HTTPS for operator cookies; set `false` only for trusted local HTTP development |

## Operator authentication

For an exposed installation, configure `DECK_OPERATOR_AUTH_FILE` with a stable operator ID and the SHA-256 fingerprint of a token containing at least 256 bits of cryptographically random material:

```json
{
  "version": 1,
  "operators": [
    {
      "id": "operator-primary",
      "token_sha256": ["<64-lowercase-hex-characters>"]
    }
  ]
}
```

The login page exchanges the raw token for a 12-hour server-side session. Deck persists only SHA-256 digests of the session and CSRF secrets, sets a host-only `HttpOnly` session cookie with `SameSite=Strict`, rejects cross-origin authenticated browser requests, and requires the browser to echo the separate CSRF cookie in `X-CSRF-Token` for mutations. Sessions survive process restart and are deleted on logout.

Deck reloads the policy for every login and readiness check, so token fingerprints can overlap during rotation without changing the operator ID. Existing sessions remain valid until logout or expiry. Keep the raw token in a secret manager or mounted secret file, never in this repository or Deck data. Outside trusted loopback, use TLS and leave secure cookies enabled.

When `DECK_OPERATOR_AUTH_FILE` is unset, the local prototype keeps its original unauthenticated operator workflow. That mode must not be exposed to an untrusted network. The complete rationale is in [decision 0005](../docs/decisions/0005-operator-authentication.md).

## Service authentication

Set `DECK_SERVICE_AUTH_FILE` before connecting Marina outside the trusted local prototype. The JSON policy maps one or more SHA-256 token fingerprints to a stable identity, explicit Deck project IDs, and explicit protocol capabilities:

```json
{
  "version": 1,
  "identities": [
    {
      "id": "marina-primary",
      "token_sha256": ["<64-lowercase-hex-characters>"],
      "projects": ["prj_default"],
      "capabilities": [
        "provider.info", "agent.register", "work.ready.list", "work.inspect",
        "work.claim.atomic", "work.claim.lookup", "run.assigned.list",
        "run.lease.renew", "run.plan.submit", "run.start", "run.progress.append",
        "run.verify", "evidence.record", "run.complete", "run.block",
        "command.list", "command.ack", "event.append", "operation.report",
        "channel.list", "channel.message.list", "channel.message.append"
      ]
    }
  ]
}
```

Use at least 256 bits of random token material and give the raw value only to Marina through its token-file reference. Deck reloads and validates the policy on every request, so two fingerprints can overlap during rotation without changing the identity. Wildcard grants, malformed policies, missing credentials, and unauthorized projects or capabilities fail closed. Bearer credentials require TLS or another authenticated encrypted transport outside single-host loopback.

The browser uses separate operator routes and, when configured, the human-session boundary above. Marina's service credential cannot create a human session. The complete service-authentication rationale and rotation sequence are in [decision 0003](../docs/decisions/0003-service-authentication.md).

## Agent API

The machine-readable contract is in [api/openapi.yaml](api/openapi.yaml). The core agent loop is:

Protocol compatibility and readiness are available at `GET /api/v1/provider`. Version 1 uses UTC Unix milliseconds for domain timestamps, echoes or creates `X-Request-ID` on every request, and returns stable structured error codes while retaining the original `error` string for compatibility. Capability identifiers let Marina enable only behavior the installed Deck version actually supports. When service authentication is configured, every operation marked with `x-capability` requires a Bearer credential and enforces its capability plus the canonical resource's project scope.

Claims and all `/runs/...` service mutations require `Idempotency-Key`. Deck stores the key, request digest, and original successful result in the same SQLite transaction as the state change. Repeating the same project, service identity, operation, key, resource, and payload returns that result without creating another run, event, decision, artifact, or command acknowledgement; changing the payload returns `409 conflict`. Records survive restart.

Every active-run mutation also requires the stable logical agent ID, exact run version, and current fence token. Claims freeze the task version and acceptance-criteria digest on the run. Lease expiry, cancellation, replan, and authority reduction advance the authority epoch so delayed writes from an older worker fail with `409 conflict`. Claim lookup and assigned-run pagination let a restarted controller resolve an ambiguous claim without issuing a new one.

1. Register the agent.
2. Discover ready tasks.
3. Atomically claim one task with a lease.
4. Send heartbeats while holding the lease.
5. Submit an immutable structured plan revision and optionally request approval.
6. Wait for the revision-bound decision and explicitly start the approved run. Deck preserves plan revisions; the configured Marina repository workflow blocks on requested changes rather than automatically revising.
7. Report durable progress and provider-neutral operational state.
8. Enter verification, record current structured evidence, and complete by referencing its IDs.

Operator commands are durable resources rather than direct lifecycle-column edits. Marina lists them after its persisted cursor and acknowledges each result idempotently. Projected events are deduplicated by producer event ID and execution sequence before entering Deck's visible timeline. Provider state and display states are derived from admitted records, lease timestamps, evidence freshness, and synchronization lag; missing facts remain visibly unknown.

Task creation and channel promotion use the same budget and PR-permission form, with two attempts by default. Tasks declaring `source.*` or `pull_request.*` authority identify repository work: entering or editing Ready requires acceptance criteria, at least two attempts, and explicit `source.read`, `source.write`, and `pull_request.create` permission. Save incomplete work in Backlog. Deck does not infer repository intent from prose; generic work remains compatible, and Marina independently checks every claimed repository task. This admission check does not grant permissions or guarantee cluster/model readiness.

Channel history uses opaque, channel-scoped `cursor` / `next_cursor` pagination in append order (100 messages by default, maximum 200 per page). The browser follows every page and refreshes on message/promotion events and SSE reconnect, deduplicating by message ID. Reopening a channel recovers history even if live notifications were missed.

Channels are conversation, not execution authority. Messages may reference canonical tasks, runs, decisions, artifacts, and evidence, but only an explicit operator promotion with acceptance criteria and a structured budget creates a task. Project export includes ledger records and artifact metadata in stable order while excluding credentials and artifact bytes; import refuses a non-empty target instead of inventing last-write-wins reconciliation.

Example health check:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## Project layout

```text
cmd/deck/              application entry point
internal/domain/          task, run, plan, decision, evidence, channel, operation, event, and artifact model
internal/store/           SQLite repositories and migrations
internal/httpapi/         REST and SSE API
internal/demo/            restart-resumable demo agent
internal/webui/           embedded production frontend
web/                      SvelteKit and TypeScript application
api/openapi.yaml          agent-facing HTTP contract
```

## Prototype boundaries

This release is single-user and single-process. Machine operations authenticate a scoped Marina service identity and use guarded, idempotent, fenced mutations; human routes use a separate optional operator session. Deck records provider-neutral work-control and operational facts but does not execute runtimes, mutate source control, operate deployments, or store artifact bytes. Multi-user permissions, password recovery, federated identity, distributed scheduling, autonomous task decomposition, and a general-purpose agent execution runtime remain outside the prototype.
