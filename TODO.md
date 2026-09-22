# The Platform backlog

Last reviewed: 2026-09-22.

The supported release runs one bounded repository task or one pinned diagnostic
analysis workload on local Kubernetes and returns verified evidence or a blocker.
Repository work stops at a pull request for human review. The next product focus
is durable conversation and result-return flows, followed by bounded repair
loops.

IDs are stable and are never reused. `Open` means behavior is missing, `Decide`
means a product choice is required, `Verify` means implementation exists but the
named acceptance proof is missing, and `Later` means work starts only after its
stated trigger. A row becomes `Done` only with linked evidence.

## Current priorities

### Conversation and delegation

These tasks implement [decision 0009](docs/decisions/0009-asynchronous-agent-work.md)
using the existing Deck channels, task ledger, and explicit project context.

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
