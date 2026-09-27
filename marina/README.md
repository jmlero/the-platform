# The Marina

Marina runs bounded repository fixes on Kubernetes infrastructure controlled by
its operator. Its first workflow produces a verified pull request for human
review, with durable progress, explicit data destinations and limited authority.
The broader software-factory vision remains the long-term direction.

This project starts from scratch as a new Kubernetes-based concept. Experience from the earlier AWS-native `platform-marina` informs the creator's technical judgment, but Marina inherits neither its architecture nor its code and is not a port or rewrite of it.

It is single-operator first. Deck over HTTP/JSON is the supported work-control provider. Headless and in-process alternatives remain future packaging; work-control surfaces, agent harnesses, runtimes, context sources, policy and verification systems, delivery integrations, and platform services remain replaceable behind governed contracts.

Read the [vision](../docs/VISION.md) for the product direction and the dark-factory goal, and [status.md](../docs/status.md) for what is supported today.

Read [the root architecture](../docs/ARCHITECTURE.md) for the joint Deck–Marina component boundaries, source-of-truth rules, lifecycle protocol, failure semantics, and delivery gates.

## Current implementation

The [KINT-03 kagent adapter](internal/kagent/README.md) implements the pinned
engine lifecycle and durable correlation at the runtime boundary, with
[isolated local recovery evidence](../docs/validation/kint-03-2026-09-22.md).
`serve` can select it only through an explicit pinned repository-engine profile. The
[KINT-04 gateway](internal/workerapi/README.md) adds governed admission and
attempt-scoped access, with real local native Codex/Substrate HTTPS injection,
revocation, rotation and bypass acceptance. Installed workload migration remains
pending; the live gateway fixture used synthetic Deck and model responses.
The [KINT-05 result boundary](../docs/validation/kint-05-2026-09-23.md)
validates legacy or collected kagent output against frozen attempt inputs and
retains the agent-free clean verifier.
The [KINT-06 local pilot](../docs/validation/kint-06-2026-09-23.md) selected
native Codex for one Deck-authorized repository task and reached a verified PR.
The trial profile was removed afterward; regular intake uses the original image.

An optional pinned conversation profile now consumes Deck's ordered explicit
agent invocation feed through a read-only kagent BYO actor. It freezes the selected thread
context, permits one governed model call, and retains a bounded result artifact.
Marina creates and removes one attempt-exclusive engine template per turn. Engine questions
are written to the originating Deck thread, and one answer is sent to the same
waiting attempt. The service identity needs `conversation.question.write` in
addition to invocation and channel read grants. Terminal turns publish one
canonical Deck reply and optional bounded task; the exact publication survives
lost responses and restart. Ready delegation needs a separately configured
repository budget and `conversation.task.delegate` grant. Replies and drafts
need `conversation.outcome.publish`. Local engine/model and browser acceptance
remain pending; see
[CHAT-04 evidence](../docs/validation/chat-04-2026-09-23.md) and
[CHAT-05 evidence](../docs/validation/chat-05-2026-09-23.md), and
[CHAT-06–08 evidence](../docs/validation/chat-06-08-2026-09-25.md).

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
- Exact-revision worker Git checkout, immutable provenance-rich context packages, and typed versioned policy snapshots with bounded authority and budget evaluation.
- Structured Deck plan-revision submission and restart-safe approval/change-request reconciliation through durable workflow checkpoints.
- Guarded source-control publication and deterministic verification through configured checks, clean workers, and source-bound evidence.
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
fixed bounded repository rules and explicit immutable context; broader policy/plugin foundations
remain available for later workflows.

An optional version-1 `analysis` profile enables the bounded diagnostic PoC:
one pinned LangChain `create_agent` worker consumes frozen synthetic records
without a repository checkout and produces a structured report. The existing
controller, journal, budgets, gateway and Kubernetes isolation are shared;
an independent Go validator checks findings and source quotes and binds evidence
to the input/profile/output digests. See the
[profile contract](docs/workloads/profiles-v1.md) and
[live proof for both harnesses](../docs/validation/agent-02-06-2026-09-21.md).
`marina workload-profile --config workflow.json` prints the installed analysis
identity and exact task authority grant without reading credentials. The
[operator API guide](docs/workloads/api-v1.md) covers installed-profile discovery,
pinned invocation and result retrieval. Harness v2 adds selected immutable
context and one governed `read_context_source` HTTP tool. Shared progress includes
confirmed usage and unresolved reservations; redacted diagnostics correlate
attempts, model/tool calls and artifacts. See the
[validation boundary](../docs/validation/agent-07-10-2026-09-21.md).
The [two-workload installation guide](../deploy/kubernetes/agent-platform/README.md)
and [combined MVP proof](../docs/validation/agent-11-13-2026-09-22.md) cover the
deployed v2 harness, shared failure checks, preserved stores and live results.
Interrupted worker I/O may still block; the harness does not checkpoint agent steps.
Analysis-only configurations need no Git repository or delivery binding.

