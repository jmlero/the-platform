# The Platform — implementation gaps and TODO

Reviewed against source code and the product visions on **2026-09-17**.

**Goal:** use Deck to define, discuss, and delegate software work; let Marina run it on operator-controlled Kubernetes; return in the desktop browser to useful results or focused questions.

This replaces the historical milestone checklist. Completed foundations are summarized below; the tables contain remaining work. [Decision 0007](docs/decisions/0007-sovereign-first-usable-release.md) still governs the first release: a bounded repository fix ends at a **PR ready for human review**, or a truthful blocker. The board, conversation, and inbox target in [decision 0009](docs/decisions/0009-asynchronous-agent-work.md) is proposed next work, not implemented behavior or a change to release scope.

## Current status: repository PoC/MVP validated — 2026-09-21

The first repository-workflow PoC and MVP acceptance checks are complete.
**RUN-09 passed locally:** a bounded Deck task used the configured model and
Kubernetes worker to produce [PR #1](https://github.com/jmlero/marina-acceptance/pull/1),
a separate verification Job, passing CI at the same head, and downloadable,
digest-checked evidence with persisted usage. **The supported MVP checks also
passed**, including recovery and failure cases; see the
[acceptance report](docs/validation/poc-mvp-2026-09-21.md). The operator closed
VALUE-01 on 2026-09-21 because representative pilot value measurement is not
wanted. Human effort and actual billing remain unmeasured; this is no longer
pending work.

| Priority | Tasks | Validation outcome |
| --- | --- | --- |
| P0 — prerequisites | RUN-03, RUN-05 | Done: configured repository and credential boundary verified in the local profile. |
| P0 — PoC | RUN-09 | Done: complete Deck → Marina → worker → verified GitHub PR → Deck evidence path. |
| P1 — MVP acceptance | RUN-11 through RUN-16, RUN-18 | Done within the supported workflow: recovery, cancellation, verification gates, changed heads, provider outages and budgets; evidence and limitations recorded. |
| P2 — agent-platform PoC | AGENT-01 through AGENT-06 | Done: Codex repository work and LangChain diagnostic analysis completed through the same local Deck/Marina controls; [evidence](docs/validation/agent-02-06-2026-09-21.md). |
| P3 — agent-platform MVP | AGENT-07 through AGENT-13 | Done within the two-workload scope: API invocation, bounded tool/context access, inspection, shared failure checks, repeatable installation and combined local live acceptance; [evidence and limits](docs/validation/agent-11-13-2026-09-22.md). |

The operator approved extending Marina toward an **AgentCore-like platform on
Kubernetes** on 2026-09-21. Extend the existing Marina component, with software
engineering as its first workflow. The two-workload PoC/MVP below is complete;
broader general agent hosting remains future work. [Decision 0015](docs/decisions/0015-marina-agent-platform.md) records
the scope. VALUE-01 stays Closed and is not a prerequisite for this work.

Use the existing local `kind-marina-local` installation first. GCP remains an
authorized alternative when a concrete validation need warrants it, under the
environment and budget rules in [decision 0010](docs/decisions/0010-initial-installation-services.md).
This sequence uses the existing bounded first-release scope for MVP acceptance;
explicit project context is complete as a subsequent increment; conversational dispatch, inbox state and automatic-revision expansion remain subsequent work.
Retain the existing €5 total model-test limit and explicit task authority.
Mark tasks Done only when their acceptance evidence exists; operator-declined
work is Closed.

## What exists and what is missing

| Area | Implemented evidence | Gap to the vision |
| --- | --- | --- |
| Deck work control | Separate SQLite ledger; scoped service authentication; operator sessions; tasks, plans, decisions, commands, evidence, export/import. See [store](deck/internal/store/) and [HTTP API](deck/internal/httpapi/). | Extend the existing ledger for conversations and attention state; preserve one owner for each fact. |
| Marina durability | Persisted claims, leases, task snapshots, execution checkpoints, budget accounting, recovery, and ordered publication. See [reconciliation](marina/internal/reconcile/) and [journal](marina/internal/journal/sqlite/). | Local restart/outage recovery is validated in the [acceptance report](docs/validation/poc-mvp-2026-09-21.md); broader packaging and operating environments remain future scope. |
| Repository execution | Configured Codex worker, Kubernetes adapter, GitHub/GHES publication, a separate clean verification Job, PR/CI observation, and current-revision evidence. See [workflow](marina/internal/flow/), [worker](marina/internal/worker/), and [real-process tests](marina/internal/deck/real_workflow_test.go). | One configured repository and one active task; one implementation plus one verification attempt. Failed checks or requested changes block. Automatic revisions and independent semantic review are absent. |
| Authority and data boundaries | Explicit Git/model destinations, attempt credentials, live authority checks, token/cost reservations, isolated workers, and authenticated artifact downloads. See [worker gateway](marina/internal/workerapi/) and [installation](deploy/kubernetes/first-release/). | The selected OpenRouter model, GitHub credential and local network boundaries are validated. Published GHCR image pulls are not covered; storage stays on Kubernetes volumes and backups are deferred. |
| Board and results | Task creation, authorization, board, decisions, cancellation, PR/evidence links, and attention/active/results queues. See [web components](deck/web/src/lib/components/) and [queue logic](deck/web/src/lib/work-queues.ts). | No persistent reviewed/unread result state. No complete browser workflow test covers delegation through return to a result. |
| Conversations | Persistent paginated channel messages, live/reconnect refresh, canonical references, and manual promotion with the shared task-budget form. See [channel storage](deck/internal/store/channels.go) and [channel UI](deck/web/src/lib/components/ChannelsView.svelte). | No conversational agent, mention dispatch, task threads, or automatic result replies. |
| Reusable context | Versioned Deck knowledge and selected messages; immutable persisted worker packages; source inspection, freshness/revocation gates and Marina-owned repository association. [CTX evidence](docs/validation/ctx-02-08-2026-09-21.md). | Explicit sources only; automatic discovery, semantic contradiction detection, and learned-memory promotion remain future scope. The new context increment has deterministic real-process evidence, not a new live-model acceptance run. |
| Installation | Paired manifests, preparation script, and a documented local Podman/Kind installation with TLS, persistent volumes, and intake disabled. See [local evidence](deploy/kubernetes/local-podman/README.md). | Local model/repository acceptance passed; see the [2026-09-21 report](docs/validation/poc-mvp-2026-09-21.md). Laptop-off testing and backup/restore work are outside the current pilot. |
| Operator CLI | Public-API client with operator-session/CSRF authentication, protected credentials, packaging, and real-process tests. See [client](cli/internal/deck/client.go). | Repository-specific budget and permission configuration remains in the web task form; the CLI creates generic tasks. |
| Broader factory | Policy, planning, signal, and verification foundations exist; Deck can store operational records. | Their presence is not an integrated production loop. Delivery, operational remediation, additional harnesses, and automatic project-wide work discovery remain future scope. |

**Verification during this review:** `make check` and `make conformance` passed. The latter starts a real Deck process and exercises Marina's controller with deterministic Git/runtime doubles, including journal reopening between workflow steps. This is not a live model, Git-provider, Kubernetes, or browser acceptance result. Fresh local installation, image, network and restart evidence is recorded in the [2026-09-17 RUN report](docs/validation/run-01-18-2026-09-17.md). The subsequent [2026-09-21 report](docs/validation/poc-mvp-2026-09-21.md) records passing live model/repository workflow acceptance.

## How to use the tables

IDs are stable: refer to them in discussions and commits, and do not renumber remaining tasks when one is completed. Each row is one focused change, decision, or acceptance exercise.

- **Open:** behavior or a concrete deliverable is missing.
- **Verify:** implementation exists; the named acceptance evidence is still missing.
- **Decide:** an operator/product choice is needed before dependent work.
- **Later:** outside the current release; revisit only after the stated trigger.
- **Closed:** declined by the operator without satisfying the original acceptance criteria.
- **Owners:** Deck = work-control surface; Marina = execution system; Joint = both sides of the public contract; Ops = installation/operator work; CLI = platformctl.

Mark a row **Done** only when its “Done when” condition is met and link the evidence. For protocol changes, completion also requires both product checks and real-process conformance. Implement fixes and features with behavior-first tests; keep deployment validation separate from deterministic tests. Listing an installation or external action here does not execute or authorize that action.

## Agent platform: PoC and MVP on Kubernetes

Build a small, operator-owned agent execution and governance platform inside
Marina's existing Go module and service. Deck keeps human intent, authorization,
decisions and results; Marina keeps execution state and adapters. Reuse the
current journal, leases, fences, budgets, gateway, artifacts and Kubernetes Jobs.
Keep one operator and one active task. Full AWS feature/API parity, a third
component, a new workflow engine, multi-tenancy, HA, interactive sessions,
pause/resume, autonomous merge/deploy and a plugin marketplace are outside these
gates. Additional infrastructure must follow a demonstrated workload need.

### PoC: two workloads share one execution foundation

AGENT-01 through AGENT-06 are complete. The versioned diagnostic fixture runs
through a LangChain worker alongside the existing Codex repository workflow,
sharing durable controls without a dummy repository, commit or PR. The
[local proof](docs/validation/agent-02-06-2026-09-21.md) records actual model use,
Jobs, outputs, validation, usage and limits. This closes the second-harness proof
under LATER-02; live production-feedback integration remains LATER-03.

**2026-09-21 harness choice:** the operator selected the **LangChain open-source
ecosystem**, starting with `create_agent` on LangGraph inside a Marina-managed
Kubernetes worker. Use self-hostable OSS libraries only; LangSmith, Managed Deep
Agents, proprietary services and AWS dependencies are outside this choice.
Custom graphs and Deep Agents are workload-driven follow-ups. This completes
AGENT-01. The [version-1 profiles](marina/docs/workloads/profiles-v1.md) define
the implemented fixture and bounded configuration; the local proof validates
gateway compatibility and both workloads' completion gates.
See [decision 0015](docs/decisions/0015-marina-agent-platform.md#harness-ecosystem-choice).

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| AGENT-01 | Done | Joint | Select the second harness ecosystem for the read-only analysis workload. | The operator selected self-hostable LangChain OSS, starting with `create_agent` on LangGraph in a Marina-managed Kubernetes worker. [Accepted decision](docs/decisions/0015-marina-agent-platform.md#harness-ecosystem-choice). Fixture and profile details are tracked under AGENT-02. |
| AGENT-02 | Done | Marina | Define versioned workload and harness profiles, including the analysis fixture. | A documented contract identifies the pinned image/harness, input and output schemas, evidence rules, tool/network permissions, model/credential references and limits. One read-only document or diagnostic-analysis fixture has explicit inputs, expected findings, output checks, model/tool destinations and bounded budgets; its harness configuration uses the selected LangChain ecosystem, with existing components evaluated before new dependencies. Repository fields belong to the coding workload; unsupported capabilities fail closed, and profile edits cannot silently alter active runs. [Profile contract](marina/docs/workloads/profiles-v1.md) and [validation](docs/validation/agent-02-06-2026-09-21.md). |
| AGENT-03 | Done | Marina | Separate reusable execution controls from repository steps. | Both workload profiles use the existing durable lifecycle, authority, budgets, cancellation, artifacts and recovery through narrow internal interfaces. Git/PR/CI steps stay in the repository workflow; existing records remain readable and the current workflow checks pass. [Validation](docs/validation/agent-02-06-2026-09-21.md). |
| AGENT-04 | Done | Marina | Run the second harness in an isolated Kubernetes Job. | The selected harness consumes its frozen inputs and returns a structured report, usage and artifacts through shared execution controls; it receives no source-write or cluster-mutation authority, and requires no hard-coded Codex invocation or repository checkout. [Validation](docs/validation/agent-02-06-2026-09-21.md). |
| AGENT-05 | Done | Joint | Apply workload-specific completion evidence. | The analysis report is checked against the fixture's expected findings and bound to input/profile/output digests with independently produced validation evidence. Missing, invalid or mismatched evidence blocks completion; repository completion still requires separate verification and CI at the current PR head. Any contract change preserves v1 semantics or uses a new major. [Validation](docs/validation/agent-02-06-2026-09-21.md). |
| AGENT-06 | Done | Joint | Demonstrate the agent-platform PoC on local Kubernetes. | The coding workflow and the non-coding workload each complete through real Deck and Marina using different harnesses and the same execution controls. A redacted report records pinned inputs/images, actual model use, Jobs, outputs, evidence, usage and limitations; mocks alone do not satisfy this gate. [Validation](docs/validation/agent-02-06-2026-09-21.md). |

**PoC exit:** both workflows pass AGENT-06 without weakening the existing
repository authority or evidence gates. A second model behind the same harness
does not establish harness portability. This proof does not claim general agent
hosting or representative economic value.

### MVP: a supported platform for those two workloads

Begin after AGENT-06. AGENT-07 through AGENT-10 supply the public and execution
behavior exercised by AGENT-11; AGENT-12 packages it and AGENT-13 records the
complete acceptance result. Reuse CTX work where applicable without making the
entire knowledge, conversation or inbox roadmap a prerequisite.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| AGENT-07 | Done | Joint | Expose approved profile selection and invocation through the public API. | A documented API client can discover supported profiles/capabilities, submit authorized work selecting a pinned profile, inspect progress, cancel and retrieve results. Deck remains the authoritative task ledger, Marina owns execution configuration, duplicate invocation is idempotent, and OpenAPI, fixtures, authorization and consumer conformance stay synchronized. [Validation](docs/validation/agent-07-10-2026-09-21.md). |
| AGENT-08 | Done | Marina | Govern one reusable read-only tool through the gateway. | The second harness can invoke one selected MCP or HTTP tool with validated arguments, per-run allowlists, scoped credential references and audited decisions. Disallowed, expired or revoked calls are denied outside the harness, and observed network controls prevent direct bypass. A general tool marketplace is unnecessary. [Validation](docs/validation/agent-07-10-2026-09-21.md). |
| AGENT-09 | Done | Marina | Deliver reproducible context to both workload types. | Existing context-package foundations supply selected immutable inputs with provenance, trust, digests and explicit revocation handling. Recovery uses recorded revisions; untrusted content cannot expand authority. Coordinate with CTX-03/05/06 and decision 0014 without duplicating Deck or Git knowledge ownership. [Validation](docs/validation/agent-07-10-2026-09-21.md). |
| AGENT-10 | Done | Joint | Make execution and results inspectable across harnesses. | Deck shows workload/profile identity, meaningful state, usage, blockers and downloadable evidence for either workload. Redacted diagnostics correlate task, execution, attempt, model/tool calls and artifacts, including uncertain usage; no new observability backend is required. [Validation](docs/validation/agent-07-10-2026-09-21.md). |
| AGENT-11 | Done | Joint | Prove shared failure and authority semantics. | Both profiles pass duplicate/lost-response handling, separate Deck/Marina restarts, cancellation with observed worker termination, lease expiry, budget exhaustion, provider outage and invalid-evidence cases without duplicate work or false completion. Tool denial/revocation and input/profile changes are covered; deterministic and live local evidence are distinguished. [Validation and safe-blocker limits](docs/validation/agent-11-13-2026-09-22.md). |
| AGENT-12 | Done | Ops | Package and document the two-workload installation. | Repeatable manifests/configuration select pinned profiles and prepackage execution dependencies; an upgrade preserves existing repository work and separate stores. Setup, supported capabilities, isolation limits, credential references, diagnostics and recovery are documented for the authorized local profile, with new intake disabled until explicitly configured. [Installation](deploy/kubernetes/agent-platform/README.md), [preservation proof](docs/validation/agent-11-13-2026-09-22.md). |
| AGENT-13 | Done | Joint | Record the agent-platform MVP acceptance. | AGENT-07 through AGENT-12 pass, along with `make check`, `make conformance` and `make check-installation`. A redacted report under `docs/validation/` links API invocations, real Jobs, outputs and failure/recovery evidence for both workloads on the named installation; remaining limitations are explicit. [Acceptance](docs/validation/agent-11-13-2026-09-22.md), [redacted observations](docs/validation/agent-11-13-2026-09-22.json). |

**MVP exit:** an API client can submit either supported workload to the same
installation, retain human governance through Deck, and obtain a verified result
or truthful blocker across restarts and cancellation. This establishes a bounded
Kubernetes agent platform, not AgentCore parity. Existing environment, credential
and spending limits still apply; these tasks grant no additional runtime spend.

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

## 2. First-release installation and acceptance

**Deployment target decided:** Deck and Marina target vanilla Kubernetes through standard APIs, with no required cloud provider or Kubernetes distribution. The user's local Podman/Kind cluster and existing GCP GKE Autopilot cluster are authorized development/test environments; choosing a hosting vendor is not a product decision or a blocker for development.

**2026-09-21 additional environment:** GCP project `jmlero`, region `europe-west3`, cluster `autopilot` is available through the configured `gcloud` CLI. Read `/Users/jmolero/personal/github/infra-gcp/AGENTS.md` and its `README.md` for infrastructure ownership, DNS endpoint access, and the EUR 10/month total budget. Use explicit contexts and separate installation state; local acceptance evidence remains local and no RUN item is completed by GCP availability. See [decision 0010](docs/decisions/0010-initial-installation-services.md).

**Initial services decided:** use OpenRouter for model inference, GitHub for source control, and GHCR for published images. Keep separate SQLite databases and artifact files on Kubernetes persistent volumes. Backup automation and restore drills are deferred. The running platform requires only GitHub services and OpenRouter externally; image builds and cluster setup may fetch required dependencies from their normal sources. Prepackage the dependencies needed by agent execution and verification. See [decision 0010](docs/decisions/0010-initial-installation-services.md). Exact model, repository, credentials, and destination rules still need configuration and validation.

**Test on local Kubernetes:** the user's local Podman installation is authorized for Kubernetes development testing. Reuse the existing [Podman/Kind profile](deploy/kubernetes/local-podman/README.md), with context `kind-marina-local` on `podman-machine-default-root`, and inspect its current state before running checks. Exercise packaging, Jobs, persistence, recovery, cancellation, and network isolation locally as relevant; model/repository workflow tests also need their configured endpoints and credentials. Keep the Mac running because it hosts Kubernetes. The operator removed RUN-10 on 2026-09-17; its ID is retired and an independent host is not an acceptance prerequisite. See [decision 0007](docs/decisions/0007-sovereign-first-usable-release.md). Local evidence does not claim validation of every cluster configuration.

Use the existing [installation and acceptance guide](deploy/kubernetes/first-release/README.md). RUN-01 through RUN-08 prepare one installation; RUN-09 validates its harness/model combination; the following checks exercise that same installation. Reuse the existing images, manifests, credentials mechanism, and adapters.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| RUN-01 | Done | Ops | Validate the vanilla Kubernetes installation using local Podman/Kind. | Installation prerequisites for storage, DNS, network-policy enforcement, and access are documented; the standard manifests install locally without cloud-specific dependencies, with cluster/configuration versions recorded. [Local evidence](docs/validation/run-01-18-2026-09-17.md). |
| RUN-02 | Done | Ops | Document the selected services and concrete runtime network destinations. | GitHub/GHCR and OpenRouter service/download destinations, internal storage, and DNS resolution are recorded with the data exchanged; ordinary build/setup downloads are distinguished from runtime traffic and OpenRouter's upstream processing is described accurately. [Local evidence](docs/validation/run-01-18-2026-09-17.md). |
| RUN-03 | Done | Ops | Configure one disposable GitHub acceptance repository. | The repository, known defect, allowed paths, trusted offline checks, required CI, and operator-approved credentials are configured outside Git. The classic-token pilot exception and live source-write proof are recorded in the [acceptance report](docs/validation/poc-mvp-2026-09-21.md). |
| RUN-04 | Done | Ops | Build and pin the acceptance worker image for GHCR or local loading. | Build-time downloads supply the fixture's tools and dependencies; the worker runs its checks without public package downloads at runtime, and its image digest is recorded. [Local evidence](docs/validation/run-01-18-2026-09-17.md). |
| RUN-05 | Done | Ops | Prepare a reproducible site overlay. | Existing preparation/manifests produce pinned images, separate persistent stores and identities, TLS trust, and private configuration without unresolved placeholders. [Workflow preflight and local proof](docs/validation/poc-mvp-2026-09-21.md). |
| RUN-06 | Done | Ops | Enforce the selected GitHub/OpenRouter network boundaries. | Required internal and selected external destinations work; disallowed worker/controller destinations are denied in observed tests. Node image pulls and DNS resolution are accounted for separately, matching RUN-02. [Local evidence](docs/validation/run-01-18-2026-09-17.md). |
| RUN-07 | Done | Ops | Start the paired services on the selected host. | Both services become ready with persistent storage and recover after a service restart; initial intake stays disabled until configuration is ready. [Local evidence](docs/validation/run-01-18-2026-09-17.md). |
| RUN-08 | Done | Ops | Validate authenticated local Deck access. | Deck is available at `https://localhost:8443` through the existing Podman/Kind port mapping without an interactive port-forward; trusted TLS, operator login and session persistence pass. LAN exposure and second-device testing are outside the current local acceptance scope. [Evidence](docs/validation/local-acceptance-follow-up-2026-09-17.md). |
| RUN-09 | Done | Marina | Validate the pinned harness against the selected OpenRouter model. | One bounded fixture run on local Kubernetes through the configured OpenRouter Responses endpoint exercises streaming, tool calls, durable usage accounting, and output collection with no implicit endpoint fallback; the repaired fixture reaches one PR, a distinct verification Job, current required CI, and downloadable evidence. [Live proof](docs/validation/poc-mvp-2026-09-21.md#run-09-complete-task-to-pr-proof). |
| RUN-11 | Done | Joint | Restart each control service during work. | Restarting Deck and Marina separately during execution and CI waiting preserves the original work and produces no duplicate branch, commit, or PR. [Four live restarts](docs/validation/poc-mvp-2026-09-21.md#run-11-final-restart-recovery). |
| RUN-12 | Done | Joint | Cancel a real active worker from Deck. | Authority is revoked, worker termination is observed before acknowledgement, and attempt resources are cleaned up while evidence remains accessible. [Live cancellation and retained evidence](docs/validation/poc-mvp-2026-09-21.md#run-12-active-cancellation). |
| RUN-13 | Done | Joint | Exercise failed and missing verification gates. | Failed local checks, failed CI, and a missing required CI check never produce a completed result; Deck explains the blocker or continued wait. [Live failed and missing gates](docs/validation/poc-mvp-2026-09-21.md#run-13-local-verification-failure). |
| RUN-14 | Done | Joint | Change a PR head after verification. | Evidence for the previous head cannot complete the task; the operator sees why a new verification attempt is needed. [Live changed-head rejection](docs/validation/poc-mvp-2026-09-21.md#run-14-changed-pr-head). |
| RUN-15 | Done | Joint | Interrupt required providers during a run. | Deck/model/Git outages stop unsafe progress, retain useful state, and recover or report a bounded blocker without duplicate external work. [Three live outages](docs/validation/poc-mvp-2026-09-21.md#run-15-provider-interruptions). |
| RUN-16 | Done | Marina | Exhaust configured execution budgets. | Time, token, attempt, and nonzero cost limits stop further admitted work and leave an inspectable reason; uncertain model usage retains its reservation. [Budget and attempt-boundary evidence](docs/validation/poc-mvp-2026-09-21.md#run-16-budget-denial-visibility), with live and deterministic scope distinguished. |
| RUN-18 | Done | Joint | Record the installation acceptance result. | A redacted report links task/run IDs, PR revision, image/configuration versions, and RUN-09 plus RUN-11 through RUN-16 results; unresolved failures stay open. [Acceptance report](docs/validation/poc-mvp-2026-09-21.md) and [redacted records](docs/validation/poc-mvp-2026-09-21.json). |

**2026-09-21 acceptance:** RUN-03/05/09 and RUN-11 through RUN-16 passed
within the supported local workflow; RUN-18 records the results. Restart defects
were fixed and retested. The baseline is restored, intake is disabled, and all
attempt resources are cleaned up. VALUE-01 was closed by the operator on
2026-09-21; representative human effort and provider/compute billing were not
measured. The classic-token pilot exception is recorded
in the [credential evidence](docs/validation/github-credential-2026-09-21.md), and
the [live report](docs/validation/poc-mvp-2026-09-21.md) records outcomes and limits.
This supersedes the missing-token and control-only status in older notes below.

**2026-09-18 scope:** the operator removed RUN-17 (deliberately failing or evicting an active worker) from first-release acceptance. Its ID is retired without renumbering the remaining tasks. RUN-09 and RUN-11 through RUN-16 remain required; see [decision 0007](docs/decisions/0007-sovereign-first-usable-release.md).

**2026-09-18 preparation:** the [workflow profile](deploy/kubernetes/local-podman-workflow/kustomization.yaml) reproducibly enables the configured workflow with intake still disabled. It renders against the existing private installation while preserving its credentials, image pins, egress rules and Deck PVC. Fresh local network/fixture checks passed. The operator confirmed the scoped Git credential is not available yet, so RUN-03/05/09/11–16/18 remain open or awaiting live evidence. See the [current preparation and acceptance report](docs/validation/poc-mvp-2026-09-21.md).

**2026-09-17 progress:** RUN-01/02/04/06/07 are complete for the local installation. A private [acceptance repository](https://github.com/jmlero/marina-acceptance), offline fixture checks and a pinned worker are prepared. The operator authorized €5 total test spend and requested a GLM trial; `z-ai/glm-5.3-flash` is now configured with conservative USD budget rates. Both GLM and Luna passed direct streaming/tool probes and produced the same fixture repair, each passing seven isolated regression tests. Six inference requests cost $0.0004802 in total; the [GLM comparison](docs/validation/glm-model-trial-2026-09-17.md) records costs, latency and limits of this evidence. RUN-03/05 still need the repository-scoped Git credential the operator will supply; RUN-09 still needs the pinned harness/gateway fixture run. The [local acceptance follow-up](docs/validation/local-acceptance-follow-up-2026-09-17.md) records fresh network, fixture and idle-restart checks, completes RUN-08 for authenticated localhost access, and removes RUN-10. RUN-03/05/09/11–18 still need the scoped Git credential and live workflow evidence. Intake remains disabled.

**First-release gate:** the local installation passes the acceptance exercises. Backup/restore work is deferred for the current pilot under decision 0010; persistent storage and restart recovery remain required. A running local Kind cluster or green mock tests alone do not satisfy the live workflow acceptance gate. Laptop-off operation is not part of this gate.

## 3. Make unattended operation recoverable

Use the same first installation. OPS-01/02 are deferred until backup recovery becomes an operator requirement; OPS-02 then depends on OPS-01. Retention work must preserve evidence needed by active runs and retained tasks.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| OPS-01 | Later | Ops | Automate a consistent quiesced backup when required. | After the operator reactivates backup work, one documented command backs up both databases with their WAL state, artifacts, identities, and required private configuration to the chosen destination. |
| OPS-02 | Later | Joint | Restore that backup into an isolated installation. | After OPS-01, services recover the same identities and work records, artifact digests validate, and reconciliation creates no duplicate external work. |
| OPS-03 | Done | Ops | Rehearse certificate renewal. | The installation can replace expiring certificates and trust without recreating operator/service identities or losing work. [Local renewal and identity evidence](docs/validation/first-release-operations-2026-09-17.md). |
| OPS-04 | Verify | Joint | Rehearse credential rotation. | Operator, service, and Git/model credential changes take effect with the documented session/revocation behavior; no credential appears in evidence or diagnostics. |
| OPS-05 | Done | Marina | Implement bounded artifact retention. | Retention rules and a dry-run report precede deletion; referenced/current evidence is preserved and expired unreferenced artifacts can be removed safely. [Implementation and verification](docs/validation/first-release-operations-2026-09-17.md). |
| OPS-06 | Done | Ops | Write a short recovery runbook from acceptance failures. | Disk-full, unavailable provider, stuck cancellation, and failed worker cases each have detection and recovery steps; no direct ledger editing is required. [Recovery runbook](deploy/kubernetes/first-release/OPERATIONS.md). Add restored-storage procedures when OPS-01/02 are activated. |

**2026-09-17 operations follow-up:** OPS-03/05/06 are complete. Operator/service
credential rotation passed locally; OPS-04 retains its live Git/model requirement.
A private model-key file is populated. The subsequent [GLM trial](docs/validation/glm-model-trial-2026-09-17.md)
records the current model, conservative rates, €5 total authorization, passing
provider probes and isolated fixture checks; scoped Git credentials remain absent. The operator
confirmed only local Podman/Kind is available. The later [local acceptance follow-up](docs/validation/local-acceptance-follow-up-2026-09-17.md) completes RUN-08 for localhost access and removes RUN-10; RUN-03/05/09/11–18 remain open or awaiting live evidence. Intake remains disabled.

## 4. Reuse project context

The product direction is accepted in [decision 0014](docs/decisions/0014-project-repository-and-knowledge.md): one GitHub repository per project, with reusable knowledge and custom instructions managed through Deck and assembled by Marina. CTX-02 through CTX-08 are implemented and verified against isolated local processes; see the [context validation report](docs/validation/ctx-02-08-2026-09-21.md). They reuse the existing project, binding, context-package, and journal foundations. Start with explicit project sources; an automatic memory or retrieval framework is not a prerequisite. Repository setup through Deck is tracked by CTX-08.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| CTX-01 | Done | Joint | Define the minimum project-context contract. | Ownership and references for repository instructions, selected documents, decisions, and conversation excerpts are recorded without creating a second authoritative ledger. [Accepted decision](docs/decisions/0014-project-repository-and-knowledge.md); implementation remains below. |
| CTX-02 | Done | Deck | Register and revise project context sources. | The operator can maintain custom instructions, project goals, selected documents, and approved decision references with versions, provenance, and superseded/revoked state through the public API; Git-owned content retains its canonical source. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-03 | Done | Marina | Assemble the existing context package in the active workflow. | Task, pinned repository, policy, and selected project sources form a persisted package delivered to the worker and reconstructible after restart. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-04 | Done | Joint | Attach bounded conversation context to a task. | Selected message IDs/revisions are captured at delegation and included as task data; unrelated conversations and subsequent edits do not silently enter the run. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-05 | Done | Marina | Enforce freshness and trust when using assembled context. | Stale, conflicting, revoked, and injected input cases cause omission or a focused clarification; retrieved text cannot expand authority. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-06 | Done | Deck | Show the task's interpreted outcome and context sources. | The operator can inspect what the agent received, where it came from, and which decisions apply without reading raw execution logs. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-07 | Done | Joint | Demonstrate context reuse across two tasks. | A second task uses an approved project decision without re-explanation; changing that decision affects only newly authorized work. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |
| CTX-08 | Done | Joint | Associate a project's repository through Deck. | The operator can associate one GitHub repository with an existing Deck project through a supported public contract; Marina owns the validated, versioned binding and credential references, Deck shows a safe summary, and active runs retain their original binding. [Evidence](docs/validation/ctx-02-08-2026-09-21.md). |

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
| INBOX-05 | Verify | Deck | Cover the main browser journey in the desktop browser. | Automated browser checks cover login, task creation, approval, cancellation, reconnect, and result/evidence access; conversation checks are added when available. Phone support is outside scope per operator direction on 2026-09-21. |

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

The operator declined the manually recorded pilot measurement and closed VALUE-01
on 2026-09-21. VALUE-02 remains a separate comparison after CHAT-10.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| VALUE-01 | Closed | Joint | Record human effort for representative tasks. | Closed at the operator's request on 2026-09-21; representative effort and cost measurement was declined. [Initial automated measurements and closure record](docs/validation/poc-mvp-2026-09-21.md#value-01-initial-measurement). |
| VALUE-02 | Open | Joint | Compare board and conversational delegation. | After CHAT-10, the same kinds of bounded work are compared for repeated context, necessary interventions, result quality, and total human effort; the next priority follows the findings. |

## 9. Later decisions, outside the active build list

These retain relevant long-term questions without carrying a speculative implementation backlog. Split approved work into small implementation rows only when its trigger is met.

LATER-02 retains its ID and is complete through AGENT-01/04/06; the other rows
remain deferred.

| ID | State | Owner | Small task | Done when |
| --- | --- | --- | --- | --- |
| LATER-01 | Later | Joint | Select a second project workflow after the pilot. | Evidence identifies whether multi-project configuration or concurrent work is actually needed; one concrete workflow is chosen before scheduling changes. |
| LATER-02 | Done | Marina | Prove the second harness through the agent-platform PoC. | LangChain diagnostic analysis and Codex repository work completed with shared execution controls and recorded usage under AGENT-01/04/06; [local proof](docs/validation/agent-02-06-2026-09-21.md). Additional runtime/provider alternatives remain deferred; no plugin marketplace is assumed. |
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
