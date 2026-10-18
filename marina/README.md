# The Marina

Marina runs bounded repository changes on Kubernetes infrastructure controlled
by its operator. Its workflow lands a verified commit on the base branch, or stops
at a focused blocker, with durable progress, explicit data destinations and
limited authority.
The broader software-factory vision remains the long-term direction.

This project starts from scratch as a new Kubernetes-based concept. Experience from the earlier AWS-native `platform-marina` informs the creator's technical judgment, but Marina inherits neither its architecture nor its code and is not a port or rewrite of it.

It is single-operator first. Marina owns its platform API, `marina.dev/v1`, with two surfaces: HTTP/JSON and Kubernetes resources ([decision 0033](../docs/decisions/0033-marina-platform-api.md), [contract](api/README.md)). Deck, `platformctl`, `kubectl` and factories are clients; Marina calls none of them. Work-control surfaces, agent harnesses, runtimes, context sources, policy and verification systems, delivery integrations, and platform services remain replaceable behind governed contracts.

Read the [vision](../docs/VISION.md) for the product direction and the dark-factory goal, and [status.md](../docs/status.md) for what is supported today.

Read [the root architecture](../docs/ARCHITECTURE.md) for the joint Deck–Marina component boundaries, source-of-truth rules, lifecycle protocol, failure semantics, and delivery gates.

## Current implementation

The [KINT-03 kagent adapter](internal/kagent/README.md) implements the pinned
engine lifecycle and durable correlation at the runtime boundary, with
[isolated local recovery evidence](../docs/validation/kint-03-2026-09-22.md).
A `repository_engine` profile selects native Codex on kagent for implementation;
Marina creates each attempt's own template ([decision 0024](../docs/decisions/0024-per-attempt-engine-templates.md))
and follows committed engine state with bounded observation recovery. The
[KINT-04 gateway](internal/workerapi/README.md) adds governed admission and
attempt-scoped access for engine model, Git read and context calls.
The [KINT-05 result boundary](../docs/validation/kint-05-2026-09-23.md)
validates collected kagent output against frozen attempt inputs and
retains the agent-free clean verifier.
The local installation runs repository work on native Codex; see
[status](../docs/status.md) for what is accepted.

