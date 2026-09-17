# The Platform — implementation gaps and TODO

Reviewed against source code and the product visions on **2026-09-17**.

**Goal:** use Deck to define, discuss, and delegate software work; let Marina run it on operator-controlled Kubernetes; return from any device to useful results or focused questions.

This replaces the historical milestone checklist. Completed foundations are summarized below; the tables contain remaining work. [Decision 0007](docs/decisions/0007-sovereign-first-usable-release.md) still governs the first release: a bounded repository fix ends at a **PR ready for human review**, or a truthful blocker. The board, conversation, and inbox target in [decision 0009](docs/decisions/0009-asynchronous-agent-work.md) is proposed next work, not implemented behavior or a change to release scope.

## What exists and what is missing

| Area | Implemented evidence | Gap to the vision |
| --- | --- | --- |
| Deck work control | Separate SQLite ledger; scoped service authentication; operator sessions; tasks, plans, decisions, commands, evidence, export/import. See [store](deck/internal/store/) and [HTTP API](deck/internal/httpapi/). | Extend the existing ledger for conversations and attention state; preserve one owner for each fact. |
| Marina durability | Persisted claims, leases, task snapshots, execution checkpoints, budget accounting, recovery, and ordered publication. See [reconciliation](marina/internal/reconcile/) and [journal](marina/internal/journal/sqlite/). | Validate recovery with real workers and the selected installation; current protocol tests use controlled dependencies. |
| Repository execution | Configured Codex worker, Kubernetes adapter, GitHub/GHES publication, a separate clean verification Job, PR/CI observation, and current-revision evidence. See [workflow](marina/internal/flow/), [worker](marina/internal/worker/), and [real-process tests](marina/internal/deck/real_workflow_test.go). | One configured repository and one active task; one implementation plus one verification attempt. Failed checks or requested changes block. Automatic revisions and independent semantic review are absent. |
| Authority and data boundaries | Explicit Git/model destinations, attempt credentials, live authority checks, token/cost reservations, isolated workers, and authenticated artifact downloads. See [worker gateway](marina/internal/workerapi/) and [installation](deploy/kubernetes/first-release/). | OpenRouter and GitHub/GHCR are selected for the pilot; configure and validate the chosen model, credentials, and actual network boundaries. Storage stays on Kubernetes volumes; backups are deferred. |
| Board and results | Task creation, authorization, board, decisions, cancellation, PR/evidence links, and attention/active/results queues. See [web components](deck/web/src/lib/components/) and [queue logic](deck/web/src/lib/work-queues.ts). | No persistent reviewed/unread result state. No complete browser workflow test covers delegation through return to a result. |
| Conversations | Persistent paginated channel messages, live/reconnect refresh, canonical references, and manual promotion with the shared task-budget form. See [channel storage](deck/internal/store/channels.go) and [channel UI](deck/web/src/lib/components/ChannelsView.svelte). | No conversational agent, mention dispatch, task threads, or automatic result replies. |
| Reusable context | Versioned context-package helpers with provenance, trust, expiry, and revocation; durable records exist. See [context package](marina/internal/contextpack/) and [worker input](marina/internal/flow/runtime.go). | The active workflow supplies task fields and a pinned repository; it does not assemble channel history, selected project documents, and prior decisions through those helpers. |
| Installation | Paired manifests, preparation script, and a documented local Podman/Kind installation with TLS, persistent volumes, and intake disabled. See [local evidence](deploy/kubernetes/local-podman/README.md). | The local installation still depends on the Mac. No documented real model/repository acceptance or laptop-off result. Backup/restore work is explicitly deferred for the current pilot. |
| Operator CLI | Public-API client with operator-session/CSRF authentication, protected credentials, packaging, and real-process tests. See [client](cli/internal/deck/client.go). | Repository-specific budget and permission configuration remains in the web task form; the CLI creates generic tasks. |
| Broader factory | Policy, planning, signal, and verification foundations exist; Deck can store operational records. | Their presence is not an integrated production loop. Delivery, operational remediation, additional harnesses, and automatic project-wide work discovery remain future scope. |

