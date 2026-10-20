# Plan: Marina as a stand-alone platform with its own API

- Date: 2026-10-17
- State: direction accepted in [decision 0033](../decisions/0033-marina-platform-api.md).
  MAR-02 to MAR-09 are built ([evidence](../validation/mar-01-09-2026-10-18.md));
  MAR-01 waits for operator review. MAR-10 and MAR-11 are installed on local Kind
  and wait for their landing proofs ([evidence](../validation/mar-10-11-2026-10-18.md)).
- Related: [decision 0032](../decisions/0032-agent-and-application-platform.md),
  [platform components plan](platform-components.md)

## Goal

Marina is a product on its own, like an extension of Kubernetes. Factories, UIs
and CLIs use it through one API with two surfaces: HTTP/JSON and Kubernetes
resources. Marina depends on no client; Deck becomes one client of Marina.

## Rules

1. **Platform invariants, factory choices.** Marina always enforces budgets and
   limits, sandbox and credential isolation, idempotency, audit, and no
   completion without independent evidence. Everything else is chosen per
   workload profile or run: checks, review, pull request or landing on main,
   repair limits, PRDs. No factory's rules are built into Marina.
2. **No engine or sandbox in the API.** Clients see workloads, runs, turns,
   budgets and evidence, never kagent, Agent Substrate, gVisor, Pods or Jobs.
   The runtime can be swapped behind `internal/runtime` without clients
   noticing. Today engine details leak through the `repository_engine`
   profile field and the `harness` field in progress events; the API keeps
   both out.
3. **One model, two surfaces.** Each resource has one schema. The Kubernetes
   resource and the HTTP/JSON object are the same object; Marina's journal is
   the record used for recovery and resource status mirrors it.
4. **Kubernetes identity and tenancy.** A project is a namespace. The HTTP API
   authenticates Kubernetes tokens (TokenReview) and authorizes with the same
   RBAC as the resources (SubjectAccessReview). Clients get access through
   normal Roles.
5. **Direct switch.** The new API replaces Marina's Deck-provider path in one
   cutover; old records stay readable.

## Today versus target

| | Today | Target |
| --- | --- | --- |
| Who owns the API | Deck ([openapi.yaml](../../deck/api/openapi.yaml)); Marina is its client | Marina, API group `marina.dev/v1`; Deck, CLI and others are clients |
| How work arrives | Marina polls Deck, claims a task, holds a lease and fence | A client creates a Run (resource or HTTP) with an idempotency key |
| Source of truth for runs | Deck's ledger; Marina mirrors it | Marina's journal; Run status mirrors it |
| Agent turns (chat) | Marina reads Deck's invocation feed | A client creates a Turn with its frozen context |
| Factory rules | Built into the repository workflow | Chosen in the Workload profile and Run |
| Identity | Deck-issued service tokens | Kubernetes identities and RBAC, one namespace per project |
| Contract tests | Deck is the provider, Marina the consumer | Marina is the provider; each client runs consumer tests |
| Installation | Private local files next to Deck | A Marina Helm chart that installs without Deck |

## API v1 resources

All namespaced except `Capabilities`. Each has `spec` (what the client asks for)
and `status` (what Marina observed).

| Resource | Purpose |
| --- | --- |
| `Capabilities` | What this installation offers: workload types, models, tools, limits |
| `Project` | Namespace-scoped project: repository binding and default workload profile |
| `Workload` | A profile: workload type plus the factory's choices (checks, review, delivery, repair limits, allowed paths, models, tools) |
| `Run` | Bounded work: intent, acceptance criteria, budget, inputs, workload; status, events, evidence; cancel |
| `Turn` | One agent turn: frozen thread and context, budget; question, answer, outcome |
| `Decision` | Something Marina needs a client to answer: plan approval, clarification |

HTTP adds what resources cannot carry well: ordered run events (polling and
server-sent events), artifact download and an idempotent create with
`Idempotency-Key`. Version-1 semantics stay stable; breaking changes need v2.

## What changes in the code

- **Removed at the cutover:** Marina's Deck client (`internal/deck`, 1,345 lines),
  the claim and Deck-lease parts of `reconcile`, and Deck's provider endpoints.
- **Stays:** the journal, budgets, frozen inputs, gateway, review, verification,
  landing, repair and recovery.
- **Generalized:** the repository workflow reads its gates from the Workload
  profile instead of hard-coding them.
