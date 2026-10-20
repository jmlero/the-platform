# Backlog

Last reviewed: 2026-10-05.

The order of work. The direction is in [VISION.md](VISION.md), and what works
today is in [status.md](status.md).

IDs are stable and never reused. States: `Open` means the behavior is missing,
`Verify` means it is built but the named acceptance proof is missing, `Later`
means work starts at its trigger, `Done` needs linked evidence, and `Retired`
means a decision made the item obsolete.

## Priority now

Marina's own platform API comes first, then the remaining platform work
(operator decisions, 2026-10-17), then the
[first milestone](VISION.md#direction): one real repository, one PRD born in the
chat, and agents landing its tasks on main without me touching the code.

The engine, the chat agent, native Codex and landing on main are accepted on
local Kind (KINT-09–11, KINT-16, CHAT-04–11, AUTO-02–06, AUTO-08, LAND-01, UI-01).
Next, in order:

1. **Marina's platform API** (MAR): Marina owns the API; Deck becomes a client.
2. **The platform** (PLAT). Its phases can run in any order.
3. **Landing and tests** (LAND-02–03, VER-01, TEST-01).
4. **PRDs from the chat** (PRD).
5. **The milestone proof** (FLOW-01).

## Now

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| KINT-16 | Done | Ops/Marina | Permanent pinned kagent/Substrate and bounded chat profile installed locally. A real model reply appeared in its Deck thread and remained readable after Deck restart; each attempt requires a fresh prepared turn. [Evidence](validation/kint-16-2026-09-26.md), [installation guide](../deploy/kubernetes/local-podman-kagent/README.md). |
| CHAT-04–10 | Done | Joint | The installed chat agent answers without write authority, asks a typed question, delegates a Ready task within a fixed budget, and reports the landed result in its originating thread, across restarts and lost responses; a second browser session saw the landed result. [Evidence](validation/chat-04-10-2026-09-28.md); earlier: [04](validation/chat-04-2026-09-23.md), [05](validation/chat-05-2026-09-23.md), [06–08](validation/chat-06-08-2026-09-25.md), [local draft and clarification](validation/chat-04-10-local-2026-09-27.md), [browser](validation/chat-04-06-browser-2026-09-27.md), [chat to landing](validation/chat-06-10-landing-2026-09-27.md). |
| KINT-09 | Done | Joint | The core live failure cases of decision 0023 passed on real Deck, Marina and kagent with repository work: lost create, send and publication responses; Marina, engine and worker restarts during work; cancellation during a model call, a tool call and a question; token and time budget exhaustion and lost usage; failed verification and CI. No duplicate work, commits or spend, and no false completion. [Evidence](validation/kint-09-core-2026-09-28.md); the earlier partial reports stay in `validation/`. |
| KINT-10 | Done | Ops/Marina | The pinned local release pair completed an active kagent model turn across image upgrade and restored the previous image with a retained data copy, preserving the exact message and usage. [Accepted local evidence](validation/kint-10-recovery-2026-09-26.md); [earlier failed transition](validation/kint-10-active-2026-09-26.md). Schema-aware application rollback passed under [KINT-17](validation/kint-12-17-2026-10-01.md#fresh-installation-upgrade-and-rollback). |
| KINT-11 | Done | Marina/Ops | New repository work runs on native Codex on kagent. All 28 Job-runtime attempts finished before the cutover, and an active attempt keeps its engine when the profile changes. [Evidence](validation/kint-11-land-01-2026-09-28.md). |
| KINT-12 | Done | Marina | Removed the custom Codex launcher, agent Jobs, retired LangChain workload and agent-only CLI/protocol paths. [Removal inventory](validation/kint-12-17-2026-10-01.md#removal-inventory). |
| KINT-13 | Done | Marina | Narrowed runtime/recovery code; retained fields serve active workflows, accounting or historical readers. Old artifacts still download with matching digests. [Retained-code rationale](validation/kint-12-17-2026-10-01.md#retained-runtime-and-journal-state). |
| KINT-14 | Done | Marina/Ops | Removed obsolete images, dependencies and configuration. Fresh application stores and retained-data upgrade use the native configuration. [Packaging](validation/kint-12-17-2026-10-01.md#packaging-and-maintenance-footprint), [installation drill](validation/kint-12-17-2026-10-01.md#fresh-installation-upgrade-and-rollback). |
| KINT-15 | Done | Joint | Final product/installation checks, real-process conformance, live repository/chat work and removal documentation passed. [Final acceptance](validation/kint-12-17-2026-10-01.md). |
| KINT-17 | Done | Joint | Event reordering, lease loss, context revocation, actor bypass denial, pinned template edits, artifact/cleanup faults, question expiry/replay, bounded lost-compute detection, explicit budget blockers, schema-gated rollback and local footprint recorded. [Failure matrix](validation/kint-12-17-2026-10-01.md#remaining-live-failure-matrix), [footprint](validation/kint-12-17-2026-10-01.md#local-resource-footprint). |
| LAND-01 | Done | Marina/Joint | Verified commits land on main by exact fast-forward after clean verification and required CI on that commit; ten local tasks landed on 28 September. The operator accepted Marina's exact-commit gate as the MVP bar; GitHub-side enforcement is LAND-04. `repository/v1` still opens a PR. Never force-push; only Marina holds Git write credentials. [Evidence](validation/kint-11-land-01-2026-09-28.md), [workflow](validation/land-01-workflow-2026-09-27.md). |
| UI-01 | Done | Deck | Three columns are built and [accepted on local Kind](validation/ui-01-2026-09-26.md): projects; a middle column with In progress, To do, and Recently completed; and chat. The last group becomes Recently landed with LAND-02. |

## Next

### Marina's platform API

Direction in [decision 0033](decisions/0033-marina-platform-api.md); outcomes
and acceptance in the [Marina platform API plan](plans/marina-platform-api.md).

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| MAR-00 | Done | Operator | Marina owns the API and execution state; Deck becomes a client; direct switch. [Decision 0033](decisions/0033-marina-platform-api.md). |
| MAR-01 | Verify | Joint | API design: OpenAPI, CRDs and fixtures in `marina/api/`, validated against both schemas with no engine or sandbox names. Waits for operator review. [Evidence](validation/mar-01-09-2026-10-18.md). |
| MAR-02–07 | Done | Marina | Kubernetes identity and Roles, Marina-owned runs on its own ledger, resource controller, factory choices in Workloads, Turns and Decisions, and the API sandbox every client tests against. [Evidence](validation/mar-01-09-2026-10-18.md). |
| MAR-08 | Done | Joint | Deck is a client: Runs for Ready tasks, Turns for chat, mirrored status and evidence; its provider routes and Marina's Deck client are removed. Browser tests pass against a real Marina. [Evidence](validation/mar-01-09-2026-10-18.md). |
| MAR-09 | Done | CLI | `platformctl` uses Marina's API; on Kind, `kubectl` creates, approves and reads a Run's evidence. [Evidence](validation/mar-01-09-2026-10-18.md). |
| MAR-10 | Verify | Joint | Deck and Marina switched together to the platform API on local Kind on 2026-10-18; old history stays readable, and a Run Deck created was cancelled while waiting. Four cutover bugs were fixed. Pending: one chat reply and one small task landing on main through the new API, which need the operator's OK for model spend and commits. [Evidence](validation/mar-10-11-2026-10-18.md). |
| MAR-11 | Verify | Marina | The [Marina chart](../marina/deploy/helm/marina/README.md) installs Marina without Deck, with the engine and traces as options; the local Marina now runs from it on its existing data, and a kubectl-created Run was accepted and cancelled. A fresh install without Deck passed on a new local Kind cluster with one script, and a kubectl Run reached plan approval there ([evidence](validation/marina-local-2026-10-20.md)). Pending: a kubectl-created Run landing on main with evidence (model spend and commits). [Evidence](validation/mar-10-11-2026-10-18.md). |
| MAR-12 | Later | Marina | A second runtime adapter behind the same port. Trigger: the operator wants to replace the sandbox. |

### The platform

Direction in [decision 0032](decisions/0032-agent-and-application-platform.md);
each task's outcome and acceptance is in the
[platform components plan](plans/platform-components.md).

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| PLAT-00 | Done | Operator | Direction recorded in [decision 0032](decisions/0032-agent-and-application-platform.md). |
| PLAT-05 | Done | Ops | Kyverno audits third-party namespaces and enforces digest pins, no privilege, memory limits, labels and NetworkPolicy presence in platform namespaces. [Evidence](validation/plat-05-09-2026-10-17.md#plat-05-kyverno). |
| PLAT-07 | Done | Ops | kgateway serves Deck at `https://localhost:8443` by TLS passthrough; kgateway and agentgateway stay separate. [Evidence](validation/plat-05-09-2026-10-17.md#plat-07-kgateway). |
| PLAT-06 | Verify | Ops/Marina | agentgateway serves the Responses API to Marina only and holds the provider key; Marina is configured to use it. A real chat turn and repository attempt through it are pending. [Evidence](validation/plat-05-09-2026-10-17.md#plat-06-agentgateway-for-models). |
| PLAT-08 | Verify | Marina/Ops | Allow-listed read-only MCP tools (Jaeger traces) through agentgateway and Marina's chat gateway. The installed Marina has this code since 2026-10-18; pending: enabling the tools in the private workflow and one live chat turn using a tool. [Evidence](validation/plat-05-09-2026-10-17.md#plat-08-mcp). |
| PLAT-09 | Verify | Marina/Ops | Collector and Jaeger run; Marina records one trace per task. The installed Marina has this code since 2026-10-18; pending: one task traced from claim to landing. [Evidence](validation/plat-05-09-2026-10-17.md#plat-09-traces). |
| PLAT-01–02, PLAT-04, PLAT-11, PLAT-17 | Open | Ops/Marina/Joint | Footprint baseline, Argo CD config, External Secrets, Prometheus and Perses (Jaeger is in place), and Knative apps deployed by Argo CD. |
| PLAT-03, PLAT-10, PLAT-12–16 | Later | Ops/Joint | The slot manifest, Langfuse (too heavy for the laptop now), experiments that compare components, SPIFFE/SPIRE workload identity, OpenFGA tool access and Keycloak sign-in. Standard benchmarks are not planned. Triggers are in the plan. |

### Feedback loop and review

The limits are in [decision 0020](decisions/0020-bounded-repair-policy.md) and
the review in [decision 0021](decisions/0021-semantic-review-gate.md). This work
starts after KINT-11 and uses kagent. Landing replaces the pull request in both:
a repair pushes new commits to the same task branch, and a failed check or
review is feedback for the next attempt.

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| AUTO-02 | Done | Marina | Attempts, frozen inputs and remaining budget survived a restart during native repair on local Kind. [Evidence](validation/auto-02-08-local-2026-10-01.md#failed-clean-check-diagnosis-repair-and-restart). |
| AUTO-03 | Done | Marina | A failed clean verification fed a saved diagnosis and different repair approach; fresh review and verification passed before landing. [Local evidence](validation/auto-02-08-local-2026-10-01.md#failed-clean-check-diagnosis-repair-and-restart). |
| AUTO-04 | Done | Marina | A real required GitHub check failure fed bounded native repair as untrusted evidence; fresh exact-head CI passed before landing. [Evidence](validation/auto-02-08-local-2026-10-01.md#real-required-github-ci-failure-and-repair). |
| AUTO-05 | Done | Joint | Failed checks and earlier reviews remain history; completion selects only the repaired commit's fresh review and checks. [Local evidence](validation/auto-02-08-local-2026-10-01.md#failed-clean-check-diagnosis-repair-and-restart). |
| AUTO-06 | Done | Joint | Native repair recovered across restart and lost create, commit, branch-update and evidence responses, with no duplicate task commits or extra budget. [Evidence](validation/auto-02-08-local-2026-10-01.md#failed-clean-check-diagnosis-repair-and-restart). |
| AUTO-08 | Done | Marina/Joint | Independent read-only review with a different model gates every exact patch before commit and rejects weakened/removed/skipped tests. A real skipped-test rejection fed saved diagnosis, a different repair approach, fresh passing review, 43 active passing tests and exact-head CI before one commit landed, within the original budget. [Repair acceptance](validation/auto-08-2026-10-03.md); [earlier rejection and unavailable-reviewer safety](validation/auto-02-08-local-2026-10-01.md#unavailable-reviewer-and-real-skipped-test-rejection). |

### Landing and tests

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| LAND-02 | Verify | Joint | Evidenced landing completes the task automatically and releases same-repository code prerequisites; Marina checks their commits on the execution base. Store, restart/replay and real-process checks pass. Updated local installation acceptance remains pending. [Evidence and limits](validation/land-02-03-ver-01-test-01-2026-10-03.md). |
| LAND-03 | Verify | Marina | Main movement admits a bounded rebase on a fresh branch, followed by fresh independent review, verification and exact-head CI. Conflicts block; no force-push. Deterministic recovery checks pass; live native rebase/conflict proof remains pending authorization for acceptance-repository commits. [Evidence and limits](validation/land-02-03-ver-01-test-01-2026-10-03.md). |
| VER-01 | Verify | Marina/Ops | Shared project-specific verifier/native Codex images package Node, Go, frozen dependencies and Chromium; runtime downloads are disabled. Installation and native-engine acceptance remain pending. [Packaging proof and limits](validation/land-02-03-ver-01-test-01-2026-10-03.md). |
| TEST-01 | Verify | Deck | Real Deck browser tests cover project creation, durable chat, task movement, landed results and legacy acceptance. All five, including PRD publication/task approval, acceptance and follow-ups, pass in local Chrome; CI installs Chromium during setup and runs them. A remote CI run remains pending an authorized source publication. [Initial evidence](validation/land-02-03-ver-01-test-01-2026-10-03.md#test-01-browser-and-frontend-checks), [PRD/browser follow-up](validation/prd-01-and-landing-checks-2026-10-03.md). |

### PRDs from the chat

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| CHAT-11 | Done | Marina/Joint | The chat agent reads the project's pinned repository through bounded read-only tools and uses the selected immutable project context. A live local reply identified code and the selected context marker; retained inputs, reply and usage survived restart without a task or Git change. [Evidence](validation/chat-11-2026-10-03.md), [decision](decisions/0027-read-only-repository-context-for-chat.md). |
| PRD-01 | Verify | Joint | Chat publishes an immutable PRD draft in its thread. Operator approval freezes its text, path, repository and budget into one publication task; Marina enforces exact document bytes through the existing review/test/CI/landing path. Restart, export/import, real-process workflow and browser checks pass. Live native publication remains pending the operator’s choice to keep GitHub unchanged. [Evidence](validation/prd-01-and-landing-checks-2026-10-03.md), [decision](decisions/0029-approved-prd-publication.md). |
| PRD-02 | Verify | Deck/Joint | A chat proposal names the landed PRD and ordered tasks with criteria. One operator approval creates all Ready tasks atomically with dependencies and frozen path/revision links. Replay, import and browser checks pass; installed native acceptance remains pending. [Evidence](validation/pending-prd-projects-2026-10-05.md), [decision](decisions/0030-ordered-prd-tasks-and-acceptance.md). |
| PRD-03 | Verify | Deck | The originating thread shows PRD publication, approved tasks and exact landed results. Operator acceptance binds the current result set; explicit follow-up requests create new proposals. Browser and persistence checks pass; installed acceptance remains pending. [Evidence](validation/pending-prd-projects-2026-10-05.md), [decision](decisions/0030-ordered-prd-tasks-and-acceptance.md). |

### Milestone proof and more projects

| ID | State | Owner | Outcome |
| --- | --- | --- | --- |
| FLOW-01 | Open | Joint | After AUTO-08, VER-01 and PRD-03: on local Kind with the acceptance repository, a chat becomes a PRD in the repository and its tasks land on main with unit and end-to-end tests. One failing check is repaired, and a restart during a task loses nothing. Store redacted evidence. Running this on a real project is the operator's own use, not agent work. |
| PROJ-01 | Verify | Joint | Explicit per-project Marina profiles bind each configured Deck project to its repository, checks and credential references. Gateways route by durable attempt identity; global admission keeps one active task. Source checks pass; installed two-project proof follows FLOW-01. On 2026-10-18 Marina's tick loop was fixed to advance every configured project's workflow, and runtime cleanup to use only the attempt's own project profile; before, a second project's tasks never progressed. [Evidence](validation/pending-prd-projects-2026-10-05.md), [decision](decisions/0031-explicit-per-project-profiles.md). |

## Later

| ID | State | Owner | Outcome and trigger |
| --- | --- | --- | --- |
| LAND-04 | Later | Ops/Joint | GitHub enforces the required check on main (branch protection or a ruleset), so an update of main without CI fails independently of Marina. Trigger: a repository whose plan supports it, or before other users. |
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