**Verification during this review:** `make check` and `make conformance` passed. The latter starts a real Deck process and exercises Marina's controller with deterministic Git/runtime doubles, including journal reopening between workflow steps. This is not a live model, Git-provider, Kubernetes, or browser acceptance result. Local installation evidence above is the recorded 2026-09-09 check, not a fresh deployment inspection.

## How to use the tables

IDs are stable: refer to them in discussions and commits, and do not renumber remaining tasks when one is completed. Each row is one focused change, decision, or acceptance exercise.

- **Open:** behavior or a concrete deliverable is missing.
- **Verify:** implementation exists; the named acceptance evidence is still missing.
- **Decide:** an operator/product choice is needed before dependent work.
- **Later:** outside the current release; revisit only after the stated trigger.
- **Owners:** Deck = work-control surface; Marina = execution system; Joint = both sides of the public contract; Ops = installation/operator work; CLI = platformctl.

Mark a row **Done** only when its “Done when” condition is met and link the evidence. For protocol changes, completion also requires both product checks and real-process conformance. Implement fixes and features with behavior-first tests; keep deployment validation separate from deterministic tests. Listing an installation or external action here does not execute or authorize that action.

## 1. Fix current entry points

These bounded corrections are complete. [Verification evidence](docs/validation/fix-01-06.md) records the local checks, real-process conformance, and the remaining interactive-browser validation limit; conversational delegation remains separate work.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| FIX-01 | Done | Deck | Give channel-promoted coding tasks valid execution limits. | Promotion exposes the same relevant budget and PR-permission controls as task creation; an authorized task reaches implementation and verification without editing its attempt count. [Evidence](docs/validation/fix-01-06.md). |
| FIX-02 | Done | Joint | Check repository-task prerequisites before Ready. | Missing acceptance criteria, insufficient attempts, or missing requested mutation authority is explained before dispatch; drafts remain allowed and Marina retains its own checks. [Evidence](docs/validation/fix-01-06.md). |
| FIX-03 | Done | Deck | Refresh the open channel on message events and reconnect. | A message from another client appears once without changing channels or reloading the page. [Evidence](docs/validation/fix-01-06.md). |
| FIX-04 | Done | Deck | Add cursor pagination to channel history. | A conversation beyond the current 100/200-message limits remains readable in order, without skipped or duplicated messages. [Evidence](docs/validation/fix-01-06.md). |
| FIX-05 | Done | CLI | Support Deck's authenticated operator session. | List, inspect, create, and cancel work against an auth-enabled Deck using session/CSRF semantics; credentials stay protected and CLI documentation matches. [Evidence](docs/validation/fix-01-06.md). |
| FIX-06 | Done | Joint | Reconcile remaining capability claims in working documents. | Visions, architecture, READMEs, and agent instructions distinguish the supported HTTP pair and review-ready PR flow from future headless packaging, pause/resume, revision loops, and merge authority. [Evidence](docs/validation/fix-01-06.md). |

## 2. Prove the first release away from the laptop

**Deployment target decided:** Deck and Marina target vanilla Kubernetes through standard APIs, with no required cloud provider or Kubernetes distribution. The user's local Podman/Kind cluster is the test environment; choosing a hosting vendor is not a product decision or a blocker for development.

**Initial services decided:** use OpenRouter for model inference, GitHub for source control, and GHCR for published images. Keep separate SQLite databases and artifact files on Kubernetes persistent volumes. Backup automation and restore drills are deferred. The running platform requires only GitHub services and OpenRouter externally; image builds and cluster setup may fetch required dependencies from their normal sources. Prepackage the dependencies needed by agent execution and verification. See [decision 0010](docs/decisions/0010-initial-installation-services.md). Exact model, repository, credentials, and destination rules still need configuration and validation.