- **New:** the HTTP API server, a controller for the custom resources, Kubernetes
  authentication and authorization, and the contract under `marina/api/`.
- **Deck:** keeps projects UI, threads, PRDs, backlog and acceptance, and creates
  Runs and Turns in Marina.

## Tasks

| ID | Outcome | Acceptance |
| --- | --- | --- |
| MAR-00 | Direction recorded. | Done: [decision 0033](../decisions/0033-marina-platform-api.md). |
| MAR-01 | API design: resource schemas, invariants versus factory choices, events, errors, idempotency, RBAC verbs, status mirroring. OpenAPI, CRD schemas and canonical fixtures in `marina/api/`. | Operator review; fixtures validate against both the OpenAPI and the CRD schemas; no engine or sandbox names. |
| MAR-02 | Kubernetes identity: HTTP API authenticates with TokenReview and authorizes with SubjectAccessReview; Roles for operator, factory and read-only clients. | Tests for allowed, denied and wrong-namespace requests. |
| MAR-03 | Run lifecycle owned by Marina: create (HTTP or resource), status, events, cancel, evidence, artifacts, from the journal. Internal ownership replaces Deck leases and fences. | Restart, lost response, cancel and budget cases pass with no Deck. |
| MAR-04 | Controller: reconciles Run, Turn, Project and Workload resources to the journal and mirrors status back; deleting a Run cancels it. | Resource and HTTP paths produce the same journal records; restart and replay pass. |
| MAR-05 | Workload profiles carry the factory choices; the repository workflow reads its gates from them. Invariants stay enforced whatever the profile says. | A profile without review and one with review both run; a profile cannot disable budgets, isolation or evidence. |
| MAR-06 | Turns and Decisions: agent turns with frozen context, questions and answers, plan approval. | Chat and approval round trips pass through both surfaces. |
| MAR-07 | Contract tests that any client runs against a real Marina process. | `make conformance` runs them for the CLI and Deck. |
| MAR-08 | Deck as a client: creates Runs and Turns, shows Marina's status and evidence. Deck's provider endpoints and Marina's Deck client are removed. | Deck's browser tests pass against a real Marina; no Marina call into Deck remains. |
| MAR-09 | `platformctl` uses Marina's API, and `kubectl` works with the resources. | CLI and kubectl each submit a run and read its evidence. |
| MAR-10 | Cutover on local Kind: stop new work, switch Deck and Marina together, keep old records readable. | The factory runs through the new API; old history is still readable. |
| MAR-11 | Helm chart installing Marina without Deck, with the platform components as options; a non-Deck client lands a run on main. | Fresh install with no Deck; a kubectl-created Run lands with evidence. |
| MAR-12 | Later: a second runtime adapter (for example Kubernetes Agent Sandbox) behind the same port. Trigger: the operator wants to replace the sandbox. | The same Run passes on both runtimes with no client change. |

Order: MAR-01 → MAR-02/03 → MAR-04/05 → MAR-06 → MAR-07 → MAR-08/09 → MAR-10 →
MAR-11. MAR-12 waits for its trigger.

## MAR-10 cutover steps

Environment: local Kind only (`kind-marina-local` on
`podman-machine-default-root`), following the
[local Podman guide](../../deploy/kubernetes/local-podman/README.md). The
private installation lives under `~/.local/share/the-platform/marina-local/`;
back up every private file you change next to it with a dated suffix. Never put
secret values in the repository, logs or evidence.

1. **Stop new work.** In Deck, leave no task Ready or running (finish, block or
   cancel them) and no chat turn open. Record the task and run counts.
2. **Snapshot data.** Copy Deck's and Marina's SQLite databases and artifact
   directories from their volumes to a dated private folder. This is the
   rollback copy, together with the current image tags.
