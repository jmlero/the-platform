# The Marina

Marina runs bounded repository fixes on Kubernetes infrastructure controlled by
its operator. Its first workflow produces a verified pull request for human
review, with durable progress, explicit data destinations and limited authority.
The broader software-factory vision remains the long-term direction.

This project starts from scratch as a new Kubernetes-based concept. Experience from the earlier AWS-native `platform-marina` informs the creator's technical judgment, but Marina inherits neither its architecture nor its code and is not a port or rewrite of it.

It is single-operator first. Deck over HTTP/JSON is the supported work-control provider. Headless and in-process alternatives remain future packaging; work-control surfaces, agent harnesses, runtimes, context sources, policy and verification systems, delivery integrations, and platform services remain replaceable behind governed contracts.

Read the root [Marina vision](../VISION.md) for the product vision, its relationship with Deck, the experience brought from `platform-marina`, the Kubernetes-native platform and extensibility thesis, and the questions still to resolve.

Read [DARK_FACTORY.md](../DARK_FACTORY.md) for the precise meaning of a dark software factory in Marina.

Read [the root architecture](../ARCHITECTURE.md) for the joint Deck–Marina component boundaries, source-of-truth rules, lifecycle protocol, failure semantics, and delivery gates.

## Current implementation

The following implementations and reusable foundations are present; the configured repository workflow below uses a narrower subset:

- A dependency-light Go control-service binary.
- A narrow, provider-neutral work-control port.
- A Deck adapter for protocol discovery, capability validation, stable error correlation, scoped service authentication, stable registration, claim recovery, assigned-run discovery, guarded run control, commands, and projected events.
- A durable SQLite execution journal with stable installation identity, versioned project bindings, acquisition intents, version-guarded lifecycle transitions, checkpoints, evidence, leases, budgets, retry state, command cursors, an ordered provider outbox, and append-only events.
- Idempotency constraints that prevent one external run, checkpoint key, or evidence key from creating duplicate internal work.
- An evidence gate that prevents an execution from completing without source-bound verification evidence.
- A side-effect-free restart planner that reconstructs the next safe recovery action from persisted state.
- Startup reconciliation that verifies the exact Deck task, run, and agent before confirming or renewing a provider lease; it never creates a replacement claim.
- A continuous, non-overlapping recovery controller that protects only journaled work, converges provider terminal states, and blocks budget-exhausted runs instead of renewing them.
- A cross-package restart proof showing inspect → heartbeat → inspect against the original run, with no replacement claim call.
- Truthful `/healthz` liveness and `/readyz` readiness for both Deck and the journal.
- A `doctor` command for checking Deck connectivity.
- File-referenced Deck credentials that reload per request for rotation and are never written to the execution journal.
- Stable keys for reconciliation-driven lease renewal and budget stopping, plus explicit keys on acquisition, planning, and progress requests.
- Deterministic product tests plus an authenticated real-process Deck conformance path.
- A bounded work-intake controller that journals intent before claim, resolves ambiguous responses without blind reacquisition, validates the frozen task snapshot, and binds one exact run before any later side effect.
- Ordered command consumption in which cancellation and authority reduction fence ordinary progress before acknowledgement.
- Time-bounded external calls, persisted classified backoff and exhaustion, non-overlapping cycles, lease safety windows, and durable normalized progress/failure projection.
- A provider-neutral runtime lifecycle and immutable specification, with durable attempt, usage, artifact, and cleanup records plus explicit ambiguous-start and orphan recovery decisions.
- Exact-revision local Git materialization, immutable provenance-rich context packages, and typed versioned policy snapshots with bounded authority and budget evaluation.
- Structured Deck plan-revision submission and restart-safe approval/change-request reconciliation through durable checkpoints.
- Narrow source-control and deterministic verification contracts, including repository-owned check manifests, evidence invalidation, and risk-based independent-review requirements.
- Validated, correlated, classified, and deduplicated operational signals whose raw text remains untrusted and grants no mutation authority.
- An offline `diagnostics` command that exports a deterministic, conservatively redacted journal view without exposing a new work-control API.

