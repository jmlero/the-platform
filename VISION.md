# Marina vision

## Purpose

Marina makes authorized agent work independent of a developer's laptop and local
chat session. It runs on Kubernetes controlled by the operator, preserves work
and context durably, and lets the operator disconnect and return to a verified
result or a focused question.

The longer-term goal is a governed software factory that can carry bounded work
through implementation, verification, delivery feedback, and recovery with less
continuous human coordination. The human remains responsible for product intent,
architecture, risk policy, and exceptional decisions.

The north star is a **dark software factory**: an always-on system that performs
normal authorized work without a person continuously operating it. “Dark” means
unattended, never opaque or uncontrolled. [DARK_FACTORY.md](DARK_FACTORY.md)
defines the term.

## Product experience

Deck and Marina should feel like one product with three views of the same work:

- **Board:** define outcomes, priorities, acceptance criteria, and budgets.
- **Conversation:** develop intent, select context, delegate deliberately, and
  answer focused questions.
- **Inbox:** find results awaiting review and decisions requiring attention.

The operator should be able to start from either a task or a project conversation,
leave, and continue later from another browser session. A direct instruction can
authorize work within existing project limits; discussion, quoted text, and a
casual agent mention do not. Missing intent produces clarification. Missing
authority produces a focused approval request.

Project context is durable and explicit. Deck owns authored instructions, goals,
selected documents, decisions, and chosen conversation excerpts. Git remains
authoritative for repository-owned files. Marina resolves exact revisions and
assembles an immutable context package for each run. Routine edits affect new
work; they do not silently change active work.

[Decision 0009](docs/decisions/0009-asynchronous-agent-work.md) records the
conversation and inbox direction. [Decision 0014](docs/decisions/0014-project-repository-and-knowledge.md)
records project knowledge and repository ownership.

## Current baseline

The supported Deck–Marina pair runs as separate HTTP/JSON services with separate
stores. It provides two bounded workloads:

1. A repository task runs one coding Job and one separate deterministic
   verification Job, publishes a pull request, observes required CI at the same
   head, and returns evidence to Deck.
2. A pinned LangChain diagnostic workload analyzes an immutable fixture, may use
   one governed read-only context tool, and returns an independently validated
   report without repository authority.

Both workloads share task authorization, immutable profiles, leases, fencing,
budgets, cancellation, Kubernetes isolation, the model gateway, artifacts,
diagnostics, and evidence rules. They passed local Podman/Kind acceptance; the
reports are indexed in [docs/README.md](docs/README.md).

The current repository workflow is deliberately one pass. Failed checks,
requested changes, a changed PR head, or interrupted worker I/O can block.
Cancellation is supported and waits for termination. Pause/resume, transparent
step recovery, automatic revisions, semantic review, merge, deployment, and
production remediation are future work.

## Product principles

### Operator-controlled infrastructure

Marina targets vanilla Kubernetes APIs rather than a mandatory hosted control
plane. Installations choose their cluster, model provider, Git provider, registry,
storage, DNS, and network boundaries. Running on private Kubernetes does not by
itself keep source private; every external data destination must remain explicit.

The current pilot uses GitHub/GHCR, OpenRouter, SQLite, and persistent volumes as
recorded in [decision 0010](docs/decisions/0010-initial-installation-services.md).
Those are installation choices rather than permanent product dependencies.

### Durable control, disposable compute

Agent identity, task state, context, authority, checkpoints, usage, and evidence
outlive Pods and model sessions. Workers are replaceable compute with short-lived
credentials. A restart is a normal reconciliation path, not a reason to create
new work blindly.

### Evidence over claims

An agent saying “done” is insufficient. Completion requires evidence tied to the
current source, input, profile, and policy. Deterministic checks or independent
validation remain separate from the producing worker. Missing or stale evidence
waits, blocks, or escalates.

### Autonomy as policy

Authority is explicit per project, task, workload, environment, and risk. Every
run has limits for time, attempts, tokens or cost, concurrency, and allowed side
effects. Context and tool output cannot expand those limits. Ambiguous or
high-impact work returns to the operator.

### Stable concepts above providers

Kubernetes is the execution fabric, not the public product model. Work control,
agent harnesses, runtimes, source control, context sources, policy, verification,
delivery, and operational signals meet Marina through narrow contracts. New
abstractions follow a proven second implementation rather than precede it.

### One owner for each fact

Deck owns human intent and the operator-visible ledger. Marina owns execution and
recovery. Git, CI, Kubernetes, and other external systems own their observations.
Projections preserve provenance rather than creating competing truth.

## Platform model

The useful system has five related layers:

| Layer | Responsibility |
| --- | --- |
| Model | Produce one response under a defined protocol and budget. |
| Context | Supply the smallest relevant, versioned, provenance-rich information. |
| Harness | Give one worker its tools, sandbox, permissions, and exit contract. |
| Loop | Persist work across attempts, feedback, restart, verification, and escalation. |
| Factory | Coordinate many governed loops from intent to operational outcomes. |

Marina concentrates on the harness, loop, and factory layers while keeping the
boundaries between all five inspectable.

[Decision 0015](docs/decisions/0015-marina-agent-platform.md) accepts Marina as a
bounded Kubernetes agent platform, with software engineering as the first
workflow. [Decision 0016](docs/decisions/0016-workload-invocation-and-context-tool.md)
defines installed profile discovery, invocation, and governed context access.
The two-workload MVP proves shared controls; it does not claim a general-purpose
agent service.

## Direction

The next increments are:

1. Link project conversations to durable tasks and results.
2. Add persistent reviewed/unread result state.
3. Add bounded repair attempts that consume current verification or review
   feedback and require fresh evidence.
4. Select one production signal and one non-production delivery target only
   after repository work is reliable.
5. Consider authority beyond PR review only for a measured, low-risk change
   class with explicit human-only boundaries.

The active, acceptance-based sequence is [TODO.md](TODO.md). New workflows should
reuse the existing execution foundation until a concrete need proves otherwise.

## Measure of success

Optimize for verified value per minute of human attention without increasing
operational risk. Agent count, token count, commits, visible activity, and raw
automation percentage are poor substitutes.

Marina succeeds when the operator can define bounded intent, step away, and
return to an evidence-backed account of what changed, what was verified, what
failed, and where human judgment is required.
