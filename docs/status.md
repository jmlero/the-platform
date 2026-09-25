# Current state

Last updated: 2026-09-25.

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

- A project workspace that opens on the conversation: one channel per project,
  threads, a compact Backlog / Ready / In progress / Done list, a task drawer,
  project context and an archive
  ([evidence](validation/deck-channel-first-2026-09-23.md)).
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

## Implemented but not yet accepted

| Capability | Where it stands | Still missing |
| --- | --- | --- |
| Conversational agent with no write authority (CHAT-04) | Deterministic checks pass. | Live engine and model acceptance. |
| Engine questions answered in the thread (CHAT-05) | Product checks and real-process conformance pass. | Installed engine and browser acceptance. |
| Conversation delegation, result publication and linked follow-up (CHAT-06–08) | Product checks and real Deck conformance pass. | Installed actor, model and browser acceptance. |
| kagent as the agent engine (KINT-01–08) | Isolated local trials passed: a native Codex PR, the LangChain workload and a question round-trip. The trials were torn down afterwards. | Permanent installation (KINT-16), core failure cases (KINT-09), upgrade and rollback (KINT-10), cutover and removal (KINT-11–15), per [decision 0023](decisions/0023-focus-kagent-cutover.md). It is **not** the installed runtime. |

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
- The three-column Deck layout with a simple middle column.
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