On service startup, Marina validates Deck's Milestone 1 capabilities, opens the journal, reloads its stable identity and project binding, registers the logical controller, reconciles unresolved acquisitions, and then protects exact run ownership and leases. The same non-overlapping control and lease cycles run periodically. Failed, blocked, and cancelled provider runs converge into guarded local terminal states; budget exhaustion blocks the exact provider run and is classified durably. Provider completion still cannot bypass Marina's verification and evidence gates.

With `--config workflow.json` (or `MARINA_CONFIG_FILE`), the service enables the
repository workflow: frozen task budget/authority, immutable plan, optional
approval, Kubernetes coding Job, scoped Git publication, independent verification
Job, PR/CI observation and structured evidence completion. It supports one active
task, one implementation and one verification attempt. Failed verification or
requested changes block the task. Cancellation waits for observed termination. Pause, resume, replan, retry and
reduce-authority commands are rejected in this workflow; ledger support for
those command types is not an execution capability. The separate verification
Job runs deterministic checks, not independent semantic review. Completion means
a PR ready for human review; merge and deployment remain human-controlled.

The worker uses a configured Codex CLI image and an explicit Responses-compatible
model endpoint. GitHub/GHES is the first source adapter. Workers receive neither
source-write nor upstream-model credentials. The gateway checks live authority;
artifacts survive worker cleanup on Marina-owned storage. This workflow uses
fixed bounded repository rules; the broader policy/context/plugin foundations
remain available for later workflows.

Without a workflow configuration, the existing Milestone 1 control-only mode
remains available. Use the [paired installation guide](../deploy/kubernetes/first-release/README.md)
for an executable configuration, image build, private credential preparation and
acceptance procedure. Local and real-process tests pass with deterministic
provider doubles; a real model/cluster acceptance run remains installation-specific.

For installation before a repository/model is selected, run `serve --intake-enabled=false`.
This skips new work discovery while continuing recovery, commands and projection
for already journaled work. It does not pause active executions. Server TLS can
be configured independently of the workflow using `--tls-cert-file` and
`--tls-key-file` (or `MARINA_TLS_CERT_FILE` / `MARINA_TLS_KEY_FILE`). Conflicting
workflow and flag certificate paths, or an unreadable key pair, fail startup
before work-control calls. See the [local Podman profile](../deploy/kubernetes/local-podman/README.md).

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

Check a Deck instance:

```bash
./bin/marina doctor --deck-url http://localhost:8080
```

Inspect the local journal without serving internal state as an API:

```bash
./bin/marina diagnostics --journal-path ./data/marina.db > marina-diagnostics.json
```

The export omits raw signal text and free-form journal messages, redacts arbitrary payload strings, sanitizes URLs, and fingerprints provider handles. It contains operational metadata only; Deck remains the authoritative human-facing work-control ledger.

For an authenticated Deck service API, set `MARINA_DECK_TOKEN_FILE` to the file containing the raw opaque token. Marina reads the file for every request so a projected or atomically replaced secret rotates without changing the stable installation, logical agent, or run identity. Do not put the token in an environment variable, command argument, URL, or repository file.

Mutating work-control requests carry `Idempotency-Key`. The intake controller persists its claim key before calling Deck; lease, budget, command, and projection operations derive repeatable keys from durable journal identity. Direct adapter callers that omit a key receive a one-shot compatibility key, which is unsuitable for ambiguous-response recovery.

This release supports Deck work-control protocol `>=1.0,<2.0`. Minor-version additions remain capability-gated; an unsupported major version or a missing reconciliation capability keeps Marina not ready and prevents dependent external actions.

Run Marina's initial HTTP service:

```bash
MARINA_DECK_URL=http://localhost:8080 \
MARINA_DECK_TOKEN_FILE=/run/secrets/deck/token \
MARINA_JOURNAL_PATH=./data/marina.db \
./bin/marina serve
```

