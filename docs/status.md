# Current state

Last updated: 2026-10-18.

This is the only description of what is supported, what is implemented but not
yet accepted, and what is not supported. Other documents link here instead of
restating it. Update it in the same change that ships, removes or re-scopes a
capability. The long-term direction is in [VISION.md](VISION.md).

Deck, Marina and the kagent engine run on local Kubernetes on the operator's
laptop. The chat agent turns a request into a task, and Marina takes one
authorized task at a time to a verified commit on main or to a clear blocker.

## What works

Accepted on the local Podman/Kind installation.

**Repository workflow** ([cutover and landing evidence](validation/kint-11-land-01-2026-09-28.md),
[core failure cases](validation/kint-09-core-2026-09-28.md),
[final release and remaining failure cases](validation/kint-12-17-2026-10-01.md))

```text
task and budget -> approved plan -> native Codex on kagent -> clean verification Job
                -> task branch -> required CI on that commit -> fast-forward main
                -> evidence in Deck
```

- One repository and one active task, with bounded implementation and repair
  attempts and separate clean verification. Marina creates each engine attempt's
  own template and removes it afterwards.
- Independent semantic review gates every exact patch before commit. A fresh
  read-only reviewer uses a different model, checks the original files and
  frozen task, and rejects weakened, removed or skipped tests. A real rejection
  fed saved diagnosis, a different repair approach, fresh review, clean
  verification and exact-head CI before one commit landed within the original
  budget ([AUTO-08 acceptance](validation/auto-08-2026-10-03.md),
  [review contract](decisions/0026-read-only-review-and-exact-patch.md)).
- Failed review, clean verification or required CI feeds a saved diagnosis and different
  repair approach under the original budget and frozen inputs. Fresh review,
  checks and required CI must pass for the repaired commit; earlier evidence
  remains history. Native repair survived restart and lost publication responses
  on local Kind ([AUTO-02–06 evidence](validation/auto-02-08-local-2026-10-01.md),
  [budget contract](decisions/0025-repair-attempts-and-budget.md)).
- It ends with the verified commit on main or at a blocker. Three failures,
  a smaller exhausted budget, a moved base branch or a changed task branch
  block. This installed profile creates no pull request; Marina never
  force-pushes or deploys.
- GitHub branch protection is not required: Marina lands only a commit whose
  required checks passed on that exact commit (operator decision, 2026-09-27).
- Restarts of Marina, the engine controller, the engine database and the egress
  gateway during a model call recovered and landed. Three consecutive unavailable
  compute observations stop the original instance and block without replacement
  work. Lease expiry stops compute even while Deck is unavailable.
- Automatic task completion and code-prerequisite observation on landing are
  tracked separately under LAND-02; this review acceptance does not complete it.

The workflow uses task authorization, immutable profiles and context, leases
and fences, budgets, cancellation, sandbox isolation, the model gateway,
artifacts and evidence rules. Cancellation completes only after the engine
instance is gone. Installations can select the `repository/v1` pull-request
delivery profile through the same native engine. The custom Codex Job runtime
and diagnostic agent are removed; Jobs run deterministic verification only.
Without workflow configuration Marina runs in control-only mode. Disabling new
intake does not pause
executions that have already started.

**Chat** ([conversation evidence](validation/chat-04-10-2026-09-28.md),
[repository/context acceptance](validation/chat-11-2026-10-03.md))

- The chat agent answers in the Deck thread where it was invoked, on kagent.
  It reads the project's pinned repository through two bounded read tools and
  uses the selected project context frozen at invocation. It has no repository
  mutation or arbitrary tool authority. Each turn gets its own engine template.
- Source edits and branch movement do not replace recorded inputs; revocation
  stops further use. Intermediate model responses and one clarification
  follow-up share the original token limit and deadline. Frozen inputs, the
  canonical reply and usage survived a local restart. Model citations can still
  be inaccurate: the acceptance reply's test-line citation was off by one.
- It can ask one typed clarification question; Deck shows it in the thread and
  the answer goes to the same waiting turn, also across a Marina restart or a
  lost response.
