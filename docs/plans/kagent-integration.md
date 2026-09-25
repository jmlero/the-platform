# Integrate kagent and remove Marina's redundant agent engine

- Date: 2026-09-22
- Status: KINT-01–08 passed their scoped gates; permanent installation, core failure cases, cutover and removal pending
- Decisions: [0017](../decisions/0017-kagent-core-engine.md); gates and scope narrowed by [0023](../decisions/0023-focus-kagent-cutover.md)
- Tracking: KINT in [the backlog](../backlog.md)
- Research: [adoption analysis](https://github.com/jmlero/the-platform/blob/744204a/docs/kagent-adoption.md)

## Outcome

Make kagent the core agent engine for newly authorized Marina workloads. Keep
Marina as the durable workflow and governance service between Deck, kagent,
verification, Git and CI. Remove Marina code whose responsibilities have moved
to kagent; completion includes deletion and installation simplification.

The target is one agent engine, not permanent support for both custom worker
hosting and kagent. A separate deterministic verification Job can remain: it
does not run an agent loop and preserves independent acceptance.

The implementation baseline is the researched kagent `v1.0.0-alpha1`, commit
`850c464726881317997c32515f8b621b949d6ce1`. KINT-01 locks compatible charts,
images, Substrate and APIs before code depends on them. The analysis distinguishes
this source from the moving `0.x` docs; do not mix examples or infer compatibility.
The [KINT-01/02 trial](../validation/kint-01-02-2026-09-22.md) locked the candidate
and proved isolated native runtime, persistence, resource feasibility and teardown
on local Kind. The trial was removed afterward. [KINT-03](../validation/kint-03-2026-09-22.md)
adds the lifecycle adapter and durable correlation with isolated local recovery
proof; governed workload dispatch and legacy deletion remain pending. See the [exact lock and installation guide](../../deploy/kubernetes/kagent-trial/README.md)
and [symbol/caller removal inventory](kagent-integration.md#removal-inventory).
The [KINT-06 local pilot](../validation/kint-06-2026-09-23.md) produced one
verified PR through native Codex, with the trial removed afterward.

## Responsibility boundary

| Owner | Responsibilities after migration |
| --- | --- |
| Deck | Intent, authorization, plans, decisions, commands, conversation identity and operator-visible results. |
| Marina | Deck claims/leases/fences, immutable workflow inputs, per-attempt authority and budget accounting, workflow transitions, source publication, independent verification, evidence admission, cross-system recovery and outbox. |
| kagent/Substrate | Harness execution, agent loop and framework state, compiled runtime revisions, engine task history, runtime lifecycle and supported interaction continuation. |
| Git/CI | Authoritative repository, PR-head and check observations. |

Use public kagent lifecycle/A2A APIs behind a narrow Marina adapter. Keep Deck's
HTTP/JSON contract and separate stores. Marina retains correlation records and
workflow checkpoints, not another copy of kagent's complete event history.
Engine completion is an observation, never sufficient evidence of task success.

Prefer native Codex for repository work. The chat agent uses kagent's BYO path.
The LangChain diagnostic workload is retired by
[decision 0023](../decisions/0023-focus-kagent-cutover.md): KINT-07 proved it on
kagent, but it is not a cutover target, and KINT-12–14 delete it.

## Sequence and acceptance

All KINT tasks start **Open**. Mark a task **Verify** only after its implementation
exists and named installation proof remains. Mark **Done** only with linked
evidence. Dependencies below are completion dependencies; contract design can
proceed before a live environment is ready. Implementation fixes follow failing
test → smallest passing change → relevant checks.

### Phase 1 — lock the boundary and prove local feasibility

**KINT-01 — Inventory and pin the integration. Owner: Marina/Ops.**

- Lock source/chart/image digests, public API versions, transitive dependencies,
  licenses, storage and runtime requirements. Record a minimal local topology,
  authentication/egress policy and measured or estimated resource requirements.
- Turn the deletion map below into a file/symbol inventory: replacement, callers,
  retained behavior, acceptance test and task authorizing removal. Establish the
  current test and resource baseline before changes.
- Exit: reproducible dependency/configuration lock and reviewed ownership/removal
  inventory. A dependency or capability that cannot meet current requirements is
  a concrete blocker, not a reason to silently weaken the requirements.

**KINT-02 — Install an isolated local engine. Owner: Ops. Depends: 01.**

- Render and install only required components into trial-owned resources on
  `kind-marina-local`, using `podman-machine-default-root` explicitly. Verify
  ARM64 images, Substrate prerequisites, supported snapshot storage and persistence.
- Narrow RBAC and direct API access, disable unrelated tools, and inventory
  cluster-scoped resources. Separate preparation downloads from runtime egress.
- Exit: engine instance runs and survives component replacement with its recorded
  state; CPU/memory/storage are measured; teardown preserves existing resources.
  Keep the current Deck installation available at `https://localhost:8443`.

### Phase 2 — integrate execution and preserve governance

**KINT-03 — Add the engine adapter and durable correlation. Owner: Marina.
Depends: 01; live proof also depends on 02.**

Completed within the [documented lifecycle boundary](../validation/kint-03-2026-09-22.md).
The adapter is not yet selected by installed workflows. Live proof exercised
lost creation/deletion responses and an interrupted-before-admission message;
accepted-message recovery and artifact collection have deterministic gRPC proof.

- Implement start, lookup by recorded key, inspect, cancellation observation,
  artifact collection and cleanup at the existing runtime boundary. Add only
  the minimum types needed for a real kagent integration.
- Persist the selected backend/version, immutable revision, profile/context
  digests, instance route and A2A context/task/message IDs. Recover ambiguous
  instance creation separately from ambiguous message delivery.
- Exit: duplicate starts and lost responses converge to the original attempt or
  a truthful blocker. Template edits cannot select unauthorized runtime inputs.
  Reconnect/restart never creates replacement work blindly.

**KINT-04 — Connect governed model, context and source access. Owner: Marina.
Depends: 03.**

Completed: deterministic checks and real isolated local Substrate injection,
credential rotation, cached-credential revocation/expiry, bypass denial and safe
teardown passed. Deck authority and model responses were synthetic; installed
workload dispatch and end-to-end failure acceptance remain later gates.
See [KINT-04 evidence](../validation/kint-04-2026-09-22.md) and the
[gateway contract and installation recipe](../../marina/internal/workerapi/README.md).
Gateway credentials and templates are attempt-exclusive; template names cannot
be reused after cleanup. Native web-search declarations are removed before
forwarding without granting provider-hosted tools. Existing profiles remain on
the supported runtime.

- Reuse external admission, current-fence checks, token/time/cost reservations,
  unknown-usage accounting, context revocation and allowed-path/source controls.
  Adapt native runtime requests to the supported contract without broadening it.
- Make every model/tool call, including retries, attributable to its attempt.
  Test Responses streaming, TLS, credential rotation and denied bypass routes.
  Use the smallest MCP bridge only where kagent needs it.
- Exit: cancellation/expiry revokes new calls independently of credential caches;
  agents receive no upstream model credential, Git write credential or Kubernetes
  token. Optional memory/delegation cannot bypass budgets or context selection.

**KINT-05 — Separate results and independent verification from worker hosting.
Owner: Marina. Depends: 03–04.**

Completed at the deterministic boundary in [KINT-05 evidence](../validation/kint-05-2026-09-23.md).
The installed workload backend remains unchanged; KINT-06/07 must emit and
prove the selected inline result artifact through their actual harnesses.

- Extract the existing result/check formats, safe change validation and clean
  verification behavior from mixed worker code only where needed by both paths.
  Translate kagent artifacts into digest-bound Marina evidence; validate actual
  bytes, scope, source revision and bounded sizes rather than trusting URLs/text.
- Keep a minimal deterministic verifier Job initially, with no model authority
  and a clean checkout. Bind its output to the exact published revision.
- Exit: malformed results, disallowed changes, missing artifacts, false success,
  and failed checks cannot pass acceptance. The verifier works without launching
  Codex or Python agent code.

**KINT-06 — Move repository implementation to native kagent Codex. Owner: Marina.
Depends: 02–05.**

Completed for one bounded local Kind pilot in [KINT-06 evidence](../validation/kint-06-2026-09-23.md).
The verified PR remains open and unmerged. The trial was removed after proof;
two-workload cutover and the broader live failure matrix remain KINT-09/11.

- Run one Deck-authorized task using frozen context/source/profile and native
  Codex. Marina publishes permitted changes, runs independent verification and
  observes required CI at the same PR head.
- Exit: one bounded task produces exactly one verified PR ready for human review
  or a truthful blocker. Successful-path evidence is required before migration;
  merge/deploy authority, automatic repair and new concurrency remain excluded.

**KINT-07 — Move the diagnostic profile to kagent. Owner: Marina.
Depends: 02–05.**

Completed in the [local KINT-07/08 acceptance](../validation/kint-07-08-2026-09-23.md):
context-enabled report and forced denied-source cases passed through real kagent.

- Package the pinned LangChain workload behind kagent's BYO contract; retain its
  immutable fixture, governed context read and independent Go report validator.
  Remove dependence on Marina launching its Python subprocess as an agent Job.
- Publish a new exact profile revision; keep historical profile digests readable.
  Replacing LangChain with native ADK requires a separate explicit harness choice.
- Exit: both context-enabled and denied-access cases work through kagent; the
  report validates without repository authority or source publication steps.

**KINT-08 — Project engine state and bridge pending questions. Owner: Joint.
Depends: 03–04.**

Completed in the [local KINT-07/08 acceptance](../validation/kint-07-08-2026-09-23.md):
question projection, restart, one correlated answer and validated result passed.

- Map engine progress, blockers, usage and pending question IDs to durable Marina
  records and Deck's supported surfaces. Correlate answers with exact pending
  operations; reject stale, duplicated or cancelled answers. Avoid a second
  custom conversation runtime or normal operator access directly to kagent.
- If a public contract addition is necessary, update OpenAPI, fixtures, consumer
  expectations and real-process conformance together. Do not advertise unsupported
  approvals or operator pause/resume from upstream capability names.
- Exit: a minimal question/answer and result projection survives disconnect and
  service restart without duplicate action or publication. Full conversation UI,
  task delegation and browser acceptance remain the existing CHAT tasks.

### Phase 3 — install for real, prove the core cases and cut over

**KINT-16 — Install kagent permanently and turn on the chat agent. Owner:
Ops/Marina. Depends: 01–08.**

- Install the pinned kagent and Substrate into the regular local installation,
  not a trial, and enable the conversation profile with a fixed delegation
  budget. Repository work stays on the current runtime until KINT-11.
- Exit: the chat agent answers in Deck threads at `https://localhost:8443`,
  history stays readable, and the local installation guide describes the setup.
  This period with two engines ends with KINT-12–14.

**KINT-09 — Run the core failure cases. Owner: Joint. Depends: 06–08.**

- Run the rows marked **core** in the [mandatory matrix](#mandatory-failure-matrix)
  against real Deck/Marina/kagent processes on local Kubernetes with repository
  work ([decision 0023](../decisions/0023-focus-kagent-cutover.md)). The other
  rows move to KINT-17.
- Cover the surviving verification Job and any child process/tool termination.
  Safe blockers match the existing release boundary; transparent step recovery
  and HA are not newly imposed requirements.
- Exit: no duplicate external mutations, excess budget, premature cancellation
  or false completion. Store redacted evidence under `docs/validation/`.

**KINT-10 — Rehearse upgrade and rollback. Owner: Ops/Marina. Depends: 16.**

- Upgrade the installation with active kagent data; history and in-flight work
  survive. Roll back by restoring the previous release and the retained data
  copy. A schema-aware downgrade and the full footprint measurement move to
  KINT-17. Backup automation remains deferred.
- Exit: documented supported versions, a reproducible upgrade and rollback,
  retained state/evidence, and operation within existing environment/spend limits.

**KINT-11 — Cut over repository work to kagent. Owner: Marina/Ops.
Depends: 09–10.**

- Drain old executions, reconcile uncertain source effects, then change the
  default for newly authorized repository work. Never switch an active attempt
  between engines.
- Keep prior release artifacts for rollback. Route rollback only to newly
  authorized tasks after stopping and reconciling kagent-owned work.
- Exit: repository work and the chat agent use kagent, no legacy execution or
  cleanup remains active, and old records/artifacts remain inspectable. KINT-12–14
  follow; the old backend has no indefinite fallback role.

**KINT-17 — Run the remaining failure cases. Owner: Joint. Later.**

The matrix rows not marked core, plus a schema-aware rollback and the full
footprint measurement. Required before the platform runs on an independent
cluster or for other users.

### Phase 4 — remove obsolete implementation

**KINT-12 — Delete the replaced agent launch paths. Owner: Marina.
Depends: 11.**

Remove custom Codex invocation, Python subprocess agent hosting, agent-specific
Job creation and obsolete worker command/protocol branches from the inventory.
Preserve only the independently justified verifier and BYO workload behavior.
Exit: neither migrated profile can select the legacy agent runtime, dead imports
and flags are gone, and relevant acceptance tests execute against the kagent path.

**KINT-13 — Simplify runtime and journal code. Owner: Marina. Depends: 12.**

Remove obsolete provider-specific polling, retry, cleanup and worker-input state
that kagent now owns. Keep durable side-effect intents, identity mapping, Deck
leases, budget reservations, outbox, workflow checkpoints and evidence rules.
Use forward migrations if needed; never edit old migrations or drop historical
records just because their old execution path has been deleted. Exit: each
remaining runtime/journal field has an active workflow, recovery or history-reader
purpose; old data loads and artifact inspection still works.

**KINT-14 — Remove obsolete images, configuration and installation plumbing.
Owner: Marina/Ops. Depends: 12–13.**

Delete unused image stages, dependencies, environment variables, grants, worker
manifests and setup branches. Keep only what the verifier or supported BYO
workload still uses. Remove obsolete tests only after transferring their required
behavior coverage. Update installation/runbooks and report before/after code,
image and service ownership. Exit: a fresh install needs no legacy agent image or
launch configuration, and the upgrade path preserves existing data.

**KINT-15 — Verify the simplified release and close the migration. Owner: Joint.
Depends: 14.**

Run deterministic product/installation checks and real-process contract
conformance, then repeat live repository and chat work and the affected failure cases
on the final code after deletion. Publish a final removal inventory, retained-code
rationale, supported dependency versions and local acceptance report. Update
architecture, visions, AGENTS guidance, READMEs and backlog claims to the actual
deployed boundary. Exit: all completion criteria below have linked evidence.

## Concrete deletion map

Paths are current starting points, not permission to delete whole directories.
KINT-01 identifies exact symbols; KINT-12–14 remove them after their replacements
and retained callers pass. A small moved helper is acceptable; renaming a legacy
engine and keeping it active does not count as removal.

| Current code/configuration | Planned treatment | Removal gate |
| --- | --- | --- |
| `marina/internal/flow/runtime.go` | Replace hard-coded Codex/Python command construction and agent-worker input transport with engine dispatch. Preserve workflow admission and result acceptance. | 06–09, 11–12 |
| `marina/cmd/marina/workers.go`, worker branches in `main.go` | Replace unconditional Kubernetes agent adapter wiring; remove the legacy agent CLI entry path. Keep only verifier entry/configuration if required. | 05–07, 11–12 |
| `marina/internal/worker/runner.go` | Remove agent command execution; preserve/extract clean checkout, safe path/change validation, check execution and result schema used by verification. | 05–06, 12 |
| `marina/internal/worker/analysis.go` | Delete with the retired diagnostic workload ([decision 0023](../decisions/0023-focus-kagent-cutover.md)). | 11–12 |
| `marina/internal/worker/remote.go`, `authority.go` | Remove agent-specific input polling/result upload/watchdog plumbing only after equivalent fencing and termination are proven; retain any verifier-specific need. | 04–05, 09, 12 |
| `marina/internal/kubernetes/adapter.go` | Remove coding/analysis Job support; narrow to independent verification if Jobs remain. Decouple shared credential helpers from the Kubernetes package. | 05, 09, 12–13 |
| `marina/internal/workerapi/server.go`, `model.go`, `tools.go` | Remove unused legacy input/result/authority routes. Retain or adapt budget admission, context access, restricted source access and authenticated artifact delivery. | 04–05, 09, 12–14 |
| `marina/internal/runtime`, runtime records in `journal/sqlite`, `execution/recovery.go` | Reduce provider-specific execution machinery; preserve attempt identity, accounting, cross-system recovery and historical readers. | 03, 09, 13 |
| `marina/internal/flow/config.go`, `cmd/marina/profile.go` | Remove old agent-image/command settings; pin engine profile/revision. Keep required verifier settings and historical digest semantics. | 06–07, 11, 14 |
| `marina/Dockerfile.worker`, `marina/Dockerfile.analysis-worker`, `marina/harnesses/langchain` | Replace agent-worker packaging with native Codex/BYO packaging; strip coding dependencies from verifier image. Delete the analysis image and LangChain harness (decision 0023). | 05–07, 12, 14 |
| `deploy/kubernetes/first-release/workers`, installation generators and validation scripts | Narrow RBAC/quotas/network policy to remaining verifier needs; remove old agent-image provisioning and test assumptions. Add pinned engine installation separately. | 02, 10–11, 14 |
| `marina/internal/deck`, `workcontrol`, `policy`, `contextpack`, `sourcecontrol`, `artifacts` | Retain product contracts and evidence/governance behavior. Remove only proven dead callers/helpers; these packages do not become redundant merely because execution moves. | 13–15 |
| `marina/internal/reconcile`, `flow`, `journal/sqlite` | Keep Deck reconciliation, workflow state and outbox. Remove only engine-owned details identified by the inventory. | 13–15 |

## Completion and verification

The migration is complete only when:

1. Repository work runs through native Codex and the chat agent through BYO on
   kagent; the diagnostic workload is removed, and no new task can select
   Marina's old agent engine.
2. Independent checks, current-source evidence, budget enforcement, cancellation
   and restart behavior pass the final build's acceptance gates.
3. Every removal candidate is deleted or has a specific retained purpose and
   tests. Temporary compatibility code has no unowned or indefinite exception.
4. Old tasks, decisions, usage and artifacts remain inspectable; removed live
   execution code is not needed to read history. Rollback uses retained releases,
   not a permanent second production execution path in the new binary.
5. A fresh install and the documented upgrade work without obsolete agent images,
   credentials, manifests or configuration. Measured maintenance/resource costs
   and any remaining limitations are recorded honestly.

Run the narrowest meaningful tests first, then `make -C marina check` for Marina
changes and `make check-installation` for installation changes. Joint API changes
require `make check` and `make conformance`; final KINT-15 runs both regardless
because this is a cross-component migration. Add `make -C deck test check` when
Deck changes. Live acceptance uses the existing authorized local profile and
bounded credentials/task/model settings; deterministic doubles do not prove it.

CHAT-04–10 build on this engine integration and keep their own acceptance
criteria. AUTO tasks follow KINT-11 and must use the selected engine. Do not grow a parallel custom agent runtime
while these tasks are pending. Product UI/intent design and required fixes can
continue independently.

If a hard compatibility or safety gate fails, keep the current working release,
record the exact failure and bounded remedy, and stop dependent cutover/deletion.
That is a blocker to solve or a reason to revisit the direction with evidence;
it is not permission to conceal a regression or leave an accidental hybrid.

## Mandatory failure matrix

KINT-09 runs the rows marked **core** against real Deck, Marina and kagent
processes with repository work; KINT-17 runs the rest later
([decision 0023](../decisions/0023-focus-kagent-cutover.md)). Carried over from
the adoption analysis.

| Test | Passing observation | Gate |
| --- | --- | --- |
| Duplicate start/send; lose instance-create and send responses separately | Original instance/attempt recovered or safely blocked; no replacement coding work or duplicate billed invocation through blind retry. | core |
| Restart Marina, kagent gateway, agent compute and engine database separately during work | Inspect the original identities; resume where proven or block truthfully. No invented completion or uncontrolled second attempt. | core |
| Disconnect event stream; duplicate/out-of-order events | Reconstruct committed engine state and publish each logical Deck transition/result once. | KINT-17 |
| Cancel during a model call, tool call, and waiting-for-input state | Admission revoked; termination observed; late responses cannot publish new mutations or revive the run. | core |
| Lose Deck/lease ownership; expire or revoke context | Further calls denied and compute stopped under the current fence rules. Credential-cache freshness cannot substitute for revocation. | KINT-17 |
| Exhaust time/tokens/cost; lose final streaming usage | Deny further consumption, keep unresolved reservations, report known and unknown usage separately, including internal retries. | core |
| Bypass Marina via model egress, MCP, engine API, or child delegation | No unauthorized provider/tool access or task invocation; no upstream secret, Git write credential, or Kubernetes token available to agent code. | KINT-17 |
| Fail verification; change PR head; fail required CI | Evidence becomes failed/stale and Deck cannot show successful completion. | core |
| Lose commit/PR/publication response | Reconcile the original source operation and outbox receipt; no duplicate PR or false failure followed by a new mutation. | core |
| Edit template/model/tools while an instance starts or runs | Authorized revision remains pinned, or mismatch blocks before invocation. | KINT-17 |
| Answer a question twice, after expiry, or after cancel | Correct pending operation only; reject stale answers and preserve the original human decision. | KINT-17 |
| Fail artifact storage or cleanup; upgrade and roll back | No completion without retained evidence; cleanup remains visible/retryable and existing history remains readable. | KINT-17 |

## Removal inventory

Carried over from the KINT-01 inventory, reviewed against the working tree on
2026-09-23. This is a removal specification, not authorization to delete before
KINT-11. The current runtime is unchanged.
The [dependency lock](../../deploy/kubernetes/kagent-trial/lock.json) and
[trial guide](../../deploy/kubernetes/kagent-trial/README.md) define the candidate.

Paths below are relative to `marina/`, unless stated otherwise. Test names refer
to current coverage that must survive the replacement; they are not evidence
that the future kagent adapter passes. The named KINT gates require new tests
against the replacement and the live failure matrix.

| File and symbol / configuration | Current callers and replacement | Retained behavior and required proof | Removal task |
| --- | --- | --- | --- |
| `internal/flow/runtime.go`: `Controller.worker`, implement/analyze command construction, `worker-input:` checkpoint | Repository and analysis flow call `worker`; dispatch through the KINT-03 adapter instead of constructing `codex exec` / Python commands. | Keep immutable specifications, attempt limits, authority checks, result digest/stage/source binding. `TestRuntimeAttemptLimitSurvivesRestartAndAllowsIdempotentReplay`; KINT-03/05/06/07/09. Verification still needs its own input. | 12–13 |
| `cmd/marina/workers.go`: `configureWorkers`, `kubernetes.New` and `AdditionalImages` wiring | `serve` wires the runtime/gateway. Select the pinned engine revision for agent attempts; retain a verifier adapter. | Source binding, scoped gateway, TLS/credential validation, artifact store and per-execution configuration remain. `TestConfigurationRequiresExplicitDestinationsAndVerification`; KINT-04/05/09. | 12, 14 |
| `cmd/marina/main.go`: worker command branch | Worker image invokes the CLI entry; replace agent entry with upstream/native or BYO runtime entry. | Keep a separately usable deterministic verifier command. CLI worker deletion follows image/caller inventory, not the command name alone. | 12 |
| `internal/worker/runner.go`: agent branch of `Run`, `promptFor`, `harnessFailure`, `parseHarness` | `RunRemote` calls `Run`; native Codex adapter supplies the agent loop and structured result conversion. | KINT-05 moved result/check formats and path validation to `internal/workresult`, shared input to `internal/worker/input.go`, and clean checkout/check execution/process termination to `internal/worker/verification.go`. Retain those independently justified pieces when deleting the agent branch. `TestVerificationChecksThePinnedRevisionInAFreshWorkspace`, `TestChangesCannotEscapeAuthorizedPathsOrModifyControlFiles`, `TestGitReadUsesOneScopedBearerHeader`. | 12 |
| `internal/worker/analysis.go`: `runAnalysis` | `Run` analysis branch hosts Python. The diagnostic workload is retired (decision 0023). | Delete with the workload, including the `internal/analysis` validator once no retained caller needs it. `TestAnalysisWorkerRunsWithoutGitOrCodingHarness`, `TestAnalysisEvidenceBindsContextForVersionTwo`; repeat scope/invalid-report cases through BYO. | 12 |
| `internal/worker/remote.go`: `RunRemote`; `authority.go`: `watchAuthority`, `retryGatewayRead` | Legacy worker entry reads inputs, posts results and polls authority. Agent path replaced by engine transport and externally enforced authority. | A verifier may still require scoped input/result transfer and termination watchdog. `TestAuthorityWatchdogStopsImmediatelyOnRevocation`, `TestAuthorityWatchdogNeverOutlivesLastConfirmedLease`, temporary-outage tests; live termination under KINT-09. | 12 |
| `internal/kubernetes/adapter.go`: `Adapter.Start`, `manifest`, `Inspect`, `InspectByKey`, `Cancel`, `Cleanup`, `jobName` | `configureWorkers` supplies this adapter for all workloads; KINT-03 replaces coding/analysis Job lifecycle. | Keep narrow verifier Job support. Preserve lost-response lookup, cancellation waiting for observed termination, cleanup and pinned image checks. `TestStartReusesExactJobAfterLostResponseAndRejectsChangedSpecification`, `TestCancelIsNotAcknowledgedUntilJobTerminationIsObserved`. | 12–13 |
| `internal/kubernetes/adapter.go`: `Token` | Worker API and Job creation share attempt credential derivation. Move only if surviving callers need it outside the verifier adapter. | Attempt/fence/expiry binding and constant-time verification must survive. `TestWorkerCredentialCannotOutliveProviderFence`; KINT-04. | 13 |
| `internal/workerapi/server.go`: `Input`, `input`, `authority`, `result` routes | `RunRemote` consumes the legacy protocol; remove agent-only transport after replacement. | Retain verifier transfer, actual-byte validation and authenticated `artifact` delivery where still used. `authorize`, `admit`, `liveContext` and restricted `source` remain governance requirements. | 12–14 |
| `internal/workerapi/model.go`: `model`, `forbiddenModelInput`, `localTools`, `analysisTools`, `price`; `tools.go`: `contextTool` | Workers currently call Marina's gateway. Adapt native Responses/BYO requests without bypassing admission. | Retain reservations, terminal-usage accounting, scope, cancellation and context revocation. `TestModelBudgetRequiresCompleteTerminalUsage`, `TestDeniedModelBudgetLeavesOneDurableExplanation`, `TestContextToolEnforcesFrozenAllowlistAndLiveRevocation`, `TestAnalysisGatewayDeniesGitAndBindsReportToFrozenInputs`. | Retain/adapt in 04; remove only dead transport in 14 |
| `internal/runtime/runtime.go`: `Specification`, `Attempt`, `Observation`, `Adapter`, runtime `PlanRecovery` | Flow and journal use the runtime port. Keep a narrow engine mapping; remove Job-specific assumptions after both workloads move. | Immutable revision/context/profile, recorded key/handle, explicit ambiguity, capability limits and historical decoding remain. `TestSpecificationIsBoundedAndContentAddressed`, `TestPlanRecoveryMakesAmbiguousRuntimeBehaviorExplicit`; KINT-03/09. | 13 |
| `internal/journal/sqlite/runtime_records.go`: `EnsureRuntimeAttempt`, `MarkRuntimeStarting`, `RecordRuntimeObservation`, cleanup/artifact records | Flow, gateway, diagnostics and retention use these records. Retain correlation and historical readers; identify obsolete fields only after KINT-03 fixes the actual mapping. | Do not delete attempt/usage/evidence history or edit old migrations. `TestRuntimeAttemptsPersistSpecificationObservationArtifactsAndCleanup`, restart/attempt-limit tests. Every proposed column removal requires a caller and migration review. | 13 |
| `internal/execution/recovery.go`: `PlanRecovery`; `internal/flow/runtime.go`: `stop`, `Command`, `cleanup` | Reconciliation uses these to enforce workflow state, leases and termination. | Retain cross-system recovery and command semantics; simplify only duplicated engine polling. `TestRestartPlannerUsesPersistedActiveStateWithoutClaiming`, cancellation tests, KINT-09 failure matrix. | Retain; narrow in 13 |
| `internal/flow/config.go`: `Config`, `Validate`, `Digest`, `AnalysisDigest`; `cmd/marina/profile.go`: `runWorkloadProfile` | Installation loader and discovery pin current worker/harness settings. New profiles pin engine revisions. | Keep old profile digests readable and withdrawn selections rejected. No active-attempt backend switch. Config tests plus KINT-10 upgrade/history proof. | 14 |
| `Dockerfile.worker`, `Dockerfile.analysis-worker`, `harnesses/langchain/run.py`, `requirements.txt` | Installation builds and pins worker images. Native Codex replaces custom coding image; the diagnostic workload is retired (decision 0023). | Strip Codex/Python agent dependencies from verifier image and retain trusted offline checks. Delete the analysis image and LangChain harness. | 14 |
| Root `deploy/kubernetes/first-release/workers/`, `scripts/prepare-installation.py`, `prepare-agent-installation.py`, acceptance helpers | Installation provisions Job RBAC, quota, network isolation and images. Replace agent provisioning with the pinned engine; preserve verifier needs. | Transfer network/fence/cancel proof, data-preserving rollback, credentials and historical inspection before deleting old setup paths. `make check-installation`, KINT-09/10/15. | 14 |
| `internal/deck`, `workcontrol`, `policy`, `contextpack`, `sourcecontrol`, `artifacts`, `analysis`, workflow reconciliation and outbox | Deck contract, source publication and evidence acceptance are Marina responsibilities. | Retain. Engine success cannot complete a Deck task. Keep leases, immutable acquisition/context, source-current CI, budget accounting, artifact integrity and idempotent outbox. Run product checks and real-process conformance after joint changes. | No package-wide deletion |

No implementation is removed in KINT-01/02. Remaining-column decisions are
deliberately deferred to the implemented adapter rather than guessing which
durable correlation fields it will need. KINT-12–14 must amend this inventory with
the actual deleted symbols and retained callers; renamed legacy hosting does not
count as removal.
