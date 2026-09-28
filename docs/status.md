# Current state

Last updated: 2026-09-28.

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
[failure cases](validation/kint-09-core-2026-09-28.md))

```text
task and budget -> approved plan -> native Codex on kagent -> clean verification Job
                -> task branch -> required CI on that commit -> fast-forward main
                -> evidence in Deck
```

- One repository, one active task, one implementation attempt and one separate
  verification attempt. Marina creates the attempt's own engine template and
  removes it afterwards.
- It ends with the verified commit on main or at a blocker. Failed verification,
  failed CI, a moved base branch, a changed task branch and an exhausted budget
  block. Marina never force-pushes, opens a pull request or deploys.
- GitHub branch protection is not required: Marina lands only a commit whose
  required checks passed on that exact commit (operator decision, 2026-09-27).
- Restarts of Marina, the engine controller, the engine database and the egress
  gateway during a model call recovered and landed. A lost worker blocks the
  task truthfully, but only at its deadline.
- Deck shows a landed task as Awaiting review until the operator accepts the
  result; completing it on landing is LAND-02.

The workflow uses task authorization, immutable profiles and context, leases
and fences, budgets, cancellation, sandbox isolation, the model gateway,
artifacts and evidence rules. Cancellation completes only after the engine
instance is gone. The `repository/v1` pull-request profile and the older Codex
worker Job remain in the code until KINT-12. Without workflow configuration
Marina runs in control-only mode. Disabling new intake does not pause
executions that have already started.

**Chat** ([evidence](validation/chat-04-10-2026-09-28.md))

- The chat agent answers in the Deck thread where it was invoked, on kagent,
  with no source, tool or write authority. Each turn gets its own engine
  template.
- It can ask one typed clarification question; Deck shows it in the thread and
  the answer goes to the same waiting turn, also across a Marina restart or a
  lost response.
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

**platformctl** is a stateless CLI over Deck's API.

**Engine** ([installation](../deploy/kubernetes/local-podman-kagent/README.md),
[KINT-16](validation/kint-16-2026-09-26.md))

- Pinned kagent and Substrate are installed permanently; both harness images come
  from the installation's local registry.
- The pinned local Marina release pair passed an image upgrade during an active
  kagent model call and a previous-image, retained-data-copy rollback while
  preserving the exact Deck reply and usage
  ([evidence](validation/kint-10-recovery-2026-09-26.md)).

## Implemented but not yet accepted

Nothing is waiting for acceptance.

## Not supported

- Pause, resume, replan, retry and reduce-authority commands. Deck can store them;
  Marina rejects them.
- Automatic repair after a failure. The policy is decided
  ([decision 0020](decisions/0020-bounded-repair-policy.md)); the behavior is not
  built.
- Independent semantic review before each commit. The policy is decided
  ([decision 0021](decisions/0021-semantic-review-gate.md)); the behavior is not
  built.
- Rebasing a task when main moved before landing (LAND-03); such a task blocks.
- GitHub-side enforcement of required CI (branch protection). Marina enforces
  the exact-commit gate itself.
- PRDs drafted in the chat and committed to the project repository, and tasks
  derived from them. Tasks can be created by hand, promoted from messages, or
  delegated by the installed chat agent; PRD automation is still absent.
- Notes and knowledge bases that agents maintain.
- The diagnostic analysis workflow (LangChain) is retired
  ([decision 0023](decisions/0023-focus-kagent-cutover.md)). Its code is still
  installed until KINT-12 removes it; do not start new work on it
  ([earlier evidence](validation/agent-11-13-2026-09-22.md)).
- Deployment, production signals and production remediation.
- More than one active task, more than one repository per workflow, arbitrary
  tools, and general agent hosting.
- Noticing a lost engine worker before the task deadline (KINT-17).
- Rejecting a late answer: Deck accepts an answer to a question whose turn has
  already ended, and Marina does not deliver it (KINT-17).
- High availability, backups and restore drills. Each service is a single replica
  on SQLite and a persistent volume.

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