**Test locally first:** the user's local Podman installation is authorized for Kubernetes development testing. Reuse the existing [Podman/Kind profile](deploy/kubernetes/local-podman/README.md), with context `kind-marina-local` on `podman-machine-default-root`, and inspect its current state before running checks. Exercise packaging, Jobs, persistence, recovery, cancellation, and network isolation locally as relevant; model/repository workflow tests also need their configured endpoints and credentials. Record the environment with each result; RUN-10 still needs a host independent of the Mac to demonstrate laptop-off operation. Local evidence does not claim validation of every cluster configuration.

Use the existing [installation and acceptance guide](deploy/kubernetes/first-release/README.md). RUN-01 through RUN-08 prepare one installation; RUN-09 validates its harness/model combination; the following checks exercise that same installation. Reuse the existing images, manifests, credentials mechanism, and adapters.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| RUN-01 | Verify | Ops | Validate the vanilla Kubernetes installation using local Podman/Kind. | Installation prerequisites for storage, DNS, network-policy enforcement, and access are documented; the standard manifests install locally without cloud-specific dependencies, with cluster/configuration versions recorded. |
| RUN-02 | Open | Ops | Document the selected services and concrete runtime network destinations. | GitHub/GHCR and OpenRouter service/download destinations, internal storage, and DNS resolution are recorded with the data exchanged; ordinary build/setup downloads are distinguished from runtime traffic and OpenRouter's upstream processing is described accurately. |
| RUN-03 | Open | Ops | Configure one disposable GitHub acceptance repository. | The repository, small known defect, allowed paths, trusted offline checks, required CI, and scoped credentials are configured; credentials remain outside Git. |
| RUN-04 | Open | Ops | Build and pin the acceptance worker image for GHCR or local loading. | Build-time downloads supply the fixture's tools and dependencies; the worker runs its checks without public package downloads at runtime, and its image digest is recorded. |
| RUN-05 | Open | Ops | Prepare a reproducible site overlay. | Existing preparation/manifests produce pinned images, separate persistent stores and identities, TLS trust, and private configuration without unresolved placeholders. |
| RUN-06 | Open | Ops | Enforce the selected GitHub/OpenRouter network boundaries. | Required internal and selected external destinations work; disallowed worker/controller destinations are denied in observed tests. Node image pulls and DNS resolution are accounted for separately, matching RUN-02. |
| RUN-07 | Verify | Ops | Start the paired services on the selected host. | Both services become ready with persistent storage and recover after a service restart; initial intake stays disabled until configuration is ready. |
| RUN-08 | Open | Ops | Provide an authenticated Deck URL independent of local forwarding. | A second device can sign in over trusted TLS without an operator-laptop port-forward or shell session. |
| RUN-09 | Verify | Marina | Validate the pinned harness against the selected OpenRouter model. | One bounded fixture run through the configured OpenRouter Responses endpoint exercises streaming, tool calls, usage accounting, and output collection with no implicit endpoint fallback. |
| RUN-10 | Verify | Joint | Complete one laptop-off repository fix. | A task created in Deck produces one PR, a distinct verification Job, current required CI, and downloadable evidence while the laptop is disconnected; another device can review it. |
| RUN-11 | Verify | Joint | Restart each control service during work. | Restarting Deck and Marina separately during execution and CI waiting preserves the original work and produces no duplicate branch, commit, or PR. |
| RUN-12 | Verify | Joint | Cancel a real active worker from Deck. | Authority is revoked, worker termination is observed before acknowledgement, and attempt resources are cleaned up while evidence remains accessible. |
| RUN-13 | Verify | Joint | Exercise failed and missing verification gates. | Failed local checks, failed CI, and a missing required CI check never produce a completed result; Deck explains the blocker or continued wait. |
| RUN-14 | Verify | Joint | Change a PR head after verification. | Evidence for the previous head cannot complete the task; the operator sees why a new verification attempt is needed. |
| RUN-15 | Verify | Joint | Interrupt required providers during a run. | Deck/model/Git outages stop unsafe progress, retain useful state, and recover or report a bounded blocker without duplicate external work. |
| RUN-16 | Verify | Marina | Exhaust configured execution budgets. | Time, token, attempt, and nonzero cost limits stop further admitted work and leave an inspectable reason; uncertain model usage retains its reservation. |
| RUN-17 | Verify | Marina | Fail an active Kubernetes worker. | Job/process failure or eviction cannot produce false completion; cleanup and the current follow-up-task recovery path are observable. |
| RUN-18 | Open | Joint | Record the installation acceptance result. | A redacted report links task/run IDs, PR revision, image/configuration versions, and RUN-09 through RUN-17 results; unresolved failures stay open. |

