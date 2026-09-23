# The Platform backlog

Last reviewed: 2026-09-22.

The supported release runs one bounded repository task or one pinned diagnostic
analysis workload on local Kubernetes and returns verified evidence or a blocker.
Repository work stops at a pull request for human review. The immediate engine
priority is integrating kagent and removing redundant Marina execution code.
Conversation and result-return flows should reuse that engine, followed by bounded
repair loops. The current supported runtime remains in place until migration gates pass.

IDs are stable and are never reused. `Open` means behavior is missing, `Decide`
means a product choice is required, `Verify` means implementation exists but the
named acceptance proof is missing, and `Later` means work starts only after its
stated trigger. A row becomes `Done` only with linked evidence.

## Current priorities

### Selected value pilot: Context Lens

The operator selected Context Lens and confirmed ubahndoku is completed.
The [assessment](docs/pilot-project-assessment-2026-09-22.md) and
[pilot preparation](deploy/pilots/context-lens/README.md) define the bounded
report/export/comparison increment. It can use the supported repository workflow;
this does not claim the planned PREP or kagent capabilities are already available.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| CLP-00 | Open | Operator/Ops | Bootstrap Context Lens CI, install/pin its offline worker and trusted checks, connect exact repository/context, and authorize only the selected task. [Offline preparation passed; CI and live setup remain](docs/validation/context-lens-pilot-preparation-2026-09-22.md). |
| CLP-01 | Open | Pilot | After 00, implement the versioned privacy-safe report serializer/validator through Marina and return an independently verified PR. Deck draft: `tsk_c723190cc08ce495d5d1`. |
| CLP-02 | Open | Pilot | After reviewed integration of 01, export the explicitly selected snapshot without private transcript/metadata leakage, preserving pause and error behavior. |
| CLP-03 | Open | Pilot | After reviewed integration of 02, compare two valid reports locally with accurate metrics and explicit accounting limitations. |
| CLP-04 | Open | Joint | After 01–03, record actual use, accepted results, blockers/rework and context continuity; distinguish product value from successful task execution. |

### Kagent integration and Marina simplification