An optional pinned conversation profile runs agent turns through a read-only
kagent BYO actor. A client creates a `Turn` with the frozen thread and selected
project sources. Before dispatch, Marina freezes the thread, exact repository
commit, complete regular-file index and selected context in retained artifacts.
The model gateway offers only bounded file listing and reading at that commit;
repository mutation and arbitrary tools remain unavailable. Model exchanges and
the one clarification follow-up share the original turn's token limit and
deadline. See [decision 0027](../docs/decisions/0027-read-only-repository-context-for-chat.md)
and the [conversation profile](docs/workloads/profiles-v1.md#conversation-selection)
for the input, trust and read limits.
Marina creates and removes one attempt-exclusive engine template per turn. An
engine question becomes a `Decision`; one answer is sent to the same waiting
attempt. A terminal turn publishes one reply and optional task or PRD proposals
in the Turn status; the exact publication survives lost responses and restart.
Acceptance and current limits are recorded in [status](../docs/status.md); see
[CHAT-04 evidence](../docs/validation/chat-04-2026-09-23.md),
[CHAT-05 evidence](../docs/validation/chat-05-2026-09-23.md) and
[CHAT-06–08 evidence](../docs/validation/chat-06-08-2026-09-25.md).

The following implementations and reusable foundations are present; the configured repository workflow below uses a narrower subset:

- A dependency-light Go control-service binary.
- A narrow, provider-neutral work-control port, implemented in process by the platform ledger (`internal/journal/sqlite`, migration 016).
- The platform API (`internal/platformapi`): HTTP/JSON server with Kubernetes TokenReview and SubjectAccessReview, and a controller that ingests `marina.dev` resources into the ledger and mirrors status back.
- A durable SQLite execution journal with stable installation identity, versioned project bindings, acquisition intents, version-guarded lifecycle transitions, checkpoints, evidence, leases, budgets, retry state, command cursors, an ordered provider outbox, and append-only events.
- Idempotency constraints that prevent one external run, checkpoint key, or evidence key from creating duplicate internal work.
- An evidence gate that prevents an execution from completing without source-bound verification evidence.
- A side-effect-free restart planner that reconstructs the next safe recovery action from persisted state.
- Startup reconciliation that verifies the exact run and agent before confirming or renewing a lease; it never creates a replacement claim.
- A continuous, non-overlapping recovery controller that protects only journaled work, converges provider terminal states, and blocks budget-exhausted runs instead of renewing them.
- A cross-package restart proof showing inspect → heartbeat → inspect against the original run, with no replacement claim call.
- Truthful `/healthz` liveness and `/readyz` readiness for the journal and platform ledger.
- A `doctor` command for checking the platform ledger.
- Stable keys for reconciliation-driven lease renewal and budget stopping, plus explicit keys on acquisition, planning, and progress requests.
- Deterministic product tests, and an API sandbox build (`go build -tags sandbox`, command `api-sandbox`) that clients run their contract tests against.
- A bounded work-intake controller that journals intent before claim, resolves ambiguous responses without blind reacquisition, validates the frozen task snapshot, and binds one exact run before any later side effect.
- Ordered command consumption in which cancellation and authority reduction fence ordinary progress before acknowledgement.
- Time-bounded external calls, persisted classified backoff and exhaustion, non-overlapping cycles, lease safety windows, and durable normalized progress/failure projection.
- A provider-neutral runtime lifecycle and immutable specification, with durable attempt, usage, artifact, and cleanup records plus explicit ambiguous-start and orphan recovery decisions.
- Exact-revision worker Git checkout, immutable provenance-rich context packages, and typed versioned policy snapshots with bounded authority and budget evaluation.
- Structured plan-revision submission and restart-safe approval/change-request reconciliation through durable workflow checkpoints.
- Guarded source-control publication and deterministic verification through configured checks, clean workers, and source-bound evidence.
- Validated, correlated, classified, and deduplicated operational signals whose raw text remains untrusted and grants no mutation authority.
- An offline `diagnostics` command that exports a deterministic, conservatively redacted journal view without exposing a new work-control API.

On service startup, Marina opens the journal, reloads its stable identity and project binding, registers the logical controller, reconciles unresolved acquisitions, and then protects exact run ownership and leases. Each cycle also reconciles the `marina.dev` resources. The same non-overlapping control and lease cycles run periodically. Failed, blocked, and cancelled provider runs converge into guarded local terminal states; budget exhaustion blocks the exact provider run and is classified durably. Provider completion still cannot bypass Marina's verification and evidence gates.

With `--config workflow.json` (or `MARINA_CONFIG_FILE`), the service enables the
repository workflow: frozen task budget/authority, immutable plan, optional
approval, native Codex on kagent, a different model in a read-only kagent review,
scoped Git publication, an independent verification Job,
required CI on the exact commit and structured evidence completion. With
`"delivery": "fast_forward"` (`repository/v2`) Marina then fast-forwards the base
branch; without it (`repository/v1`) it opens a pull request. One active task
shares its frozen intent and budget across implementation, review, verification
and bounded repair. A rejected review, failed clean verification or required CI
can feed a repair only after its diagnosis and different approach are recorded. Requested review changes
block. The attempt limits are in the [profile contract](docs/workloads/profiles-v1.md);
installation acceptance is in [status](../docs/status.md). Cancellation waits for observed termination. Pause, resume, replan, retry and
reduce-authority commands are rejected in this workflow; ledger support for
those command types is not an execution capability. The separate verification
Job runs deterministic checks. The text-only reviewer sees the exact candidate
and original files, frozen criteria/context and repair history; it rejects weakened,
removed or skipped tests. Only the reviewed patch can commit, and its verdict is
recorded with the resulting commit and reviewer model/profile. Missing or invalid
verdicts block; disputed findings escalate. Review model calls share the original
budget and replay retained responses without new spend. See
[decision 0026](../docs/decisions/0026-read-only-review-and-exact-patch.md).
Completion means
the verified commit landed (v2) or a PR is ready for review (v1). Marina never
deploys.

Implementation attempts reach an explicit Responses-compatible model endpoint only
through Marina's gateway. GitHub/GHES is the first source adapter. Workers receive neither
source-write nor upstream-model credentials. The gateway checks live authority;
artifacts survive worker cleanup on Marina-owned storage. This workflow uses
fixed bounded repository rules and explicit immutable context; broader policy/plugin foundations
remain available for later workflows.

Repository configuration requires a pinned `repository_engine` selection. The
Kubernetes adapter only starts independent verification Jobs using
`Dockerfile.verifier`; `marina verify` is its entry point. The diagnostic profile
and custom Codex/Python hosting paths are removed. Existing journal records,
artifacts and credential identities retain their historical representation.
See the [profile contract](docs/workloads/profiles-v1.md),
[API guide](docs/workloads/api-v1.md) and
[installation guide](../deploy/kubernetes/agent-platform/README.md).

`marina check-config --config workflow.json` validates the current configuration
without reading credentials or contacting external services. Unknown retired
settings fail closed. Both stores reject unknown applied schema migrations;
rollback to an older release requires its compatible retained snapshot.

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

Check the platform ledger:

```bash
./bin/marina doctor --journal-path ./data/marina.db
```

Serve the platform API with an in-memory Kubernetes API and a simulated executor, for client contract tests:

```bash
go build -tags sandbox -o bin/marina-sandbox ./cmd/marina
./bin/marina-sandbox api-sandbox --addr 127.0.0.1:18090
```

Add `--kube-server`, `--kube-token-file` and `--kube-ca-file` to use a real cluster where the resources from `deploy/kubernetes/platform/marina-api` are installed.

Inspect the local journal without serving internal state as an API:

```bash
./bin/marina diagnostics --journal-path ./data/marina.db > marina-diagnostics.json
```

The export omits raw signal text and free-form journal messages, redacts arbitrary payload strings, sanitizes URLs, and fingerprints provider handles. It contains operational metadata only.

After draining work and stopping Marina, `marina artifact-retention --journal-path
/data/marina.db --artifact-directory /data/artifacts` reports expired unreferenced
artifacts without deleting them. Apply only a reviewed report with `--apply-plan`.
All historical journal references remain protected; seven days and 100 files are
the default age and batch size. See the [operations runbook](../deploy/kubernetes/first-release/OPERATIONS.md)
for the maintenance lock, Kubernetes procedure and recovery cases.

Mutating API requests are idempotent: `metadata.name` keys a create, and cancel
and answer carry `Idempotency-Key`. Clients authenticate with Kubernetes tokens
and are authorized by the same RBAC as the resources; see the [contract](api/README.md).

Run Marina's HTTP service:

```bash
MARINA_JOURNAL_PATH=./data/marina.db ./bin/marina serve --config workflow.json
```

The service listens on `:8090` by default. Liveness is available at `/healthz`; readiness at `/readyz` is successful only when the execution journal and platform ledger are available. The platform API is served under `/apis/`. The journal directory is created automatically, but the file must live on durable single-writer storage in Kubernetes.

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
| `MARINA_PROJECT_ID` | `default.default` | Ledger project (`<namespace>.<project>`) bound to the control-only mode. |
| `MARINA_JOURNAL_PATH` | `data/marina.db` | Durable SQLite execution journal. |
| `MARINA_REPOSITORY` | `disabled://milestone-one` | Repository binding; deliberately non-mutating in Milestone 1. |
| `MARINA_POLICY_VERSION` | `milestone-one` | Version persisted with the project binding and executions. |
| `MARINA_LEASE_RENEW_BEFORE` | `10s` | Window in which reconciliation renews an external lease. |
| `MARINA_RUN_LEASE` | `30s` | Lease duration on a claimed run, renewed by heartbeats. |
| `MARINA_RECONCILE_INTERVAL` | `10s` | Interval between non-overlapping lease reconciliation cycles. |

## Project layout

```text
cmd/marina/                 CLI and service entry point
../docs/ARCHITECTURE.md     Joint Deck–Marina architecture
internal/workcontrol/      Provider-neutral work-control contract
internal/platformapi/      Platform API server, controller and API sandbox
internal/kubeapi/          Small Kubernetes REST client
api/                       Public contract: OpenAPI, CRDs, fixtures
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
engine and verifier image identities, selected client and Git sources and
explicit conversation excerpts. It persists the package before engine dispatch and
recovers those exact bytes on restart. Plans bind the package digest; the native
initial prompt and verifier input carry it. Clients read a bounded derived
snapshot in the Run status.

New source revisions affect newly authorized tasks. Explicit revocation and
expired included inputs deny further gateway access and stop the workflow
for clarification, waiting for runtime termination before reporting a blocker.
Expired reference data and known instruction overrides can be omitted at assembly;
stale guidance and declared conflicts block. Conversation stays untrusted data.
These checks do not claim general semantic conflict or prompt-injection detection;
existing filesystem, network, source-publication, authority and budget gates apply.

A `Project` resource binds the repository: an owner/repository name plus branch
and a credential Secret reference, never an arbitrary endpoint or credential.
Marina reconciles it even with new task intake disabled. Validation uses the
installed GitHub API/clone origin. Versioned repository history survives restart
and active runs resolve their original binding. An installation-profile change
requires review and newly authorized work; it cannot silently alter old
execution policy. See [the context proof](../docs/validation/ctx-02-08-2026-09-21.md).

## Publishing approved PRDs

The conversation actor can propose a `docs/prd/` Markdown document without
source authority. The client owns its explicit approval and creates the bounded
publication Run. Marina retains the typed approved document in the frozen task
and native input, rejects changed bytes or unrelated file operations before
review and publication, and applies the same independent review, clean checks,
CI and landing gates. Later conversation turns receive retained PRD text as
untrusted thread data. See [decision 0029](../docs/decisions/0029-approved-prd-publication.md).

Use matching Marina and conversation BYO releases. The installed
repository scope must include `docs/prd/`; it is never expanded by model output.
Current installation acceptance is in [status](../docs/status.md).

## Explicit profiles for more projects

`serve --config` and `check-config --config` accept either the original single
workflow or `{"version":1,"projects":[...complete workflow profiles...]}`.
Each project needs one explicit private profile.
Repository, checks, allowed paths, required CI, models, credential file references,
engine and verifier remain specific to that project. Service TLS, signing and
artifact settings must agree. Configuration is bounded to sixteen profiles.

One durable intake controller admits work across the configured projects. An
active execution or unresolved acquisition blocks any additional task. Workflow
and command reconciliation use the recorded project's profile; gateway routing
uses the durable attempt/turn identity and then validates the original credential
and live authority. Removing a project never routes its work to another profile.
The original single-project configuration retains its digest and behavior.
See [decision 0031](../docs/decisions/0031-explicit-per-project-profiles.md) and
[installation acceptance](../docs/status.md).

Approved PRD implementation tasks retain a typed PRD source reference separately
from the publication document. Marina requires that source commit on the execution
base even if ordinary task dependencies have been edited. Task proposals do not
authorize work.
