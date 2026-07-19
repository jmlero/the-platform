# AppDeck

AppDeck is a lightweight, local-first control plane for human-agent work. It combines a backlog and state-machine board with agent leases, plan approval, live activity, human decisions, and evidence-based completion.

This directory contains the first working prototype. Read [VISION.md](VISION.md) for the product direction and the root [ARCHITECTURE.md](../ARCHITECTURE.md) for its boundary with Marina.

## What the prototype demonstrates

- Persistent tasks backed by SQLite.
- Backlog refinement and a Kanban-style execution board.
- Atomic task claims with expiring leases.
- A durable distinction between a task and its execution runs.
- Agent registration, heartbeats, plans, progress, and verification.
- Human approval or change requests through a decision inbox.
- Append-only events streamed live to the browser with SSE.
- Verification artifacts attached to completed runs.
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
```

## Container

```bash
docker compose up --build
```

The image runs as an unprivileged user and stores its SQLite database in the `/data` volume.

For an initial Kubernetes deployment, run a single replica with a persistent volume mounted at `/data`. SQLite WAL mode is intentionally a single-host design. A future multi-replica mode should introduce PostgreSQL rather than share the SQLite file across hosts.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `APPDECK_ADDR` | `:8080` | HTTP listen address |
| `APPDECK_DATA_DIR` | `./data` | Directory containing `appdeck.db` |
| `APPDECK_DEMO_AGENT` | `true` | Seed demo tasks and run Atlas |
| `APPDECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

## Agent API

The machine-readable contract is in [api/openapi.yaml](api/openapi.yaml). The core agent loop is:

Protocol compatibility and readiness are available at `GET /api/v1/provider`. Version 1 uses UTC Unix milliseconds for domain timestamps, echoes or creates `X-Request-ID` on every request, and returns stable structured error codes while retaining the original `error` string for compatibility. Capability identifiers let Marina enable only behavior the installed AppDeck version actually supports.

1. Register the agent.
2. Discover ready tasks.
3. Atomically claim one task with a lease.
4. Send heartbeats while holding the lease.
5. Submit a plan and optionally request approval.
6. Wait for the decision and explicitly start the approved run.
7. Report durable progress events.
8. Enter verification and complete with evidence.

Example health check:

```bash
curl http://localhost:8080/healthz
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

This release is single-user, single-process, and intentionally has no authentication. Do not expose it directly to an untrusted network. Multi-user permissions, distributed scheduling, autonomous task decomposition, and a general-purpose agent execution runtime remain outside the prototype.
