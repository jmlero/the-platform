# Backlog

Last reviewed: 2026-09-25.

The order of work. The direction is in [VISION.md](VISION.md), and what works
today is in [status.md](status.md).

IDs are stable and never reused. States: `Open` means the behavior is missing,
`Verify` means it is built but the named acceptance proof is missing, `Later`
means work starts at its trigger, `Done` needs linked evidence, and `Retired`
means a decision made the item obsolete.

## Priority now

Everything serves the [first milestone](VISION.md#direction): one real
repository, one PRD born in the chat, and agents landing its tasks on main
without me touching the code.

1. **kagent for real, and the chat agent live** (KINT-16, then CHAT-04–10).
   kagent has only run as throwaway trials. A permanent installation turns on
   the chat agent, which is the main tool of the outer loop, and everything else
   builds on it.
2. **Repository work on native Codex** (KINT-09 and KINT-10, then KINT-11). Pass
   the core failure cases of [decision 0023](decisions/0023-focus-kagent-cutover.md),
   then send new repository work to kagent.
3. **Land on main instead of opening a pull request** (LAND-01). This is Marina's
   Git delivery code and does not depend on the engine, so it runs alongside 1
   and 2.
4. **The three-column Deck** (UI-01). Deck only; it runs alongside the rest.

After that, in order: the feedback loop and review (AUTO), PRDs from the chat
(PRD), and the milestone proof (FLOW-01). Removing the old runtime (KINT-12–15)
runs alongside AUTO.

## Now

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| KINT-16 | Done | Ops/Marina | Permanent pinned kagent/Substrate and bounded chat profile installed locally. A real model reply appeared in its Deck thread and remained readable after Deck restart; the next attempt-exclusive turn is prepared. Repository work remains on the current runtime. [Evidence](validation/kint-16-2026-09-26.md), [installation guide](../deploy/kubernetes/local-podman-kagent/README.md). |
| CHAT-04–10 | Verify | Joint | Built; product checks and real Deck conformance pass ([04](validation/chat-04-2026-09-23.md), [05](validation/chat-05-2026-09-23.md), [06–08](validation/chat-06-08-2026-09-25.md)). Remaining, in the browser on the installation from KINT-16: a model reply in the thread (04), a question answered in the thread (05), a request that becomes a task (06), results and blockers back in the thread (07), a follow-up task (08), restart and duplicate-request cases (09), and the whole chat-to-result flow (10). |
| KINT-09 | Open | Joint | The core live failure cases of decision 0023, on real Deck, Marina and kagent with repository work: duplicate and lost responses, restarts during work, cancel during a model call, a tool call and a question, budget exhaustion and lost usage, and failed verification or CI. No duplicate work, commits or spend, and no false completion. Partial evidence: [23–24 September](validation/kint-09-10-2026-09-24.md), [cancel while waiting](validation/kint-09-2026-09-25.md), [permanent-installation response loss and stale worker route](validation/kint-09-permanent-2026-09-26.md), [queued chat turn across Marina restart](validation/kint-09-10-permanent-2026-09-26.md), [native budget denial and model-call cancellation](validation/kint-09-native-permanent-2026-09-26.md), [native Marina restart with exact-run cancellation](validation/kint-09-native-restart-2026-09-26.md), [native controller restart and safe failure](validation/kint-09-gateway-restart-2026-09-26.md), [native one-cent cost admission](validation/kint-09-native-cost-2026-09-26.md), [native database restart with exact-run cancellation](validation/kint-09-native-db-restart-2026-09-26.md), [native worker restart with exact-run cancellation](validation/kint-09-native-compute-restart-2026-09-26.md), [real GitHub PR response recovery](validation/kint-09-pr-response-2026-09-26.md). |
| KINT-10 | Done | Ops/Marina | The pinned local release pair completed an active kagent model turn across image upgrade and restored the previous image with a retained data copy, preserving the exact message and usage. [Accepted local evidence](validation/kint-10-recovery-2026-09-26.md); [earlier failed transition](validation/kint-10-active-2026-09-26.md). Schema-aware downgrade and broader version changes remain KINT-17. |
| KINT-11 | Open | Marina/Ops | After 09–10, drain legacy executions and send new repository work to native Codex on kagent. An active attempt never moves between engines. |
| LAND-01 | Open | Marina/Joint | Replace the pull request ([decision 0022](decisions/0022-autonomous-software-factory.md)): after clean verification, push the exact commit to a task branch, wait for required GitHub CI on it, then fast-forward main to it. If main moved, block for now (LAND-03 adds rebase). Lost push or update responses reconcile without a second push. Never force-push; only Marina holds Git write credentials. Configure the acceptance repository so Marina may update main while branch protection still requires CI. Proof: product checks, conformance and one live task on local Kind. |
| UI-01 | Open | Deck | Three columns: projects; a very simple middle column with in progress, to do, and recently landed on main (recently completed until LAND-01); and the chat. Nothing more in the middle column for now. |

## Next

### Feedback loop and review

The limits are in [decision 0020](decisions/0020-bounded-repair-policy.md) and
the review in [decision 0021](decisions/0021-semantic-review-gate.md). This work
starts after KINT-11 and uses kagent. Landing replaces the pull request in both:
a repair pushes new commits to the same task branch, and a failed check or
review is feedback for the next attempt.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| AUTO-02 | Open | Marina | Persist attempts and the remaining budget across restarts. |
| AUTO-03 | Open | Marina | A failed clean verification feeds one repair attempt. A diagnosis and a different approach are recorded first; the new commit gets fresh verification. |
| AUTO-04 | Open | Marina | A failed required CI check on the task branch feeds a repair attempt the same way, as untrusted data. |
| AUTO-05 | Open | Joint | Earlier evidence stays as history; only evidence for the latest commit can land or complete the task. |
| AUTO-06 | Open | Joint | A repair loop recovers across restart with no duplicate commits or pushes and no extra budget. |
| AUTO-08 | Open | Marina/Joint | After 02–05, an independent reviewer passes every change right before commit. Only the exact reviewed patch is committed, the verdict is stored as evidence, and a failed review feeds the repair loop. The reviewer rejects changes that weaken, remove or skip tests. |

### Landing and tests

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| LAND-02 | Open | Joint | After LAND-01, a task completes on its own when its commit lands on main with evidence. Marina observes code prerequisites on main; the operator no longer confirms them. |
| LAND-03 | Open | Marina | After LAND-01, when main moved, rebase the task commit and pass both checks again. A conflict becomes repair feedback (AUTO) or a blocker. Never force-push. |
| VER-01 | Open | Marina/Ops | Each project's verification image carries what its unit and end-to-end tests need, such as browsers, prepackaged. Checks run without runtime downloads. |
| TEST-01 | Open | Deck | Browser end-to-end tests for Deck's main flows run in CI: open a project, chat, see a task move through the middle column, open its result. |

### PRDs from the chat

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| CHAT-11 | Open | Marina/Joint | The chat agent can read the project's repository at a pinned revision, plus the selected project context, without write access, so conversations and PRDs reflect the code. |
| PRD-01 | Open | Joint | After CHAT-04 and LAND-01, the chat agent drafts a PRD in a thread. After I approve it, Marina commits it to the project repository (default `docs/prd/`) through the landing path. |
| PRD-02 | Open | Deck/Joint | After PRD-01, the agent proposes top-level tasks from an approved PRD, with acceptance criteria and an order. One approval makes them Ready in that order. Each task links to the PRD's path and revision. |
| PRD-03 | Open | Deck | After LAND-02 and PRD-02, Deck shows each PRD with its tasks and what landed. I accept the PRD or ask for follow-ups in the chat. |

### Removing the old runtime

Required work, not optional cleanup; the [kagent plan](plans/kagent-integration.md)
lists what goes and what stays. A failed gate stops the dependent deletion; the
working release stays until the failure is fixed.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| KINT-12 | Open | Marina | After 11, delete the replaced Codex launch path, the retired LangChain workload, agent Job support, and unused worker entry and protocol branches. |
| KINT-13 | Open | Marina | After 12, remove duplicated runtime and journal machinery; keep workflow recovery, accounting and history readers. |
| KINT-14 | Open | Marina/Ops | After 12–13, remove unused images, dependencies, configuration, grants and installation branches. |
| KINT-15 | Open | Joint | After 14, verify the simplified build, a fresh install, and live repository and chat work. Publish the removal evidence and update the docs. |

### Milestone proof and more projects

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| FLOW-01 | Open | Joint | After AUTO-08, VER-01 and PRD-03: on local Kind with the acceptance repository, a chat becomes a PRD in the repository and its tasks land on main with unit and end-to-end tests. One failing check is repaired, and a restart during a task loses nothing. Store redacted evidence. Running this on a real project is the operator's own use, not agent work. |
| PROJ-01 | Open | Joint | After FLOW-01, every Deck project can run work on its own repository: Marina binds a repository, checks and credentials per project. Still one active task at a time. |

## Later

| ID | State | Owner | Outcome and trigger |
| --- | --- | --- | --- |
| KINT-17 | Later | Joint | The live failure cases that decision 0023 moved out of KINT-09: event reordering, lease loss and context revocation during a call, bypass attempts, template edits during a call, artifact storage failure, schema-aware rollback, and a full footprint measurement. Required before an independent cluster or other users. |
| LATER-01 | Later | Joint | Several tasks in parallel. Trigger: after FLOW-01, when one active task is the bottleneck. |
| LATER-06 | Later | Joint | Context and knowledge bases for agents in the outer loop, including notes that agents maintain. Trigger: after PRD-03. |
| LATER-07 | Later | Ops | Move to an independent Kubernetes cluster, in the cloud or on-premises. Trigger: FLOW-01 passes and the laptop limits daily use; needs KINT-17. |
| OPS-01 | Later | Ops | One consistent backup of both SQLite stores, WAL state, artifacts, identities and required private configuration. Trigger: backup recovery becomes a requirement. |
| OPS-02 | Later | Joint | After OPS-01, restore into an isolated installation without identity loss or duplicate external work. |
| OPS-04 | Verify | Joint | Rehearse operator, service, Git and model credential rotation with no secret leakage. Marina's GitHub token expires on 2026-12-20. |

## Completed milestones

| IDs | Result | Evidence |
| --- | --- | --- |
| KINT-01–08 | kagent pinned; lifecycle adapter, governed gateway and shared result boundary built; native Codex, the LangChain workload and a question round-trip passed isolated local trials. | [01–02](validation/kint-01-02-2026-09-22.md), [03](validation/kint-03-2026-09-22.md), [04](validation/kint-04-2026-09-22.md), [05](validation/kint-05-2026-09-23.md), [06](validation/kint-06-2026-09-23.md), [07–08](validation/kint-07-08-2026-09-23.md) |
| CHAT-01–03 | Message intent rules, persisted threads and explicit agent invocation. | [Rules](decisions/0009-asynchronous-agent-work.md#chat-01-conversation-intent-and-authorization), [CHAT-02](validation/chat-02-2026-09-23.md), [CHAT-03](validation/chat-03-2026-09-23.md) |
| LEAN-01–02 | Channel-first Deck: project channels, threads, a compact task list, manual draft promotion, human acceptance and dependency gates. | [Evidence](validation/deck-channel-first-2026-09-23.md) |
| AUTO-01, AUTO-07 | Repair limits and the independent review gate decided. | [Decision 0020](decisions/0020-bounded-repair-policy.md), [decision 0021](decisions/0021-semantic-review-gate.md) |
| INBOX-06 | Local Deck opens without sign-in. | [Evidence](validation/deck-local-access-2026-09-23.md) |
| FIX-01–06 | Entry-point budgets, readiness checks, channel recovery, CLI authentication and capability wording. | Product checks and contract conformance. |
| RUN-01–09, RUN-11–16, RUN-18 | Local installation and repository workflow, including restart, cancellation, failure, stale-head, outage and budget cases. | [Repository acceptance](validation/poc-mvp-2026-09-21.md) |
| OPS-03, OPS-05–06 | Certificate renewal, conservative artifact retention and the recovery runbook. | [Operations evidence](validation/first-release-operations-2026-09-17.md), [runbook](../deploy/kubernetes/first-release/OPERATIONS.md) |
| CTX-01–08 | Versioned project knowledge, selected conversation context, immutable packages, inspection and repository association. | [Decision](decisions/0014-project-repository-and-knowledge.md), [evidence](validation/ctx-02-08-2026-09-21.md) |
| AGENT-01–13 | Codex repository work and a pinned LangChain diagnostic workload on Marina's governed execution foundation. | [PoC](validation/agent-02-06-2026-09-21.md), [API/tool/context](validation/agent-07-10-2026-09-21.md), [MVP](validation/agent-11-13-2026-09-22.md) |
| VALUE-01 | Closed by the operator; human-effort and provider-billing measurement was declined. | [Closure record](validation/poc-mvp-2026-09-21.md#value-01-initial-measurement) |
| LATER-02 | A second harness completed through the shared platform controls. | [Two-workload proof](validation/agent-02-06-2026-09-21.md) |

## Retired

| IDs | Why |
| --- | --- |
| PREP-01–08 | Replaced by PRD-01–03 ([decision 0022](decisions/0022-autonomous-software-factory.md)). The earlier design is kept in [history](https://github.com/jmlero/the-platform/blob/83ff021/docs/plans/work-preparation.md). |
| LEAN-03 | Drafts from the chat are CHAT-06 and PRD-02; notes that agents maintain are LATER-06. |
| INBOX-01–04 | The middle column's "recently landed" list replaces an inbox (UI-01). |
| INBOX-05 | Replaced by TEST-01. |
| VALUE-02 | It compared the board with the chat; the board is gone. |
| LATER-03, LATER-04 | Deployment and production signals stay outside the platform (decision 0022). |
| LATER-05 | Superseded by decision 0022: agents land on main after the platform check and GitHub CI. |
| CLP-00–04 | The Context Lens pilot is the operator's own use of the platform, not agent work ([status](status.md#environment-and-use)). |
| RUN-10, RUN-17 | Removed from local acceptance by the operator. |
