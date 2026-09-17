# Deck Vision

## Purpose

Deck is the lightweight operator surface for authorized agent work, usable locally and on infrastructure controlled by its operator.

It gives people a clear place to define outcomes and approval boundaries, lets agents claim structured work, and keeps every plan, decision, event, artifact, and result visible and auditable.

The first usable release focuses on task authorization, decisions, results ready
for review, and exceptions. It forms one coherent installation with Marina while
retaining its separate service and public API. General project-management and chat
expansion are deferred under [decision 0007](docs/decisions/0007-sovereign-first-usable-release.md).

Deck is not intended to be a "main brain" that understands or executes everything. It is the shared contract between humans and replaceable agent runtimes: a dependable place to coordinate, observe, steer, and verify work.

## Supported release boundary

Deck and Marina run as separate services over HTTP/JSON. The current repository
workflow supports cancellation, one implementation and one separate deterministic
verification attempt, and ends at a PR ready for human review or a blocker.
Deck's broader command ledger does not implement pause/resume in Marina.
Headless/in-process packaging, automatic CI/review revisions, independent semantic
review, merge and deployment authority remain future work. The broader controls
and integrations below describe product direction, not supported runtime behavior.

## Proposed way of working

Deck and Marina should feel like one product: define work on a board or discuss it in a project web conversation, delegate to a configured agent, and return later to progress, a result, or a focused question. Marina executes the work on operator-controlled Kubernetes infrastructure. Deck keeps the task, conversation, decisions, and result accessible across devices.

The board organizes work; conversation develops intent and supports delegation; an inbox brings back results awaiting review and decisions requiring attention. All three refer to the same task and history. Operators should be able to use either a task or a conversation as their starting point.

An explicit request such as `@codex implement the agreed fix` should be able to initiate authorized work with the relevant discussion and project context. The resulting task should expose the interpreted outcome and scope without requiring duplicate data entry. Questions, ordinary mentions, and quoted instructions do not grant coding authority. Further approval is needed only for missing authority; unclear intent requires clarification.

Project knowledge and decisions should be reusable across tasks. The agent should receive relevant context without the operator rebuilding it in every conversation. Updates should highlight meaningful progress, actionable questions, and reviewable results, while detailed activity remains available on demand.

The accepted starting point is one GitHub repository per project. Deck should
provide repository association and a persistent project knowledge base for
custom instructions, goals, selected documents, and approved decision references.
Marina owns the repository connection and assembles the exact context each task
receives; Git remains authoritative for repository-owned files. These additions
are pending implementation under [decision 0014](docs/decisions/0014-project-repository-and-knowledge.md).

This target is described in [decision 0009](docs/decisions/0009-asynchronous-agent-work.md). Automatic conversational dispatch remains proposed behavior. The current release still uses explicit task authorization and channel promotion under decision 0007; this vision update does not claim that mention-triggered execution is implemented.

## Product principles

### The board is a view, not the data model

Kanban and backlog views should make work easy to understand, but agent execution requires more precise concepts: task claims, leases, dependencies, run attempts, plan revisions, approvals, heartbeats, failures, and completion evidence.

### Live status must be truthful

An agent is shown as active only when it holds a valid lease and reports heartbeats or events. The interface should distinguish running, waiting, blocked, stalled, failed, and completed work rather than presenting decorative activity.

### Human steering is explicit

Feedback should become an actionable, recorded instruction such as approve, reject, pause, cancel, replan, reassign, or answer a decision request. Agents must acknowledge instructions, and plan changes should create new revisions rather than overwrite history.

### Completion requires evidence

An agent declaring a task complete is not sufficient. A completed run should include artifacts and evidence tied to the task's acceptance criteria, such as test results, patches, reports, or review notes.

### Agents are replaceable

Deck should expose a small, stable protocol through an API, CLI, and eventually an MCP adapter. Agent runtimes remain outside the core application so that different tools and models can participate without becoming hard dependencies.

### Local first, portable by design

