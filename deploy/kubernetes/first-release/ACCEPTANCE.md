# Remaining live acceptance exercises

This extends the [installation guide](README.md). Use one configured disposable
repository, pinned image/model, trusted checks and required CI. Configuration
preflight must pass; record a total operator-approved test-spend limit before
enabling intake. Keep source/provider credentials out of tasks and evidence.
These are procedures, not passing results.

For each exercise record: UTC times; task/run/attempt and Job IDs; Git branch,
commit and PR URL/head; image/configuration digests; expected and observed result;
artifact digests/download results; token/cost observations; cleanup result; and
any unresolved blocker. Never record a credential, worker token or raw Secret.
Use a separate bounded task where a preceding exercise terminates the run.

| ID | Exercise | Required observation |
| --- | --- | --- |
| RUN-09 | Run the known clamp regression through the pinned Codex harness and explicit OpenRouter Responses endpoint. Inspect streamed tool activity, output collection and persisted usage. | The exact configured model supports the harness; code and a result artifact arrive; tool calls, streaming and accounting work without endpoint fallback. HTTPS reachability alone does not pass. |
| RUN-10 | On a host independent of the Mac, create/authorize the regression task in Deck, disconnect the Mac, then return from another device. | One PR, a separate clean verification Job at its head, current required CI and downloadable digest-checked evidence. Record the disconnected interval and second-device access. Local Kind cannot pass this gate. |
| RUN-11 | During implementation, restart Deck and Marina separately; repeat both restarts during CI waiting. Record Job, branch and PR identities before and after each restart. | Exact run/attempt recovery, durable output and no duplicate branch, commit or PR. An idle restart is only preparation. |
| RUN-12 | Cancel an active implementation task using Deck. Observe the command, attempt gateway, Pod termination and cleanup in order. | New model/source work is denied, termination precedes acknowledgement, attempt resources disappear, and already-collected evidence remains accessible. Do not substitute deletion of a probe Job. |
| RUN-13 | Use three controlled variants: a trusted local check that fails; a required CI check that reports failure; and a required check name that never appears. Keep CI/workflow changes outside the agent's allowed paths. | Failed gates block with an explanation. Missing CI waits or reaches a bounded timeout. None completes. Preserve the exact check/head/result as evidence. |
| RUN-14 | After verification but before completion, make a normal additional commit to the disposable PR branch under operator authority. Do not force-push. | Previous-head evidence cannot complete the task; the displayed blocker identifies the changed revision and need for a new verified attempt. |
| RUN-15 | Separately interrupt Deck, the model path and the Git path during an active run. Save each original Deployment/network rule before changing it and restore that exact resource afterwards. Do not rely on an additive deny policy while an allow policy remains. | Authority expires safely, incomplete/uncertain model usage stays reserved, persisted state survives, and restoration either recovers the original work or produces a bounded blocker without duplicated external work. |
| RUN-16 | Use separate valid tasks to exhaust wall time, tokens and a nonzero cost cap using configured nonzero model rates. Exercise the two-attempt boundary with failed verification and confirm that no third implementation is admitted. Interrupt one admitted model response to test uncertain usage. | Each cap stops further admitted work and remains inspectable; no hidden attempt or cleared reservation permits extra execution. Record provider-side spend separately from gateway admission accounting. |
| RUN-17 | Fail or evict only the identified Marina-managed worker Pod during implementation. Preserve its Job/attempt identity and termination observations. | Failure cannot count as completion; cleanup and the original blocker are visible. A separately authorized follow-up demonstrates the supported recovery path without overwriting old evidence. |
| RUN-18 | Assemble the redacted results above, including failures and missing prerequisites. | Each completed acceptance condition has concrete evidence; unresolved cases remain open. Local observations and the independent-host result are explicitly distinguished. |

Use the [operations runbook](OPERATIONS.md) for containment and recovery. Review
the task status, active Jobs and restored policies after every exercise before
starting another. Keep intake disabled when the chosen fixture/model/budget is
not ready. The first workflow stops at review-ready PRs or blockers; it has no
automatic retry/revision, merge or deployment authority.