3. **Repository changes** (tests first, then `make check` and
   `make conformance`):
   - `deploy/kubernetes/first-release/control/deck.yaml` and
     `deploy/kubernetes/local-podman/deck.yaml`: drop `DECK_SERVICE_AUTH_FILE`,
     `DECK_DEMO_AGENT` and `DECK_ARTIFACT_*`; add `DECK_MARINA_URL` (Marina's
     in-cluster HTTPS service), `DECK_MARINA_CA_FILE` and
     `DECK_MARINA_PROJECTS_FILE`; mount a projected service account token for
     `DECK_MARINA_TOKEN_FILE`. See [deck/README.md](../../deck/README.md).
   - Add the API install (`deploy/kubernetes/platform/marina-api`: CRDs, the
     controller ClusterRole and client Roles) to the local overlay, and a
     RoleBinding giving Deck's service account `marina-factory` in each
     project namespace.
   - One namespace per Marina project with its `Project` and `Workload`
     resources (repository, checks, review, delivery, models) matching the
     current private workflow profile.
   - Remove Marina's Deck-provider settings and Deck's Marina service policy
     from the installation scripts, and update
     `scripts/test_prepare_installation.py`.
4. **Images.** Build Deck, Marina and the verifier from the commit, load them
   into Kind and pin them in the private overlay.
5. **Private config.** Write the Deck projects mapping file (Deck project to
   namespace, Marina project, `repository/v2` to the landing Workload, chat to
   the conversation Workload). Remove the old Deck service token and policy.
6. **Apply together.** Apply the reviewed private overlay so Deck and Marina
   restart on the new release at the same time; check `/readyz` on both.
7. **Prove it** and store redacted evidence under `docs/validation/`:
   - old tasks, runs, decisions and evidence still show in Deck;
   - `kubectl get runs,decisions -n <project namespace>` works;
   - one chat turn gets a reply (spends a small model budget);
   - one small task lands on main in the acceptance repository with review,
     verification, CI and landing evidence (spends model budget and commits:
     needs the operator's OK at that moment);
   - cancelling a waiting run reaches Cancelled.
8. **Rollback** if any step fails: reapply the previous images and private
   overlay, and restore the step 2 copy.

Done when the steps above pass and status, backlog and the validation report
are updated.

## Risks

1. **Large change in both products with a direct switch.** Until MAR-10 the
   current installation keeps running the old release; the cutover installs
   Deck and Marina together. If it fails, roll back to the previous release
   pair and its data copy.
2. **Security surface.** Marina gains a public API. Kubernetes authentication
   and RBAC apply from the first request; the local installation stays
   localhost-only.
3. **Generic profiles can grow without limit.** MAR-05 adds only the choices the
   current factory already uses; new choices need a workload that needs them.

## Ideas from OpenChoreo

[OpenChoreo](https://github.com/openchoreo/openchoreo) (CNCF Sandbox, Apache-2.0)
is a Kubernetes developer platform built from pluggable parts. Its developer
portal (Backstage) is out of scope for Marina; its resource model has useful
patterns. Evaluated on 2026-10-17 against OpenChoreo v1.3.

| OpenChoreo idea | What Marina could take | Fit |
| --- | --- | --- |
| Planes as resources (`DataPlane`, `WorkflowPlane`, `ObservabilityPlane`, with `Cluster*` variants) | Register each platform slot as a cluster-scoped resource, for example `ClusterRuntime` (engine and sandbox), `ClusterModel`, `ClusterToolServer` (MCP) and `ClusterTelemetry`. This replaces the slot manifest file (PLAT-03) and is how MAR-12 swaps a sandbox. | Strong: the clearest way to make components pluggable and declarative |
| `ComponentType` and composable `Trait`, cluster-wide and namespaced | Platform-wide `ClusterWorkload` templates a factory's namespaced `Workload` refers to; traits for cross-cutting choices such as review, plan approval, MCP tools and budgets, attached instead of repeated | Good, after MAR-05 shows which choices recur |
| Immutable `ComponentRelease` / `RenderedRelease` bound by `ReleaseBinding` | A visible `WorkloadRevision` for the profile digest Marina already freezes per run | Optional: makes freezing inspectable; not needed for correctness |
| `Environment` and `DeploymentPipeline` | Promotion of apps between environments when the platform deploys apps (PLAT-17) | Later, with PLAT-17 |
| `Workflow` / `WorkflowRun` on Argo Workflows | In-cluster CI for the parked GitHub-dependency topic | Later, with that topic |
| `SecretReference` | Already matches `Project.spec.repository.credentialSecret`; pairs with External Secrets (PLAT-04) | Already aligned |
| Its own `AuthzRole` and bindings | Not needed: Marina uses Kubernetes RBAC directly | Skip |

Recommended first step after MAR-11: replace PLAT-03's file with cluster-scoped
plane resources, starting with `ClusterRuntime` and `ClusterModel`.