**First-release gate:** the selected installation passes the acceptance exercises. Backup/restore work is deferred for the current pilot under decision 0010; persistent storage and restart recovery remain required. A running local Kind cluster or green mock tests alone do not satisfy the workflow acceptance gate, and RUN-10 separately proves laptop independence.

## 3. Make unattended operation recoverable

Use the same first installation. OPS-01/02 are deferred until backup recovery becomes an operator requirement; OPS-02 then depends on OPS-01. Retention work must preserve evidence needed by active runs and retained tasks.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| OPS-01 | Later | Ops | Automate a consistent quiesced backup when required. | After the operator reactivates backup work, one documented command backs up both databases with their WAL state, artifacts, identities, and required private configuration to the chosen destination. |
| OPS-02 | Later | Joint | Restore that backup into an isolated installation. | After OPS-01, services recover the same identities and work records, artifact digests validate, and reconciliation creates no duplicate external work. |
| OPS-03 | Open | Ops | Rehearse certificate renewal. | The installation can replace expiring certificates and trust without recreating operator/service identities or losing work. |
| OPS-04 | Verify | Joint | Rehearse credential rotation. | Operator, service, and Git/model credential changes take effect with the documented session/revocation behavior; no credential appears in evidence or diagnostics. |
| OPS-05 | Open | Marina | Implement bounded artifact retention. | Retention rules and a dry-run report precede deletion; referenced/current evidence is preserved and expired unreferenced artifacts can be removed safely. |
| OPS-06 | Open | Ops | Write a short recovery runbook from acceptance failures. | Disk-full, unavailable provider, stuck cancellation, and failed worker cases each have detection and recovery steps; no direct ledger editing is required. Add restored-storage procedures when OPS-01/02 are activated. |

## 4. Reuse project context

This is proposed work after the first-release proof. Reuse the existing context-package and journal foundations. Start with explicit project sources; an automatic memory or retrieval framework is not a prerequisite.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| CTX-01 | Decide | Joint | Define the minimum project-context contract. | Ownership and references for repository instructions, selected documents, decisions, and conversation excerpts are recorded without creating a second authoritative ledger. |
| CTX-02 | Open | Deck | Register and revise project context sources. | The operator can maintain a small set of sources with versions, provenance, and superseded/revoked state through the public API. |
| CTX-03 | Open | Marina | Assemble the existing context package in the active workflow. | Task, pinned repository, policy, and selected project sources form a persisted package delivered to the worker and reconstructible after restart. |
| CTX-04 | Open | Joint | Attach bounded conversation context to a task. | Selected message IDs/revisions are captured at delegation and included as task data; unrelated conversations and subsequent edits do not silently enter the run. |
| CTX-05 | Open | Marina | Enforce freshness and trust when using assembled context. | Stale, conflicting, revoked, and injected input cases cause omission or a focused clarification; retrieved text cannot expand authority. |
| CTX-06 | Open | Deck | Show the task's interpreted outcome and context sources. | The operator can inspect what the agent received, where it came from, and which decisions apply without reading raw execution logs. |
| CTX-07 | Verify | Joint | Demonstrate context reuse across two tasks. | A second task uses an approved project decision without re-explanation; changing that decision affects only newly authorized work. |