- Exact answer replay preserves the original human decision. Changed replays,
  expired questions and answers after the turn ends are rejected
  ([final question acceptance](validation/kint-12-17-2026-10-01.md#questions-and-publications)).
- It can create a Backlog draft, or a Ready task within a fixed delegation budget
  when the request clearly asks for work. Results and blockers appear in the
  originating thread, and feedback on a finished task creates a linked follow-up.
- Publications survive lost responses and restarts without duplicate messages or
  tasks.

**Deck**: operator surface and ledger

- A three-column project workspace with projects, a middle work overview
  (In progress, To do, Recently completed), and conversation with threads and
  a persistent composer. Task details, project context, needs-attention work,
  and the archive remain accessible
  ([channel evidence](validation/deck-channel-first-2026-09-23.md),
  [local UI-01 evidence](validation/ui-01-2026-09-26.md)).
- Promote a message to a Backlog draft, or save it as a reference note (manual).
- Versioned project knowledge and selected conversation excerpts, frozen for
  active runs. The repository association is validated by Marina
  ([evidence](validation/ctx-02-08-2026-09-21.md)).
- Human acceptance bound to a completed run. Same-project, acyclic dependencies
  are checked when finding ready work and at claim. A code prerequisite needs the
  operator's confirmation that its code is on the base branch.
- Threads linked to tasks, and recorded requests to the configured agent
  ([evidence](validation/chat-03-2026-09-23.md)).
- Installed workload-profile discovery and idempotent invocation, and
  project- and capability-scoped service identities.
- The ledger records projects, tasks, budgets, runs, plans, decisions, commands,
  events, artifacts and evidence. Updates use versions, leases, fence tokens and
  idempotent replay.
- Digest-checked artifact downloads, live updates, project export/import, and a
  versioned OpenAPI v1 contract used by Marina and platformctl.
- The local installation opens without sign-in
  ([evidence](validation/deck-local-access-2026-09-23.md)).

**Marina**: execution and recovery

- A durable journal and outbox. It recovers from restarts without duplicate work.
- A model gateway with short-lived per-attempt credentials and token/cost
  reservations. Workers get no upstream model, Git-write or Kubernetes
  credentials.
- Worker isolation: separate namespace, read-only root filesystem, no service
  account token, network limited to Marina and DNS.
- Immutable, content-addressed context packages with provenance, digests, trust
  and revocation.
- Artifact-storage failures prevent completion until evidence is retained.
  Failed cleanup is visible and retries removal of the original owned resources.

**platformctl** is a stateless CLI over Deck's API.

**Engine** ([installation](../deploy/kubernetes/local-podman-kagent/README.md),
[KINT-16](validation/kint-16-2026-09-26.md))

- Pinned kagent and Substrate are installed permanently; both harness images come
  from the installation's local registry.
- The pinned local Marina release pair passed an image upgrade during an active
  kagent model call and a previous-image, retained-data-copy rollback while
  preserving the exact Deck reply and usage
  ([evidence](validation/kint-10-recovery-2026-09-26.md)).
- The final release passed repository and chat work on new application stores,
  a schema-gated restore of the retained Deck/Marina pair, and upgrade of that
  restored history. Original volumes and digest-checked artifacts remained
  intact ([evidence](validation/kint-12-17-2026-10-01.md#fresh-installation-upgrade-and-rollback)).

**Platform components** ([installation](../deploy/kubernetes/platform/README.md),
[evidence](validation/plat-05-09-2026-10-17.md))

- Kyverno enforces digest-pinned images, no privileged containers, memory limits,
  app labels and a NetworkPolicy before Pods start in the platform's own
  namespaces; it reports the same rules for third-party components (PLAT-05).
- kgateway serves `https://localhost:8443`: one Gateway API TLS listener passes
  Deck's own TLS through by host name (PLAT-07).
- agentgateway holds the model provider key and serves the Responses API to
  Marina only, with a client key and installation-CA TLS. It also serves Jaeger's
  read-only trace tools over MCP behind an allow-list (PLAT-06, PLAT-08).
- An OpenTelemetry Collector strips credential-like data and sends traces to an
  in-memory Jaeger; agentgateway's model traces arrive there (PLAT-09).

## Built, awaiting acceptance

The source changes below pass product and real-process checks, but are not yet
accepted on the installed local release. See the
[validation report and remaining proof](validation/land-02-03-ver-01-test-01-2026-10-03.md).

- **LAND-02:** evidenced landing atomically completes the task, retains its exact
  commit and selected evidence, and releases code prerequisites without operator
  confirmation. Marina checks prerequisite ancestry on the execution base.
- **LAND-03:** a moved main branch admits a bounded native rebase on a fresh
  branch, then fresh independent review, clean verification and exact-head CI.
  Conflicts block and previous attempts remain history.
- **VER-01:** the verifier and native Codex images share prepackaged Node, Go,
  frozen dependencies and Chromium. Runtime downloads are disabled; installing
  the image also requires a new engine golden snapshot.
- **TEST-01:** five browser tests pass in local Chrome against a disposable
  real Deck process and its production frontend. They cover projects, durable
  chat, task movement, landed results, legacy acceptance and exact PRD approval, ordered task approval, PRD acceptance and follow-ups. CI is configured;
  its remote execution has not been observed.

- **PRD-01:** the chat agent proposes an immutable PRD in its thread. Operator
  approval creates one bounded publication task with the exact text, `docs/prd/`
  path and repository binding. Marina rejects altered text or unrelated files
  before publication and applies the normal review/test/CI/landing gates.
  Restart, replay, export/import, browser and real-process workflow checks pass
  ([evidence](validation/prd-01-and-landing-checks-2026-10-03.md)). Installation
  and live native publication are pending; the operator kept GitHub unchanged.

- **PRD-02–03:** a published PRD receives an immutable ordered task proposal in
  its thread. One operator approval creates bounded tasks with frozen PRD source
  links and dependency order. The thread shows landed results, accepts the current
  evidenced result set and requests follow-up proposals. Persistence and five
  real-process browser tests pass; installed native acceptance remains pending.
- **PROJ-01:** Marina accepts explicit private profiles per project, with separate
  repositories, checks, credential references and versioned inputs. Attempt-based
  gateway routing and global admission preserve one active repository task.
  Product tests and two-project real-process conformance pass; installed
  two-project acceptance follows FLOW-01.

These additions and the corrected strict chat PRD response schema are recorded in
[the follow-up evidence](validation/pending-prd-projects-2026-10-05.md).

- **PLAT-06, PLAT-08, PLAT-09 through Marina:** Marina's model calls are
  configured to go through agentgateway, and its chat gateway can offer
  allow-listed MCP tools. Marina records one trace per task with step and model
  spans. Product checks pass; the installed Marina image predates the MCP and
  trace code, and no chat turn or task has yet run through the new path
  ([evidence](validation/plat-05-09-2026-10-17.md)).

- **MAR-01 to MAR-09, Marina's platform API:** Marina serves `marina.dev/v1`
  over HTTP/JSON and as Kubernetes resources from its own run ledger, with
  Kubernetes identity and RBAC. Deck creates Runs and Turns and mirrors them;
  `platformctl` and `kubectl` work with the same resources. Product and
  real-process checks pass against Marina's API sandbox, and kubectl worked
  against the Kind API server. The installed release still runs the earlier
  Deck-provider pair until the cutover (MAR-10)
  ([evidence](validation/mar-01-09-2026-10-18.md)).

## Not supported

- Pause, resume, replan, retry and reduce-authority commands. Deck can store them;
  Marina rejects them.
- GitHub-side enforcement of required CI (branch protection). Marina enforces
  the exact-commit gate itself.
- Notes and knowledge bases that agents maintain.
- The diagnostic analysis workflow (LangChain) is retired
  and its runtime, images and setup paths are removed
  ([decision 0023](decisions/0023-focus-kagent-cutover.md)). Historical records
  and artifacts remain readable.
- Deployment, production signals and production remediation.
- More than one active task, more than one repository per workflow, arbitrary
  tools, and general agent hosting.
- High availability, automated backups and a full engine-version downgrade.
  Application rollback is manual and requires the compatible retained snapshot;
  each application is a single replica on SQLite and a persistent volume.

A ledger record, schema or reusable foundation does not make a capability
supported.

## Environment and use

- Acceptance environment: local Podman/Kind (`kind-marina-local`) on the
  operator's laptop, at `https://localhost:8443`, with the Mac awake. After it
  sleeps, the Podman VM clock needs a resync
  ([local guide](../deploy/kubernetes/local-podman/README.md)). The GCP cluster is
  authorized for tests; it is not an acceptance environment.
- New task intake stays disabled until the operator enables it; one existing
  Ready Context Lens task would otherwise be claimed.
- Pilot services: GitHub/GHCR, OpenRouter, separate SQLite stores and artifact
  files on persistent volumes ([decision 0010](decisions/0010-initial-installation-services.md)).
- First real use: the operator runs the Context Lens pilot personally through the
  platform ([pilot preparation](../deploy/pilots/context-lens/README.md)). It is
  not agent backlog work; agents do not perform or track it.