The first deployment target is one reliable application process that works locally or in a container. The application should be easy to back up, move, inspect, and restart. Kubernetes support should not require Kubernetes-specific architecture in the core product.

### Safe autonomy has boundaries

Every run should support limits for time, attempts, cost or token usage, and permissions. Secrets must not be stored in task descriptions or activity logs. All meaningful state changes should be auditable.

## Core experience

Deck should provide:

- A backlog and Kanban-style board for organizing work.
- Structured tasks with an outcome, context, constraints, acceptance criteria, priority, dependencies, and budget.
- Atomic task claiming so that agents cannot unknowingly duplicate work.
- Versioned plans that can require human approval before execution.
- A focused inbox for decisions that are blocking agents.
- A live timeline driven by agent heartbeats and execution events.
- Controls to pause, cancel, replan, reassign, retry, or resume work.
- Artifacts and verification evidence attached to each run.
- A durable history of plans, feedback, decisions, attempts, and outcomes.

## Task lifecycle

The initial lifecycle is:

```text
Backlog -> Ready -> Planning -> Awaiting decision -> Running -> Verifying -> Done
                         \-> Blocked / Failed / Cancelled
```

A task and a run are separate concepts. A task describes the desired outcome; a run records one attempt by an agent. A task can therefore retain multiple failed, cancelled, or superseded runs without losing history.

## Core model

The minimum domain model consists of:

- **Project**: a collection of related work.
- **Task**: the durable definition of an intended outcome.
- **Run**: one execution attempt for a task.
- **Agent**: an identified participant with capabilities and current presence.
- **Lease**: a time-limited, exclusive claim on work.
- **Plan revision**: a versioned proposal for how a run will proceed.
- **Decision request**: a structured question requiring human input.
- **Event**: an immutable record of progress or a state transition.
- **Artifact**: evidence or output produced by a run.

## Initial architecture

The product should begin as one application process with:

- A simple, polished web interface.
- SQLite as the canonical local database.
- Markdown task descriptions combined with structured metadata.
- An append-only activity history.
- Server-sent events or WebSockets for live updates.
- A versioned agent-facing HTTP API and a separate operator CLI; an agent MCP adapter remains future work.
- Artifact metadata and authenticated downloads; Marina owns workflow artifact bytes on separate storage.
- Deterministic export and import using readable Markdown and JSON.

```text
Human -- Web UI ---------+
                         +-- Deck control plane
Agents -- API/CLI/MCP ---+           |
                                     +-- SQLite
                                     +-- artifacts and exports
```

Agents should use validated application commands rather than directly editing the database or operational files. Human-readable exports preserve portability without sacrificing safe concurrent coordination.

## First release

The first release should prove one complete workflow:

1. A human creates a task with an outcome, constraints, acceptance criteria, priority, and budget.
2. One agent atomically claims the task with a lease.
3. The agent submits a versioned plan.
4. The human approves the plan or requests changes; the current Marina workflow blocks on a change request, preserving the decision for follow-up work.
5. The agent emits heartbeats and progress events while executing.
6. Marina completes with artifacts and verification evidence for a PR ready for human review, or reports a blocker. Merge/deploy authority stays with the human.
7. The complete state and history survive an application restart.

## Initial non-goals

The first release will not attempt to provide:

- Multi-tenant or sophisticated team permissions.
- A distributed scheduling system.
- Autonomous decomposition of large goals into task graphs.
- A Kubernetes operator.
- A general-purpose agent execution runtime.
- A plugin marketplace.
- Elaborate animated representations of agent activity.

These capabilities can be evaluated after the core human-agent contract has proven useful.

## Product differentiation

Backlogs and boards are familiar entry points, not the primary differentiation. Deck's value comes from making agent work interruptible, observable, and verifiable through:

- Plan review before execution.
- Structured human decisions.
- Trustworthy live run state.
- Explicit steering controls.
- Evidence-based completion.
- A complete, durable audit trail.
- Agent-neutral integration surfaces.

The product succeeds when a human can understand what every agent is doing, why it is doing it, what it needs, and whether the result is actually complete.