## 5. Delegate from a web conversation

These tasks implement the proposal in decision 0009. CHAT-01 resolves the interaction rules; context-dependent execution uses CTX-03/04. Extend the existing public contract and keep the first configured coding harness.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| CHAT-01 | Decide | Joint | Specify questions, delegation, and follow-up semantics. | Examples distinguish discussion, explicit coding requests, quoted mentions, insufficient authority, and feedback; approval is requested only when required authority is missing. |
| CHAT-02 | Open | Deck | Persist task-linked conversation threads. | A discussion can reference its task and runs, with stable identity across devices; existing channel history remains accessible. |
| CHAT-03 | Open | Deck | Add the thread view and explicit agent invocation UI. | The operator can discuss work, select the configured agent, and distinguish asking from delegating without leaving the project. |
| CHAT-04 | Open | Marina | Run a bounded conversational agent on Kubernetes. | The agent can answer about the permitted project context and propose work; conversational runs persist and have budgets without source-write authority. |
| CHAT-05 | Open | Joint | Persist clarification questions and their answers. | Missing intent can be resolved after disconnect/restart, and each answer resumes the correct conversation without silently changing active task authority. |
| CHAT-06 | Open | Joint | Convert explicit delegation into one authorized task. | Repeated delivery creates one task with interpreted outcome, acceptance criteria, scope, and budget; existing authorization avoids duplicate forms, and casual mentions do not execute. |
| CHAT-07 | Open | Marina | Return milestones and results to the originating thread. | Relevant progress, blockers, PRs, and evidence are linked once to the correct conversation, including after restart or a lost response. |
| CHAT-08 | Open | Joint | Turn result feedback into a linked follow-up task. | Feedback retains the original result and context, establishes new bounded authority, and cannot overwrite a completed run or its evidence. |
| CHAT-09 | Verify | Joint | Prove conversation-to-result through real Deck. | Dispatch, clarification, duplicate requests, recovery, and result publication pass both product checks and the real-process conformance suite. |
| CHAT-10 | Verify | Joint | Complete the conversational workflow across devices. | On the selected installation, discuss and delegate from one device, disconnect, then answer a question or review the result from another. |

## 6. Return to an inbox with useful information

Extend the existing attention/active/results queues; do not rebuild them. INBOX-01/02 can work for board-created tasks before conversation dispatch is implemented.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| INBOX-01 | Open | Deck | Persist the operator's reviewed/unread state. | Reading a result survives refresh and device changes without changing whether the underlying task is completed. |
| INBOX-02 | Open | Deck | Add an unread-results view to the current overview. | New results remain visible until reviewed; decisions still needing an answer remain in attention even after being read. |
| INBOX-03 | Open | Deck | Link board, thread, decision, and result views. | Each view opens the same task/run and relevant conversation directly, without duplicate task state or copied result records. |
| INBOX-04 | Open | Deck | Summarize meaningful changes since the last visit. | Returning operators see new results, blockers, and decisions; routine heartbeat events stay available in detail without filling the inbox. |
| INBOX-05 | Verify | Deck | Cover the main browser journey at desktop and mobile sizes. | Automated browser checks cover login, task creation, approval, cancellation, reconnect, and result/evidence access; conversation checks are added when available. |

## 7. Handle ordinary setbacks with less intervention

