# KINT-09 native repository call during a kagent controller restart

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate and native Codex.
- Result: the kagent controller restarted during a real repository-work model
  call. The original attempt failed, the exact run was cancelled and cleaned
  up, and no source publication occurred. This does not prove transparent
  continuation after a controller restart.

A fresh native template `kint09-repo-4` at revision
`625d4e3739e4856b14e1a79c698d32414ce00d3e7b19f97e4763377918e69fd6`
was selected temporarily. Deck associated the project with disposable
`jmlero/marina-acceptance` at binding version 52. Only task
`tsk_01c9ee5b45002ba1a7cd` was admitted; Marina intake was disabled after
claim. The approved plan for run `run_b55574e3c55402133ab0` limited edits to
`src`, allowed one review PR, and prohibited merge or deployment. Its budget
was 15 minutes, 200,000 tokens and USD 5.

The original attempt entered model call `mdl_bb057e824994c99c4648b7fd`
with a 55,070-token and 7,146-microdollar reservation. The controller
Deployment restart replaced Pod UID
`559477d1-29c8-4533-84cb-7aeb6901392e` with
`833bace8-0c36-400b-b7e0-6654e62a5049`. The old Pod was still terminating
when the new one became Ready, and was absent on the later check. During the
restart the operator submitted exact-run cancel command
`cmd_1f74ef9daaedde9bb174`. Deck recorded it completed and marked both task
and run cancelled.

The copied Marina journal passed SQLite `integrity_check`. It contained one
execution `exec_de0863edeed7760cf7d6c3ee`, cancelled with zero known used
tokens and cost, and exactly one runtime attempt
`rtm_ed95348903fa20ceba0e9262`, failed with cleanup completed. The model
call had no final usage response; its reservation remained unresolved. The
engine record retained the original instance and task identities in failed
state. The controller returned Ready, and read-only GitHub branch and PR lists
showed no publication for this execution. Deck's provider projection reported
a later stale-authority conflict; it did not revive or complete the run.

This is a safe failure and cancellation observation for the
[KINT-09 core matrix](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md#mandatory-failure-matrix).
It does not close the restart-only, gateway-resume, or other untested rows.
KINT-11 remains gated.

After both bounded native tests, the saved private workflow restored the
legacy repository engine and selected `kint16-chat-11` profile. Deck's
versioned repository API restored `jmlero/context-lens` at revision 22,
binding version 54. Marina and Deck were Ready with new task intake disabled.
The four temporary native templates, ModelConfigs, Secrets, and the exact
KINT-09 credential Role and RoleBinding were deleted. The permanent chat
resources and the historical Deck/Marina records were retained.