Without a workflow configuration, the existing Milestone 1 control-only mode
remains available. Use the [paired installation guide](../deploy/kubernetes/first-release/README.md)
for an executable configuration, image build, private credential preparation and
acceptance procedure. Local and real-process tests pass with deterministic
provider doubles; a real model/cluster acceptance run remains installation-specific.

For installation before a repository/model is selected, run `serve --intake-enabled=false`.
This skips new work discovery while continuing recovery, commands and projection
for already journaled work. `--intake-task-id <id>` limits enabled discovery to
one task while preserving the same recovery behavior. Neither option pauses
active executions. Server TLS can
be configured independently of the workflow using `--tls-cert-file` and
`--tls-key-file` (or `MARINA_TLS_CERT_FILE` / `MARINA_TLS_KEY_FILE`). Conflicting
workflow and flag certificate paths, or an unreadable key pair, fail startup
before work-control calls. See the [local Podman profile](../deploy/kubernetes/local-podman/README.md).

## Development

Requirements:

- Go 1.27+ (required by the pinned kagent API module)

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

After draining work and stopping Marina, `marina artifact-retention --journal-path
/data/marina.db --artifact-directory /data/artifacts` reports expired unreferenced
artifacts without deleting them. Apply only a reviewed report with `--apply-plan`.
All historical journal references remain protected; seven days and 100 files are
the default age and batch size. See the [operations runbook](../deploy/kubernetes/first-release/OPERATIONS.md)
for the maintenance lock, Kubernetes procedure and recovery cases.

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
| `MARINA_CONFIG_FILE` | unset | Explicit workload configuration; enables repository and/or diagnostic worker execution. |
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
../docs/ARCHITECTURE.md     Joint Deck–Marina architecture
internal/workcontrol/      Provider-neutral work-control contract
internal/deck/          Deck adapter
internal/execution/        Durable execution model and recovery decisions
internal/httpserver/       Marina liveness and readiness HTTP surface
internal/journal/sqlite/   Transactional execution journal and migrations
internal/runtime/          Provider-neutral runtime lifecycle and recovery semantics
internal/contextpack/      Immutable context manifests and trust boundaries
internal/policy/           Versioned authority and budget evaluator
internal/sourcecontrol/    Guarded provider-neutral source-control port
internal/signals/          Validated observations and diagnostic-work boundary
internal/reconcile/        Restart planning, ownership checks, and recovery control
deploy/kubernetes/         Single-replica always-on deployment baseline
docs/decisions/            Reversible architecture decisions
```

Read the root [AGENTS.md](../AGENTS.md) and Marina [AGENTS.md](AGENTS.md) before making changes, [ARCHITECTURE.md](../docs/ARCHITECTURE.md) for the target system boundaries, and [the backlog](../docs/backlog.md) for the prioritized build sequence. The initial language choice and its reversal criteria are recorded in [Marina decision 0001](docs/decisions/0001-initial-go-core.md); the single-replica SQLite decision and its PostgreSQL migration triggers are in [Marina decision 0002](docs/decisions/0002-sqlite-execution-journal.md); the initial typed policy representation and local provider-neutral foundations are in [Marina decision 0003](docs/decisions/0003-initial-policy-and-local-foundations.md); the cross-product service identity and rotation model is in [platform decision 0003](../docs/decisions/0003-service-authentication.md); and the lossless handoff, fencing, command, and outbox mechanics are in [platform decision 0004](../docs/decisions/0004-milestone-one-work-control-safety.md).

## Explicit project context

The repository workflow assembles a content-addressed package before plan
submission: the authorized task and budget, installed limits, pinned repository,
worker image, selected Deck/Git sources and explicit conversation excerpts.
It persists the package before worker dispatch and recovers those exact bytes on
restart. Plans bind the package digest; both implementation and verification
worker inputs include it. Deck receives a bounded derived snapshot for inspection.

New source revisions affect newly authorized tasks. Explicit revocation and
expired included inputs deny further worker-gateway access and stop the workflow
for clarification, waiting for worker termination before reporting a blocker.
Expired reference data and known instruction overrides can be omitted at assembly;
stale guidance and declared conflicts block. Conversation stays untrusted data.
These checks do not claim general semantic conflict or prompt-injection detection;
existing filesystem, network, source-publication, authority and budget gates apply.

Marina reconciles Deck repository requests even with new task intake disabled.
The request is an owner/repository name plus branch, never an arbitrary endpoint
or credential. Validation uses the installed GitHub API/clone origin and private
credential reference. Versioned repository history survives restart and active
runs resolve their original binding. An installation-profile change requires
review and newly authorized work; it cannot silently alter old execution policy.
Grant the project-scoped service identity `context.read`, `repository.read`, and
`repository.write` when upgrading an existing installation; the setup generator
includes them for new installations. See [the context proof](../docs/validation/ctx-02-08-2026-09-21.md).
