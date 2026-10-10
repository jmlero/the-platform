# KINT-09 native repository fault checks on the permanent local installation

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate and native Codex.
- Result: a real model call was cancelled and fenced; a separate insufficient
  token budget was denied. KINT-09 remains **Open** because the other core
  failure rows have not passed here.

## Repository association and bounded setup

The local private repository profile was configured for the disposable
`jmlero/marina-acceptance` repository while Deck still recorded
`jmlero/context-lens`. A first task, `tsk_7aa6dd70bde2e24b22e7`, blocked
before a plan or model call with the truthful original-binding/profile-change
reason. Marina now permits a versioned association request to catch up with
local private profile revisions only when Deck's exact observed binding is in
Marina's immutable repository history. A test rejects a different historical
binding. `make -C marina check` passed, and the updated Marina image was
deployed locally. Deck then explicitly associated the project with the
disposable repository for these tests; the base revision was
`10a96c6221f5c8a128ccb2617d8be27213d7b296`.

The native Codex attempt used a fresh template, ModelConfig and Secret with an
exact Secret grant. Only the named test task could enter Marina; intake was
disabled again after each claim. The approved plan allowed changes under
`src`, one PR for review, and no merge or deployment.

## Budget denial before inference

Task `tsk_6bfeb503d64e80319bb3` and run
`run_d5d123e2e6396c7b5d21` had a 20,000-token limit. Codex sent one
request to Marina's `/engine/model/v1/responses` route. The route returned
HTTP 429 before an upstream model call; Marina recorded
`model.budget_exhausted` with `token budget cannot admit model request`.
The conservative input-byte plus 4,096-output-token reservation exceeded the
task limit. The run blocked, recorded zero model calls, and produced no branch
or PR. The cancellation watcher sent no command because it observed no active
model reservation. This is a token admission check, not proof of a successful
native execution.

Changing to a fresh attempt-exclusive template changed the Marina execution
profile. A draft claimed before Deck's association was refreshed blocked with
the same profile-change guard. Deck's versioned request for the same disposable
repository advanced the binding, after which a new task could plan.

## Cancellation during a model call

Task `tsk_185ff92c2b79733ef227`, run
`run_97bbccac4458deaa08a8`, used the fresh native template revision
`420082ffa3b028dd7bffd22b09f108cf6070e65fe110fe81d6888ae5cba6d11e`
and binding version 41. Its approved plan was scoped to the same repository
and base revision, with a 200,000-token, USD 5, 15-minute budget.

Marina reserved 55,057 tokens and 7,145 microdollars for model call
`mdl_bcb46057c79569817f015001`. The watcher then submitted the exact run
cancel command `cmd_c9ec63ed2e3ee508d27a`. Deck marked the run and task
cancelled and acknowledged `Worker termination confirmed`. Marina retained
the model reservation with unknown final usage, recorded no used tokens, and
published one cancellation transition. The runtime cleanup completed; the
kagent instance was absent from its database afterwards. No source branch or
PR for this execution appeared on GitHub; the pre-existing KINT-06 PR #13
remained the latest PR. An ordinary engine-failure event was also published
before cancellation won the final Deck state. Deck's provider projection
reported a stale-authority conflict for later progress, which did not revive
the run.

After the test, the private installation's original repository engine
selection was restored while the kagent chat profile remained selected. Deck's
versioned repository API restored the original `jmlero/context-lens`
association at revision 18, binding version 43. Marina intake remained
disabled. No KINT-11 cutover was performed.

## Remaining gate

The [core matrix](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md#mandatory-failure-matrix)
still needs real repository checks for lost instance-create and send responses,
restarts of Marina, gateway, compute and engine database during work, tool-call
and waiting-input cancellation, time/cost and missing-usage cases, failed
verification/CI, and lost source/publication responses. The chat restart and
image rollback evidence are recorded separately in the
[installation follow-up](kint-09-10-permanent-2026-09-26.md); neither proves
those repository cases.
