# The Marina

The Marina is the baseline for a platform-agnostic, always-on autonomous software engineering factory: durable agents working across the SDLC and DevOps lifecycle while humans steer intent, architecture, policy, and risk.

This project starts from scratch as a new Kubernetes-based concept. Experience from the earlier AWS-native `platform-marina` informs the creator's technical judgment, but Marina inherits neither its architecture nor its code and is not a port or rewrite of it.

It is single-operator first. AppDeck is its default pluggable human-steering component, while agent harnesses and execution platforms remain replaceable behind stable contracts.

Read [VISION.md](VISION.md) for the product vision, its relationship with AppDeck, the experience brought from `platform-marina`, the Kubernetes-first platform hypothesis, and the questions still to resolve.

Read [DARK_FACTORY.md](DARK_FACTORY.md) for the precise meaning of a dark software factory in Marina.

Read [the root architecture](../ARCHITECTURE.md) for the joint AppDeck–Marina component boundaries, source-of-truth rules, lifecycle protocol, failure semantics, and delivery gates.

## Current implementation

The first executable foundation is now present:

- A dependency-light Go control-service binary.
- A narrow, provider-neutral work-control port.
- An AppDeck adapter for protocol discovery, capability validation, stable error correlation, agent registration, ready-work discovery, atomic claims, existing-run inspection, and heartbeats.
- A durable SQLite execution journal with version-guarded lifecycle transitions, checkpoints, evidence, leases, budgets, and append-only events.
- Idempotency constraints that prevent one external run, checkpoint key, or evidence key from creating duplicate internal work.
- An evidence gate that prevents an execution from completing without source-bound verification evidence.
- A side-effect-free restart planner that reconstructs the next safe recovery action from persisted state.
- Startup reconciliation that verifies the exact AppDeck task, run, and agent before confirming or renewing a provider lease; it never creates a replacement claim.
- A continuous, non-overlapping recovery controller that protects only journaled work, converges provider terminal states, and blocks budget-exhausted runs instead of renewing them.
- A cross-package restart proof showing inspect → heartbeat → inspect against the original run, with no replacement claim call.
- Truthful `/healthz` liveness and `/readyz` readiness for both AppDeck and the journal.
- A `doctor` command for checking AppDeck connectivity.
- Deterministic tests that do not require external services or local network listeners.

On service startup, Marina validates AppDeck's protocol and the capabilities required by its current reconciliation controllers, opens the journal, reconstructs recovery intents for unfinished executions, and reconciles ownership and leases for non-expired budgets. Missing capabilities are reported precisely and prevent dependent lease or budget mutations without hiding read-only provider information. The same bounded reconciliation runs periodically while the service is alive. Failed, blocked, and cancelled provider runs converge into guarded local terminal states; budget exhaustion blocks the exact provider run and is classified durably. Provider completion still cannot bypass Marina's verification and evidence gates.

This increment deliberately does **not** resume a runtime or create replacement work. It does not run agents, claim tasks automatically, drive retry loops, publish journal events as AppDeck progress, or create pull requests. Those behaviors remain unchecked in the [root TODO](../TODO.md). Marina will not claim real work until acquisition is idempotent, run authority is fenced, failure handling is bounded, and progress publication is durable.

## Development

Requirements:

- Go 1.26+

Useful commands:

```bash
make fmt
make check
make build
```

Build output is `bin/marina`.

Build the non-root container image:

```bash
make image IMAGE=marina:dev VERSION=dev
```

Check an AppDeck instance:

```bash
./bin/marina doctor --appdeck-url http://localhost:8080
```

This release supports AppDeck work-control protocol `>=1.0,<2.0`. Minor-version additions remain capability-gated; an unsupported major version or a missing reconciliation capability keeps Marina not ready and prevents dependent external actions.

Run Marina's initial HTTP service:

```bash
MARINA_APPDECK_URL=http://localhost:8080 \
MARINA_JOURNAL_PATH=./data/marina.db \
./bin/marina serve
```

The service listens on `:8090` by default. Liveness is available at `/healthz`; readiness at `/readyz` is successful only when the configured work-control provider and execution journal are reachable. The journal directory is created automatically, but the file must live on durable single-writer storage in Kubernetes.

## Kubernetes baseline

The initial Kubernetes base is under [`deploy/kubernetes`](deploy/kubernetes/README.md). It runs one non-root control-service replica with `Recreate` updates and a `ReadWriteOnce` persistent volume, matching the accepted SQLite boundary. It does not deploy an agent harness or agent Job yet.

Render it locally:

```bash
make manifests
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `MARINA_ADDR` | `:8090` | Marina HTTP listen address. |
| `MARINA_APPDECK_URL` | `http://localhost:8080` | AppDeck server root URL. |
| `MARINA_JOURNAL_PATH` | `data/marina.db` | Durable SQLite execution journal. |
| `MARINA_LEASE_RENEW_BEFORE` | `10s` | Window in which reconciliation renews an external lease. |
| `MARINA_RUN_LEASE` | `30s` | AppDeck lease duration requested by each heartbeat. |
| `MARINA_RECONCILE_INTERVAL` | `10s` | Interval between non-overlapping lease reconciliation cycles. |

## Project layout

```text
cmd/marina/                 CLI and service entry point
../ARCHITECTURE.md          Joint AppDeck–Marina target architecture
internal/workcontrol/      Provider-neutral work-control contract
internal/appdeck/          AppDeck adapter
internal/execution/        Durable execution model and recovery decisions
internal/httpserver/       Marina liveness and readiness HTTP surface
internal/journal/sqlite/   Transactional execution journal and migrations
internal/reconcile/        Restart planning, ownership checks, and recovery control
deploy/kubernetes/         Single-replica always-on deployment baseline
docs/decisions/            Reversible architecture decisions
```

Read the root [AGENTS.md](../AGENTS.md) and Marina [AGENTS.md](AGENTS.md) before making changes, [ARCHITECTURE.md](../ARCHITECTURE.md) for the target system boundaries, and [TODO.md](../TODO.md) for the prioritized build sequence. The initial language choice and its reversal criteria are recorded in [decision 0001](docs/decisions/0001-initial-go-core.md); the single-replica SQLite decision and its PostgreSQL migration triggers are in [decision 0002](docs/decisions/0002-sqlite-execution-journal.md).
