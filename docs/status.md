# Current state

Last updated: 2026-09-26.

This is the only description of what is supported, what is implemented but not
yet accepted, and what is not supported. Other documents link here instead of
restating it. Update it in the same change that ships, removes or re-scopes a
capability. The long-term direction is in [VISION.md](VISION.md).

Deck and Marina run on local Kubernetes on the operator's laptop. They take one
authorized task to either a verified pull request for human review or a clear
blocker.

## What works

Accepted on the local Podman/Kind installation.

**Repository workflow** ([evidence](validation/poc-mvp-2026-09-21.md))

```text
task and budget -> approved plan -> coding Job -> clean verification Job
                -> pull request -> required CI at the same head -> evidence in Deck
```

- One repository, one active task, one implementation attempt and one separate
  verification attempt.
- It ends at a PR for human review or at a blocker. Failed checks, a changed or
  closed PR, and requested changes block. Marina never merges or deploys.

The workflow uses task authorization, immutable profiles and context, leases
and fences, budgets, cancellation, Kubernetes isolation, the model gateway,
artifacts and evidence rules. Cancellation completes only after the worker has
stopped. Without workflow configuration Marina runs in control-only mode.
Disabling new intake does not pause executions that have already started.

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

**Local chat engine** ([evidence](validation/kint-16-2026-09-26.md))

- Pinned kagent and Substrate are permanently installed beside the existing
  repository runtime. A prepared, bounded conversation turn produced a real
  model reply in its Deck thread, and that thread survived a Deck restart.
- Each chat turn needs a fresh prepared template, ModelConfig and Secret. The
  operator prepares the next slot with the
  [local helper](../deploy/kubernetes/local-podman-kagent/README.md) after the
  current turn finishes. The next slot is currently waiting for its golden
  snapshot; repository tasks still use the existing runtime.
- The pinned local Marina release pair passed an image upgrade during an active
  kagent model call and a previous-image, retained-data-copy rollback while
  preserving the exact Deck reply and usage
  ([evidence](validation/kint-10-recovery-2026-09-26.md)).

## Implemented but not yet accepted

| Capability | Where it stands | Still missing |
| --- | --- | --- |
| Conversational agent with no write authority (CHAT-04) | Deterministic checks and one earlier installed model reply pass. The [latest browser probe](validation/chat-04-10-local-2026-09-26.md) blocked after model accounting, and the next actor template is pending its golden snapshot. | Restore a ready single-use slot, then pass browser and negative/restart cases. |
| Engine questions answered in the thread (CHAT-05) | Product checks and real-process conformance pass. | Installed engine and browser acceptance. |
| Conversation delegation, result publication and linked follow-up (CHAT-06–08) | Product checks and real Deck conformance pass. | Installed actor, model and browser acceptance. |
| kagent for repository work (KINT-01–11) | Isolated local trials passed a native Codex PR and a question round-trip. KINT-16 installed the engine permanently for chat; KINT-10 accepted the pinned local image transition. | Core repository failure cases (KINT-09), cutover (KINT-11), and legacy removal (KINT-12–15), per [decision 0023](decisions/0023-focus-kagent-cutover.md). New repository work still uses the existing runtime. |

## Not supported

- Pause, resume, replan, retry and reduce-authority commands. Deck can store them;
  Marina rejects them.
- Automatic repair after a failure. The policy is decided
  ([decision 0020](decisions/0020-bounded-repair-policy.md)); the behavior is not
  built.
- Independent semantic review before each commit. The policy is decided
  ([decision 0021](decisions/0021-semantic-review-gate.md)); the behavior is not
  built.
- Landing on main without a pull request, after the platform check and GitHub
  CI ([decision 0022](decisions/0022-autonomous-software-factory.md)). Work still
  ends at a pull request.
- PRDs drafted in the chat and committed to the project repository, and tasks
  derived from them. Tasks are created by hand or promoted from messages.
- Notes and knowledge bases that agents maintain.
- The diagnostic analysis workflow (LangChain) is retired
  ([decision 0023](decisions/0023-focus-kagent-cutover.md)). Its code is still
  installed until KINT-12 removes it; do not start new work on it
  ([earlier evidence](validation/agent-11-13-2026-09-22.md)).
- Deployment, production signals and production remediation.
- More than one active task, more than one repository per workflow, arbitrary
  tools, and general agent hosting.
- Transparent recovery of an interrupted worker step. Active worker I/O can block
  across outages.
- High availability, backups and restore drills. Each service is a single replica
  on SQLite and a persistent volume.

A ledger record, schema or reusable foundation does not make a capability
supported.

## Environment and use

- Acceptance environment: local Podman/Kind (`kind-marina-local`) on the
  operator's laptop, at `https://localhost:8443`. The GCP cluster is authorized
  for tests; it is not an acceptance environment.
- Pilot services: GitHub/GHCR, OpenRouter, separate SQLite stores and artifact
  files on persistent volumes ([decision 0010](decisions/0010-initial-installation-services.md)).
- First real use: the operator runs the Context Lens pilot personally through the
  platform ([pilot preparation](../deploy/pilots/context-lens/README.md)). It is
  not agent backlog work; agents do not perform or track it.
