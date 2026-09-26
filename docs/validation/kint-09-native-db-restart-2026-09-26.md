# KINT-09 native repository call during engine database restart

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate and native Codex.
- Result: the kagent PostgreSQL Deployment restarted during a real
  repository-work model call. The original engine task remained in the
  database and failed; the exact Deck run was cancelled and cleaned up with
  no source publication. This does not prove continuation through a database
  restart without operator cancellation.

The temporary native template `kint09-repo-6` at revision
`aad9567d27f0d395d57213cd73cb76be8df90e07a921f59f76f96c7143bacb37`
was selected with Deck binding version 58 for disposable
`jmlero/marina-acceptance`. Only task `tsk_f739ba197cebb11369d9` was
admitted; Marina intake was disabled again after claim. Run
`run_00f8171b583ab778adc9` had an approved plan limited to `src`, one
review PR, no merge or deployment, and a 15-minute, 200,000-token, USD 5
budget.

The original attempt reached model call `mdl_ee450ac1423d720105291ed0`
with a 55,103-token and 7,149-microdollar reservation. Restarting only
`kint-engine-postgresql` replaced Pod UID
`09026b69-f8cf-4032-8b11-c5d7067579d3` with
`aed4507f-e875-41a7-85f4-57f01313cba2`. The database Deployment became
Ready. During the restart the operator submitted exact-run cancel command
`cmd_07e4245329bd7d24a40f`, which Deck completed; the run and task were
cancelled.

The copied Marina journal passed SQLite `integrity_check`. It had one
cancelled execution `exec_358827ac141683a8a050b38e`, one failed runtime
attempt `rtm_127dc3bae17f4a8e4d30d2ee` with cleanup completed, no second
attempt, and zero known used tokens or cost. The model call had no final
usage response, so its reservation remained unresolved. After PostgreSQL
returned Ready, its retained `agent_instance_task` row held the **same**
original task `01a0de4c-abc5-75bb-b592-d1b674eb7dfd` in failed state.
Read-only GitHub inspection found no branch or PR for this execution. A later
Deck provider projection reported stale authority and did not revive the run.

Afterward, the saved private workflow restored the legacy repository engine
and `kint16-chat-11` profile. Deck restored `jmlero/context-lens` at revision
26, binding version 60. Marina and Deck were Ready with new task intake
disabled. The temporary native template, ModelConfig, Secret, exact
credential Role and RoleBinding were removed. The
[KINT-09 core matrix](../plans/kagent-integration.md#mandatory-failure-matrix)
still needs a database restart without simultaneous cancellation and the
other untested rows; KINT-11 remains gated.
