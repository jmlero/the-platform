# AppDeck

AppDeck is a lightweight, local-first control plane for human-agent work. It combines a backlog and state-machine board with agent leases, plan approval, live activity, human decisions, and evidence-based completion.

This directory contains the first working prototype. Read the root [AppDeck vision](../VISION_DECK.md) for the product direction and [ARCHITECTURE.md](../ARCHITECTURE.md) for its boundary with Marina.

## What the prototype demonstrates

- Persistent tasks backed by SQLite.
- Backlog refinement and a Kanban-style execution board.
- Atomic task claims with expiring leases.
- A durable distinction between a task and its execution runs.
- Agent registration, heartbeats, plans, progress, and verification.
- Human approval or change requests through a decision inbox.
- Append-only events streamed live to the browser with SSE.
- Verification artifacts attached to completed runs.
- Optional service authentication with stable identities and explicit project and capability grants.
- Durable idempotency records for claims and every run mutation.
- Monotonic run versions and ownership fence tokens required on every service mutation.
- Frozen task-version and acceptance-criteria digests on claimed runs.
- Claim recovery and paginated active-run discovery for stable Marina controllers.
- Durable pause, resume, cancel, replan, retry, and reduce-authority commands with ordered acknowledgements.
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

On a new database AppDeck seeds a small demo workspace. Atlas claims the ready task and submits a plan. Open **Decisions**, approve the plan, and return to **Agent factory** to watch the durable lifecycle progress through execution, verification, and completion.

Runtime data is stored in `./data/appdeck.db` by default.

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
make image    # Build the AppDeck container image
make manifests # Render the Kubernetes deployment base
```

## Container

```bash
docker compose up --build
```

The image runs as an unprivileged user and stores its SQLite database in the `/data` volume.

For an initial Kubernetes deployment, run a single replica with a persistent volume mounted at `/data`. SQLite WAL mode is intentionally a single-host design. A future multi-replica mode should introduce PostgreSQL rather than share the SQLite file across hosts.

The hardened single-replica base is under [deploy/kubernetes/](deploy/kubernetes/). Render it with `make manifests`; use an environment overlay to select an immutable image, storage class, service-authentication Secret, encrypted transport, backup mechanism, and eventual authenticated human ingress.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APPDECK_ADDR` | `:8080` | HTTP listen address |
| `APPDECK_DATA_DIR` | `./data` | Directory containing `appdeck.db` |
| `APPDECK_DEMO_AGENT` | `true` | Seed demo tasks and run Atlas |
| `APPDECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `APPDECK_SERVICE_AUTH_FILE` | unset | Version-1 service-identity policy for authenticated machine API operations |

## Service authentication

Set `APPDECK_SERVICE_AUTH_FILE` before connecting Marina outside the trusted local prototype. The JSON policy maps one or more SHA-256 token fingerprints to a stable identity, explicit AppDeck project IDs, and explicit protocol capabilities:

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
        "run.lease.renew", "run.block", "command.list", "command.ack", "event.append"
      ]
    }
  ]
}
```

Use at least 256 bits of random token material and give the raw value only to Marina through its token-file reference. AppDeck reloads and validates the policy on every request, so two fingerprints can overlap during rotation without changing the identity. Wildcard grants, malformed policies, missing credentials, and unauthorized projects or capabilities fail closed. Bearer credentials require TLS or another authenticated encrypted transport outside single-host loopback.

The browser currently uses separate local operator routes. Those routes do not yet have human sessions and must not be exposed to an untrusted network. The complete rationale and rotation sequence are in [decision 0003](../docs/decisions/0003-service-authentication.md).

## Agent API

The machine-readable contract is in [api/openapi.yaml](api/openapi.yaml). The core agent loop is:

Protocol compatibility and readiness are available at `GET /api/v1/provider`. Version 1 uses UTC Unix milliseconds for domain timestamps, echoes or creates `X-Request-ID` on every request, and returns stable structured error codes while retaining the original `error` string for compatibility. Capability identifiers let Marina enable only behavior the installed AppDeck version actually supports. When service authentication is configured, every operation marked with `x-capability` requires a Bearer credential and enforces its capability plus the canonical resource's project scope.

Claims and all `/runs/...` service mutations require `Idempotency-Key`. AppDeck stores the key, request digest, and original successful result in the same SQLite transaction as the state change. Repeating the same project, service identity, operation, key, resource, and payload returns that result without creating another run, event, decision, artifact, or command acknowledgement; changing the payload returns `409 conflict`. Records survive restart.

Every active-run mutation also requires the stable logical agent ID, exact run version, and current fence token. Claims freeze the task version and acceptance-criteria digest on the run. Lease expiry, cancellation, replan, and authority reduction advance the authority epoch so delayed writes from an older worker fail with `409 conflict`. Claim lookup and assigned-run pagination let a restarted controller resolve an ambiguous claim without issuing a new one.

1. Register the agent.
2. Discover ready tasks.
3. Atomically claim one task with a lease.
4. Send heartbeats while holding the lease.
5. Submit a plan and optionally request approval.
6. Wait for the decision and explicitly start the approved run.
7. Report durable progress events.
8. Enter verification and complete with evidence.

Operator commands are durable resources rather than direct lifecycle-column edits. Marina lists them after its persisted cursor and acknowledges each result idempotently. Projected events are deduplicated by producer event ID and execution sequence before entering AppDeck's visible timeline.

Example health check:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## Project layout

```text
cmd/appdeck/              application entry point
internal/domain/          task, run, decision, event, and artifact model
internal/store/           SQLite repositories and migrations
internal/httpapi/         REST and SSE API
internal/demo/            restart-resumable demo agent
internal/webui/           embedded production frontend
web/                      SvelteKit and TypeScript application
api/openapi.yaml          agent-facing HTTP contract
```

## Prototype boundaries

This release is single-user and single-process. Machine operations authenticate a scoped Marina service identity and use guarded, idempotent, fenced mutations; the human operator login session is not implemented yet. Do not expose the local operator routes directly to an untrusted network. Multi-user permissions, distributed scheduling, autonomous task decomposition, and a general-purpose agent execution runtime remain outside the prototype.
