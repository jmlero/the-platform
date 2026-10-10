# KINT-09 native repository cost admission

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate and native Codex.
- Result: the first native repository model request was rejected by Marina's
  cost gate before upstream inference. This is one cost-admission case, not
  proof of time exhaustion, post-call accounting, or lost final usage.

The temporary private profile selected fresh `kint09-repo-5` revision
`5707070a17218b6ac913fe51148255de5778b3c91f7f90a7b2f13af831990e49`
and doubled the **estimated input price** from 100,000 to 200,000
microdollars per million tokens. That estimate was deliberately changed only
for this fault test. Deck temporarily associated the project with disposable
`jmlero/marina-acceptance` at binding version 55. Only task
`tsk_f2794025da9a1c97677b` was admitted, and new Marina task intake was
disabled after claim.

Run `run_5d3a69ff4da518e02f07` used a 15-minute, 200,000-token and
**one-cent** task budget. Its reviewed plan limited changes to `src`, allowed
one review PR, and prohibited merge or deployment. After approval, the native
worker reached Marina's model gateway. Marina published one
`model.budget_exhausted` event with `cost budget cannot admit model request`.
The run and task blocked. The copied journal passed SQLite `integrity_check`:
execution `exec_b1c0b7a7b5e6d2a19c9e3905` had a 10,000-microdollar cost
limit, zero known used tokens and cost, **zero model-call records**, and one
failed runtime attempt with cleanup completed. Its outbox had no remaining
unpublished event after restart. Read-only GitHub checks found no branch or
PR for this execution.

The original model estimate and legacy repository-engine selection were
restored from the private saved workflow; `kint16-chat-11` remained selected.
Deck restored `jmlero/context-lens` at revision 24, binding version 57.
Marina and Deck were Ready with new task intake disabled. The temporary native
template, ModelConfig, Secret, exact credential Role and RoleBinding were
deleted. The [KINT-09 core matrix](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md#mandatory-failure-matrix)
still has other cost/time/usage and repository failure cases, so KINT-11
remains gated.
