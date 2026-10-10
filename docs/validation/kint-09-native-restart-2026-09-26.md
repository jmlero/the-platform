# KINT-09 native repository restart and cancellation

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate, native Codex.
- Result: Marina restarted while one real repository-work model call was
  reserved; cancellation of the exact Deck run then terminated and cleaned up
  the original attempt without source publication. This is a combined
  restart-and-cancel observation, not a standalone restart-resume pass.

The private workflow temporarily selected the fresh `kint09-repo-3` native
template at revision
`4379acecaf25f66cf2c0251c27fa7aa978295e721c28621ed8811bdc2c08a563`.
Deck explicitly bound the project to disposable `jmlero/marina-acceptance` at
binding version 51. Only task `tsk_4770c5122aff218a13b2` was admitted;
Marina task intake was disabled again after it was claimed. Its approved plan
for run `run_6c728c1dcc7cafb6e926` allowed edits under `src`, one review PR,
and no merge or deployment. The task budget was 15 minutes, 200,000 tokens,
and USD 5.

After the original runtime attempt entered a model call, Marina recorded call
`mdl_abff71d7eb91b617d2de8260` with a 55,044-token and 7,143-microdollar
reservation. The Marina Deployment was restarted: Pod UID
`a27b2f3d-eb6e-4722-9f2c-4208f454f2d1` was replaced by
`9d3fb7e9-9552-48e8-9369-0a6c34086506`. During that restart the operator
submitted exact-run cancel command `cmd_6c3171a0e43a353665c1`. The new
Marina Pod became Ready. Deck recorded the command completed and both the run
and task cancelled.

The copied Marina journal passed SQLite `integrity_check`. It contained one
execution `exec_219c43d4e86df1601a682316` in `cancelled` state and exactly
one runtime attempt `rtm_ec1560fa8b203835e7f5ca55`, also cancelled with
cleanup completed. There was no second attempt. The model call had no final
usage response; its reservation remained unresolved and known used tokens and
cost stayed zero. The original kagent instance was collected and deleted.
Read-only GitHub inspection found no branch or PR for this execution. No
completion was published after cancellation.

This adds live repository evidence for cancellation during a model call across
a Marina restart. The [KINT-09 core matrix](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md#mandatory-failure-matrix)
still needs separate restart-only observations, lost create/send and source
publication responses, gateway/compute/database restarts, tool/question
cancellation, time/cost and missing-usage limits, and failed verification/CI.
KINT-11 remains gated.
