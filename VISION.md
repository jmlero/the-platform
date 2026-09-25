# The Platform: current state and vision

**This is the reference document for The Platform.** It has two parts:

1. [Current state](#current-state): what works today, what exists but is not yet
   accepted, and what is not supported.
2. [Vision and goal](#vision-and-goal): where the platform is going and how
   success is measured.

Other documents link here instead of restating either part. Update this file in
the same change that ships, removes or re-scopes a capability, or changes the
direction.

Related documents:

- [ARCHITECTURE.md](ARCHITECTURE.md): how the system works (ownership,
  invariants, contracts, deployment and failure behavior).
- [TODO.md](TODO.md): the order of upcoming work and its acceptance status.
- [docs/decisions/](docs/decisions/): why each choice was made.
- [docs/README.md](docs/README.md): index of validation evidence.

---

## Current state

Last updated: 2026-09-25.

Deck and Marina run on local Kubernetes on the operator's laptop. They take one
authorized task to either a verified pull request for human review or a clear
blocker.

### What works

Accepted on the local Podman/Kind installation.

**Repository workflow** ([evidence](docs/validation/poc-mvp-2026-09-21.md))

```text
task and budget -> approved plan -> coding Job -> clean verification Job
                -> pull request -> required CI at the same head -> evidence in Deck
```

- One repository, one active task, one implementation attempt and one separate
  verification attempt.
- It ends at a PR for human review or at a blocker. Failed checks, a changed or
  closed PR, and requested changes block. Marina never merges or deploys.

**Diagnostic analysis workflow** ([evidence](docs/validation/agent-11-13-2026-09-22.md))

- A pinned LangChain worker analyzes a fixed, immutable fixture.
- It may use one governed, read-only context tool.
- An independent Go validator checks the report. It has no repository authority.

Both workflows share task authorization, immutable profiles and context, leases
and fences, budgets, cancellation, Kubernetes isolation, the model gateway,
artifacts and evidence rules. Cancellation completes only after the worker has
stopped. Without workflow configuration Marina runs in control-only mode.
Disabling new intake does not pause executions that have already started.

**Deck**: operator surface and ledger

- A project workspace that opens on the conversation: one channel per project,
  threads, a compact Backlog / Ready / In progress / Done list, a task drawer,
  project context and an archive.
- Promote a message to a Backlog draft, or save it as a reference note (manual).
- Versioned project knowledge and selected conversation excerpts, frozen for
  active runs. The repository association is validated by Marina.
- Human acceptance bound to a completed run. Same-project, acyclic dependencies
  are checked when finding ready work and at claim. A code prerequisite needs the
  operator's confirmation that its code is on the base branch.
- Threads linked to tasks, and recorded requests to the configured agent.
- Installed workload-profile discovery and idempotent invocation, and
  project- and capability-scoped service identities.
- The ledger records projects, tasks, budgets, runs, plans, decisions, commands,
  events, artifacts and evidence. Updates use versions, leases, fence tokens and
  idempotent replay.
- Digest-checked artifact downloads, live updates, project export/import, and a
  versioned OpenAPI v1 contract used by Marina and platformctl.
- The local installation opens without sign-in.

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

### Implemented but not yet accepted

| Capability | Where it stands | Still missing |
| --- | --- | --- |
| Conversational agent with no write authority (CHAT-04) | Deterministic checks pass. | Live engine and model acceptance. |
| Engine questions answered in the thread (CHAT-05) | Product checks and real-process conformance pass. | Installed engine and browser acceptance. |
| Conversation delegation, result publication and linked follow-up (CHAT-06–08) | Product checks and real Deck conformance pass. | Installed actor, model and browser acceptance. |
| kagent as the agent engine (KINT-01–08) | Isolated local trials passed: a native Codex PR, the LangChain workload and a question round-trip. The trials were torn down afterwards. | Failure matrix (KINT-09), upgrade and rollback (KINT-10), cutover and removal (KINT-11–15). It is **not** the installed runtime. |

### Not supported

- Pause, resume, replan, retry and reduce-authority commands. Deck can store them;
  Marina rejects them.
- Automatic repair after a failure. The policy is decided
  ([decision 0020](docs/decisions/0020-bounded-repair-policy.md)); the behavior
  is not built.
- Agent-created notes (LEAN-03) and reviewed/unread result state (INBOX-01–04).
- Breaking a broad outcome into focused child tasks (PREP).
- Independent semantic review before each commit. The policy is decided
  ([decision 0021](docs/decisions/0021-semantic-review-gate.md)); the behavior
  is not built.
- Merge, deployment and production remediation.
- More than one active task, more than one repository per workflow, arbitrary
  tools, and general agent hosting.
- Transparent recovery of an interrupted worker step. Active worker I/O can block
  across outages.
- High availability, backups and restore drills. Each service is a single replica
  on SQLite and a persistent volume.

A ledger record, schema or reusable foundation does not make a capability
supported.

### Environment and use

- Acceptance environment: local Podman/Kind (`kind-marina-local`) on the
  operator's laptop, at `https://localhost:8443`. The GCP cluster is authorized
  for tests; it is not an acceptance environment.
- Pilot services: GitHub/GHCR, OpenRouter, separate SQLite stores and artifact
  files on persistent volumes ([decision 0010](docs/decisions/0010-initial-installation-services.md)).
- First real use: the operator runs the Context Lens pilot personally through the
  platform ([pilot preparation](deploy/pilots/context-lens/README.md)). It is not
  agent backlog work; agents do not perform or track it.

---

## Vision and goal

### Goal

Make authorized agent work independent of a developer's laptop and chat session:
it runs on Kubernetes the operator controls and keeps work and context durable.
The operator can define bounded intent, step away, and return to an
evidence-backed account: what changed, what was verified, what failed, and where
human judgment is needed. The operator should always be able to tell what work
was authorized, what is happening, why it stopped, and whether the result is
supported by current evidence.

**Measure of success:** verified value per minute of human attention, without
added operational risk. Agent count, token count, commits, visible activity and
automation percentage are poor substitutes.

### North star: a dark software factory

> An evidence-gated system that turns bounded intent or trusted signals into
> verified outcomes within explicit policy, and escalates decisions outside its
> authority.

"Dark" means unattended. It does not mean opaque, unaccountable or unlimited.

```text
Human governance
intent -> architecture -> policy -> priorities -> exceptional decisions
                              |
                              v
Factory operation
sense -> plan -> execute -> verify -> deliver -> observe -> recover
  ^                                                           |
  +-----------------------------------------------------------+
```

Humans stay responsible for product direction, architecture, acceptable risk,
evidence standards, and decisions that must never be delegated. The factory owns
the repeatable engineering work those choices already authorize. Release work
only as fast as verification and review can absorb it.

Autonomy is granted per repository, workload, change class, environment and
risk. A bounded dependency update can run unattended and return as a verified
PR, or block if tests, CI, authority or budget fail. A migration that needs
product or data judgment returns to the operator. Correct escalation is
successful factory behavior.

| Factory concept | Platform component |
| --- | --- |
| Control room | Deck |
| Control system | Marina |
| Factory floor and work cell | Kubernetes and an isolated worker |
| Machine and tooling | Agent engine, harness and sandbox |
| Work order | Task, acceptance criteria, authority and budget |
| Materials | Pinned repository and immutable context |
| Inspection | Tests, CI, policy checks and independent validation |
| Safety interlocks | Identity, leases, fences, limits and cancellation |
| Finished output | Evidence-backed result for human review |

An individual agent is one replaceable worker inside the factory. A cron job,
terminal agent, generated patch or automatic merge does not by itself provide
durable authority, recovery and evidence.

### Product experience

Accepted in [decision 0019](docs/decisions/0019-channel-first-project-work.md).

- Deck and Marina feel like one conversation-first product. A project is one
  GitHub repository with one main channel. Topics use threads. The compact task
  list and project context sit inside the project.
- The human lifecycle is Backlog → Ready → In progress → Done. Awaiting review
  sits inside In progress.
- Agents may create Backlog drafts and maintain factual, source-bound notes.
  Standing instructions and accepted decisions change only with human approval.
- Moving a task to Ready authorizes execution within the configured limits and
  dependencies. Done requires human acceptance; an agent cannot accept its own
  output.
- A task is the intended outcome and its acceptance criteria. A run is one
  attempt with its plan, authority, events, decisions and evidence. Conversation
  references these records instead of copying them.
- The interface emphasizes what changes a decision: work awaiting approval,
  active work, blockers and reviewable results. Routine heartbeats stay out of
  the way.
- Start with one configured kagent agent and one OpenRouter model. Multiple
  harness/model profiles come later.

### Roles

| Component | Owns |
| --- | --- |
| Deck | Human intent, the backlog and work preparation (breakdown, acceptance criteria, context selection, authorization), decisions, and the operator-visible ledger. It never schedules Pods, checks out repositories, calls models or keeps an execution journal. |
| Marina | Governed workflows: authority, budgets, immutable context packages, source publication, independent verification, evidence admission, and recovery and reconciliation with Deck. |
| kagent (target engine) | Harness execution, engine interactions and runtime lifecycle ([decision 0017](docs/decisions/0017-kagent-core-engine.md)). |
| Git, CI, Kubernetes | Their own facts: source revisions, check results, workload state. |
| platformctl | A stateless client of Deck's public API. |

Project context is explicit and versioned. Deck owns authored knowledge and
selected sources; Git owns repository files. Marina resolves exact revisions into
an immutable package per run. Routine edits affect new work, never active work
([decision 0014](docs/decisions/0014-project-repository-and-knowledge.md)).

### Principles

1. **Durable control, disposable compute.** Identity, task state, context,
   authority, usage and evidence outlive Pods and model sessions. A restart is
   normal reconciliation, not a reason to create new work.
2. **Evidence over claims.** Completion needs evidence tied to the current
   source, inputs, profile and policy. The producing worker is never its only
   verifier. Missing or stale evidence waits, blocks or escalates.
3. **Autonomy as policy.** Every run has explicit limits for time, attempts,
   tokens or cost, concurrency and side effects. Context and tool output cannot
   expand them. Ambiguous or high-impact work returns to the operator.
4. **Explicit steering.** Operator actions become versioned commands or decisions
   with acknowledgement and history.
5. **Status from durable facts.** Running, waiting, blocked, stale, failed,
   cancelled and completed each mean something distinct.
6. **One owner per fact.** Projections keep provenance instead of creating a
   competing truth.
7. **Operator-controlled infrastructure.** Vanilla Kubernetes; each installation
   chooses its cluster, model provider, Git provider, registry, storage and
   network boundaries. Every external data destination stays explicit.
8. **Stable concepts above providers.** Work control, engines, runtimes, source
   control, context, policy, verification and delivery meet through narrow
   contracts. Agents are replaceable; the API does not depend on one harness or
   model. A new abstraction follows a proven second implementation.
9. **Portable and inspectable.** One process and SQLite per service until a
   measured need justifies more.

The platform concentrates on the harness, loop and factory layers while keeping
the model and context layers inspectable:

| Layer | Responsibility |
| --- | --- |
| Model | Produce one response under a defined protocol and budget. |
| Context | Supply the smallest relevant, versioned, provenance-rich information. |
| Harness | Give one worker its tools, sandbox, permissions and exit contract. |
| Loop | Persist work across attempts, feedback, restart, verification and escalation. |
| Factory | Coordinate many governed loops from intent to operational outcomes. |

### Direction

[TODO.md](TODO.md) holds the sequence and acceptance criteria. In outline:

1. **kagent becomes the core engine.** Cut over both workloads, then delete the
   Marina runtime code it replaces ([decision 0017](docs/decisions/0017-kagent-core-engine.md),
   KINT).
2. **Conversation to results.** An explicit delegation creates one authorized
   task; results return to the originating thread; feedback becomes a linked
   follow-up. Then agent-created drafts and notes, and unread result state
   ([decision 0009](docs/decisions/0009-asynchronous-agent-work.md), CHAT,
   LEAN-03, INBOX).
3. **Bounded repair and review.** At most three failures per task. Each repair
   starts from new failure evidence and tries a different approach
   ([decision 0020](docs/decisions/0020-bounded-repair-policy.md)). An
   independent reviewer checks every change against the acceptance criteria
   right before it is committed ([decision 0021](docs/decisions/0021-semantic-review-gate.md), AUTO).
4. **Work preparation.** Deck breaks broad outcomes into small, verifiable tasks
   with their own context and budgets. A read-only planning agent proposes; the
   human accepts ([decision 0018](docs/decisions/0018-deck-work-preparation.md),
   PREP).
5. **Later, each behind its own trigger:** a second workflow or concurrent work,
   one diagnosis-only production signal, one preview or canary delivery target,
   and authority beyond PR review for one measured low-risk change class.
   Multi-user permissions, federated identity and HA remain later decisions.