The service listens on `:8090` by default. Liveness is available at `/healthz`; readiness at `/readyz` is successful only when the configured work-control provider and execution journal are reachable. The journal directory is created automatically, but the file must live on durable single-writer storage in Kubernetes.

## Kubernetes baseline

The control-only base is under [`deploy/kubernetes`](deploy/kubernetes/README.md).
The [first-release overlay](../deploy/kubernetes/first-release/README.md) adds
paired TLS services, separate identities/storage, worker RBAC and network policy,
artifact access and configured Job execution. Both use single-replica Recreate
updates and ReadWriteOnce SQLite storage.

Render it locally:

```bash
make manifests
```

| Variable | Default | Purpose |
| --- | --- | --- |
| `MARINA_ADDR` | `:8090` | Marina HTTP listen address. |
| `MARINA_CONFIG_FILE` | unset | Explicit repository workflow configuration; enables worker execution. |
| `MARINA_DECK_URL` | `http://localhost:8080` | Deck server root URL. |
| `MARINA_DECK_TOKEN_FILE` | unset | File reference containing the Deck service Bearer token. |
| `MARINA_DECK_PROJECT_ID` | `prj_default` | Deck project bound to this initial controller. |
| `MARINA_JOURNAL_PATH` | `data/marina.db` | Durable SQLite execution journal. |
| `MARINA_REPOSITORY` | `disabled://milestone-one` | Repository binding; deliberately non-mutating in Milestone 1. |
| `MARINA_POLICY_VERSION` | `milestone-one` | Version persisted with the project binding and executions. |
| `MARINA_LEASE_RENEW_BEFORE` | `10s` | Window in which reconciliation renews an external lease. |
| `MARINA_RUN_LEASE` | `30s` | Deck lease duration requested by each heartbeat. |
| `MARINA_RECONCILE_INTERVAL` | `10s` | Interval between non-overlapping lease reconciliation cycles. |

## Project layout

```text
cmd/marina/                 CLI and service entry point
../ARCHITECTURE.md          Joint Deck–Marina target architecture
internal/workcontrol/      Provider-neutral work-control contract
internal/deck/          Deck adapter
internal/execution/        Durable execution model and recovery decisions
internal/httpserver/       Marina liveness and readiness HTTP surface
internal/journal/sqlite/   Transactional execution journal and migrations
internal/planning/         Versioned plan and decision reconciliation
internal/runtime/          Provider-neutral runtime lifecycle and recovery semantics
internal/contextpack/      Immutable context manifests and trust boundaries
internal/policy/           Versioned authority and budget evaluator
internal/workspace/        Exact-revision local materialization
internal/verification/     Repository checks and source-bound evidence evaluation
internal/sourcecontrol/    Guarded provider-neutral source-control port
internal/signals/          Validated observations and diagnostic-work boundary
internal/reconcile/        Restart planning, ownership checks, and recovery control
deploy/kubernetes/         Single-replica always-on deployment baseline
docs/decisions/            Reversible architecture decisions
```

Read the root [AGENTS.md](../AGENTS.md) and Marina [AGENTS.md](AGENTS.md) before making changes, [ARCHITECTURE.md](../ARCHITECTURE.md) for the target system boundaries, and [TODO.md](../TODO.md) for the prioritized build sequence. The initial language choice and its reversal criteria are recorded in [Marina decision 0001](docs/decisions/0001-initial-go-core.md); the single-replica SQLite decision and its PostgreSQL migration triggers are in [Marina decision 0002](docs/decisions/0002-sqlite-execution-journal.md); the initial typed policy representation and local provider-neutral foundations are in [Marina decision 0003](docs/decisions/0003-initial-policy-and-local-foundations.md); the cross-product service identity and rotation model is in [platform decision 0003](../docs/decisions/0003-service-authentication.md); and the lossless handoff, fencing, command, and outbox mechanics are in [platform decision 0004](../docs/decisions/0004-milestone-one-work-control-safety.md).