These are later increments after the first release, not gaps in its intentionally bounded one-pass workflow. Preserve human merge/deploy authority. Sequence AUTO-01/02 before revision execution; AUTO-05 relies on a new verified revision.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| AUTO-01 | Decide | Marina | Define bounded revision and escalation rules. | Retryable check/review failures, stop conditions, maximum attempts, and authority/budget reuse are specified for one repository workflow. |
| AUTO-02 | Open | Marina | Persist revision attempts within one execution. | Attempt identities and remaining budgets survive restart; the current fixed two-attempt limit is replaced only for the configured revision workflow. |
| AUTO-03 | Open | Marina | Feed one failed verification check into a repair attempt. | A fresh implementation attempt receives the failure and authorized context, then requires separate verification; it stops at its configured limit. |
| AUTO-04 | Open | Marina | Feed one CI or review-change request into a repair attempt. | Feedback is tied to the current PR revision, treated as untrusted data, and cannot expand the task's scope or permissions. |
| AUTO-05 | Open | Joint | Supersede evidence after a revision. | Only evidence valid for the new head can complete the run; earlier evidence remains visible as historical and is never presented as current. |
| AUTO-06 | Verify | Joint | Recover a revision loop across a restart. | One injected failure is repaired, verified, and reflected in the same PR without duplicate commits/requests or exceeded budgets. |
| AUTO-07 | Decide | Joint | Define the first independent semantic review gate. | One change class has an explicit acceptance check beyond test success, with separate reviewer authority and a documented escalation rule. |

## 8. Measure whether the platform is useful

Begin with a small manually recorded pilot after RUN-10; automate only measurements that prove useful.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| VALUE-01 | Open | Joint | Record human effort for representative tasks. | Each pilot task records preparation, supervision, review, correction, maintenance effort, elapsed time, and observed model/compute cost. |
| VALUE-02 | Open | Joint | Compare board and conversational delegation. | After CHAT-10, the same kinds of bounded work are compared for repeated context, necessary interventions, result quality, and total human effort; the next priority follows the findings. |

## 9. Later decisions, outside the active build list

These retain relevant long-term questions without carrying a speculative implementation backlog. Split approved work into small implementation rows only when its trigger is met.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| LATER-01 | Later | Joint | Select a second project workflow after the pilot. | Evidence identifies whether multi-project configuration or concurrent work is actually needed; one concrete workflow is chosen before scheduling changes. |
| LATER-02 | Later | Marina | Evaluate a second harness or provider boundary. | After the first path is useful, one real alternative demonstrates the portability requirement and its cost; no plugin marketplace is assumed. |
| LATER-03 | Later | Joint | Choose the first production-feedback use case. | After repository work is reliable, one signal source and a diagnosis-only acceptance case are selected; existing signal records alone do not count as integration. |
| LATER-04 | Later | Joint | Define one non-production delivery proof. | A specific preview/canary environment, existing delivery integration, observation window, and rollback criterion are chosen before implementing deployment automation. |
| LATER-05 | Later | Joint | Decide whether any autonomy should extend beyond PR review. | Pilot evidence identifies one eligible change class and explicit human-only boundaries; no automatic merge or production authority follows from this backlog. |

## Removed or consolidated from the old list

| Old material | Treatment |
| --- | --- |
| Completed bootstrap, Go/SQLite/authentication decisions, and repeated Milestone 1 checkboxes | Replaced with the evidence-linked current-state summary. Decisions and tests retain their history. |
| “No runtime/source mutation enabled,” “implement Kubernetes adapter,” “create the first PR,” and “choose the initial artifact store/harness” | Removed as stale build tasks. Those implementations exist; installation-specific configuration and acceptance remain under RUN/OPS. |
| Full context assembly, pause/resume, and all-control-command completion claims | Narrowed to actual behavior. Context helpers need integration, and the active repository workflow supports cancel while rejecting other control commands. |
| Duplicate milestone exit gates and broad “test every boundary” tasks | Replaced with named acceptance exercises and the shared completion rule above. |
| Early TUI, general project-management expansion, multi-tenancy, billing, marketplace, multi-cluster/HA machinery, and independent product launches | Removed from the active backlog; none is needed to prove the current one-operator web workflow. |
| Unscheduled merge/deploy/remediation loops and broad factory automation | Reduced to the explicitly deferred LATER decisions. Automatic CI/review repair remains a separate bounded increment under AUTO. |