[Decision 0017](docs/decisions/0017-kagent-core-engine.md) establishes the target:
Deck remains the interface, Marina owns governed workflows, and kagent becomes
the core agent engine. The [implementation and removal plan](docs/kagent-integration-plan.md)
contains dependencies, acceptance criteria, concrete deletion targets and rollback.
KINT-01/02 passed the isolated local feasibility trial; the trial was removed
after teardown proof. KINT-03/04 add the governed adapter and gateway; KINT-05
adds the shared result boundary. KINT-06 passed one native repository pilot;
the two-workload cutover remains planned.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| KINT-01 | Done | Marina/Ops | Pinned source/charts/ARM64 images/APIs and recorded ownership, removal gates and baseline. [Lock and inventory evidence](docs/validation/kint-01-02-2026-09-22.md#kint-01-lock-and-ownership). |
| KINT-02 | Done | Ops | Native Codex snapshot/restore, durable instance identity, isolation, resource sample and safe teardown passed on local Kind. [Live evidence and limits](docs/validation/kint-01-02-2026-09-22.md). |
| KINT-03 | Done | Marina | Pinned lifecycle adapter, durable revision/A2A correlation, no-resend recovery, observed cancellation and artifact cleanup. [Deterministic checks and isolated local recovery proof](docs/validation/kint-03-2026-09-22.md); active workloads remain unchanged. |
| KINT-04 | Done | Marina | Governed admission and attempt-scoped model/tool/source access, with real local Substrate HTTPS injection, credential rotation, cached-credential cancellation/expiry, denied bypasses and durable accounting. [Checks, live evidence and safe teardown](docs/validation/kint-04-2026-09-22.md); workload migration remains pending. |
| KINT-05 | Done | Marina | Shared bounded result validation, digest-bound kagent artifact translation, and agent-free clean verification. [Deterministic checks and real-process conformance](docs/validation/kint-05-2026-09-23.md); installed workload migration remains pending. |
| KINT-06 | Done | Marina | One Deck-authorized native Codex task produced [verified PR #13](docs/validation/kint-06-2026-09-23.md) on local Kind: separate clean verification and required CI passed at the exact head; the PR remains open and unmerged. The pilot was torn down after proof. |
| KINT-07 | Open | Marina | After 02–05, run the pinned LangChain diagnostic workload through kagent BYO with unchanged evidence and authority rules. |
| KINT-08 | Open | Joint | After 03–04, project engine state and bridge exact pending questions/answers without a second conversation runtime or duplicate results. |
| KINT-09 | Open | Joint | After 06–08, pass both workloads and the mandatory live failure matrix through real Deck/Marina/kagent. |
| KINT-10 | Open | Ops/Marina | After 02 and 06–07, prove installation upgrade, credential handling, retained history and data-preserving rollback; measure full footprint. |
| KINT-11 | Open | Marina/Ops | After 09–10, drain legacy executions and cut over both newly authorized profiles to kagent. |
| KINT-12 | Open | Marina | After 11, delete replaced Codex/Python launch paths, agent Job support and unused worker entry/protocol branches. |
| KINT-13 | Open | Marina | After 12, remove duplicated runtime/journal machinery while retaining workflow recovery, accounting and historical inspection. |
| KINT-14 | Open | Marina/Ops | After 12–13, remove unused images, dependencies, configuration, grants and installation branches; retain necessary verification/BYO code only. |
| KINT-15 | Open | Joint | After 14, verify the final simplified build, fresh install and live workflows; publish removal evidence and update supported-behavior documentation. |

KINT-12–14 are required delivery work, not optional cleanup. A failed integration
gate stops dependent cutover/deletion. Retain the working release while resolving
the failure; do not leave a permanent dual-engine installation by default.

### Backlog refinement and focused task preparation

[Decision 0018](docs/decisions/0018-deck-work-preparation.md) establishes Deck's
ownership of preparing small tasks and selecting their context. Marina runs
planning assistance and governed execution. The
[implementation design](docs/work-preparation.md) gives detailed acceptance
criteria. Manual Deck work can proceed alongside KINT; new planning execution
follows KINT-15. Start with one parent level and sequential children, preserving
the human-review PR boundary. All PREP behavior is planned.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| PREP-01 | Open | Deck/Joint | Define additive contracts and persist versioned work packages, breakdowns and child identities; preserve migration/export/import and reject stale edits or cyclic dependencies. |
| PREP-02 | Open | Deck | After 01, support manual breakdown and atomic acceptance with criterion coverage, focused briefs and idempotent child creation. |
| PREP-03 | Open | Joint | After 02, select and inspect per-child context, preserve mandatory guidance, and bind exact prerequisite inputs without mutating active work. |
| PREP-04 | Open | Joint | After 02–03, enforce aggregate allocations, authorization, sequential release and review capacity at every claim path, including cancellation and ambiguous usage. |
| PREP-05 | Open | Marina/Joint | After 03–04 and KINT-15, run a bounded read-only planning profile that proposes validated breakdowns without self-authorization or repository mutation. |
| PREP-06 | Open | Joint | After 04–05, reconcile dependency evidence, parent acceptance/cancellation and usage; never treat an unmerged PR as an available code prerequisite. |
| PREP-07 | Open | Joint | After 01–06, pass product checks and real-process conformance for preparation, dispatch, budgets, immutable context and recovery failure cases. |
| PREP-08 | Open | Joint | After 07, prove the broad-outcome-to-focused-results flow in the local browser/Kind installation, including restart, and record redacted evidence. |

### Conversation and delegation

These tasks implement [decision 0009](docs/decisions/0009-asynchronous-agent-work.md)
using the existing Deck channels, task ledger, and explicit project context.
CHAT-04/05/07 reuse KINT's engine and interaction integration; their full product
acceptance remains separate. Intent/UI design and independent Deck work can
continue while the engine migration is underway.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| CHAT-01 | Decide | Joint | Specify when a message is discussion, clarification, delegation, or result feedback, with examples for missing intent and authority. |
| CHAT-02 | Open | Deck | Persist task-linked conversation threads with stable identity across devices. |
| CHAT-03 | Open | Deck | Add a thread view and explicit configured-agent invocation. |
| CHAT-04 | Open | Marina | Run a bounded conversational agent with no source-write authority. |
| CHAT-05 | Open | Joint | Persist clarification questions and resume the correct conversation from their answers. |
| CHAT-06 | Open | Joint | Convert one explicit delegation into one idempotent, authorized task. |
| CHAT-07 | Open | Marina | Return milestones, blockers, results, and evidence to the originating thread once. |
| CHAT-08 | Open | Joint | Turn result feedback into a linked follow-up task without rewriting completed history. |
| CHAT-09 | Verify | Joint | Pass dispatch, clarification, duplicate-request, restart, and result-publication conformance through real Deck. |
| CHAT-10 | Verify | Joint | Complete the conversation-to-result workflow across two browser sessions on the selected installation. |

### Inbox and browser experience

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| INBOX-01 | Open | Deck | Persist reviewed/unread state independently of task completion. |
| INBOX-02 | Open | Deck | Keep new results visible until reviewed while decisions remain in attention. |
| INBOX-03 | Open | Deck | Link board, thread, decision, and result views to the same task and run. |
| INBOX-04 | Open | Deck | Summarize meaningful changes since the last visit without promoting routine heartbeats. |
| INBOX-05 | Verify | Deck | Cover login, task creation, approval, cancellation, reconnect, and evidence access in the desktop browser. Add conversation coverage after CHAT-03. |

### Bounded repair loops

Preserve human merge and deployment authority. A new revision must receive fresh
verification and current-head evidence.
Implement new engine-dependent repair behavior after KINT-15 using kagent;
AUTO-01 and AUTO-07 policy decisions can be specified earlier.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| AUTO-01 | Decide | Marina | Define retryable failures, limits, budget reuse, and escalation for one repository workflow. |
| AUTO-02 | Open | Marina | Persist revision attempts and remaining budgets across restart. |
| AUTO-03 | Open | Marina | Feed one failed verification result into a separately verified repair attempt. |
| AUTO-04 | Open | Marina | Feed current-revision CI or review feedback into a bounded repair attempt as untrusted data. |
| AUTO-05 | Open | Joint | Keep old evidence as history while requiring fresh evidence for the new head. |
| AUTO-06 | Verify | Joint | Recover one repair loop across restart without duplicate commits, PRs, or excess budget. |
| AUTO-07 | Decide | Joint | Define the first independent semantic-review gate and escalation rule. |

### Operations and measurement

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| OPS-01 | Later | Ops | When backup recovery becomes a requirement, create one consistent backup of both SQLite stores, WAL state, artifacts, identities, and required private configuration. |
| OPS-02 | Later | Joint | After OPS-01, restore into an isolated installation without identity loss or duplicate external work. |
| OPS-04 | Verify | Joint | Rehearse operator, service, Git, and model credential rotation with no secret leakage. |
| VALUE-02 | Open | Joint | After CHAT-10, compare board and conversational delegation for repeated context, intervention, result quality, and human effort. |

## Later decisions

| ID | State | Trigger |
| --- | --- | --- |
| LATER-01 | Later | Pilot evidence shows a need for a second project workflow or concurrent work. |
| LATER-03 | Later | Repository work is reliable enough to select one diagnosis-only production signal. |
| LATER-04 | Later | A concrete preview/canary target and rollback rule are available. |
| LATER-05 | Later | Pilot evidence supports considering authority beyond PR review for one low-risk change class. |

## Completed milestones

| IDs | Result | Evidence |
| --- | --- | --- |
| FIX-01–06 | Entry-point budgets, readiness checks, channel recovery, CLI authentication, and capability wording completed. | Current product checks and contract conformance. |
| RUN-01–09, RUN-11–16, RUN-18 | Local installation and repository workflow passed, including restart, cancellation, failure, stale-head, outage, and budget cases. RUN-10 and RUN-17 are retired. | [Repository acceptance](docs/validation/poc-mvp-2026-09-21.md) |
| OPS-03, OPS-05–06 | Certificate renewal, conservative artifact retention, and recovery runbook completed. | [Operations evidence](docs/validation/first-release-operations-2026-09-17.md), [runbook](deploy/kubernetes/first-release/OPERATIONS.md) |
| CTX-01–08 | Versioned project knowledge, selected conversation context, immutable packages, inspection, and repository association completed. | [Decision](docs/decisions/0014-project-repository-and-knowledge.md), [evidence](docs/validation/ctx-02-08-2026-09-21.md) |
| AGENT-01–13 | Codex repository work and a pinned LangChain diagnostic workload share Marina's governed execution foundation. | [PoC](docs/validation/agent-02-06-2026-09-21.md), [API/tool/context](docs/validation/agent-07-10-2026-09-21.md), [MVP](docs/validation/agent-11-13-2026-09-22.md) |
| VALUE-01 | Closed by the operator; representative human-effort and provider-billing measurement was declined. | [Closure record](docs/validation/poc-mvp-2026-09-21.md#value-01-initial-measurement) |
| LATER-02 | A second harness completed through the shared platform controls. | [Two-workload proof](docs/validation/agent-02-06-2026-09-21.md) |

Completed implementation detail belongs in source, decisions, and validation
records rather than an ever-growing backlog. See [docs/README.md](docs/README.md)
for the maintained evidence map.
