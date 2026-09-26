# KINT-09 native repository call during worker-compute restart

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`, permanent pinned kagent/Substrate and native Codex.
- Result: the pinned kagent worker Deployment restarted during a real
  repository-work model call. The exact Deck run was cancelled and the
  original attempt was cleaned up without source publication. This is a
  combined restart-and-cancel observation, not a standalone resume pass.

A fresh `kint09-repo-7` template at revision
`a2e0ef9130a25a5f024509ff3b53d9a2d8c3636246d0b65d18e1d0b48359187c`
was selected temporarily with Deck binding version 61 for disposable
`jmlero/marina-acceptance`. Only task `tsk_56c3abe25c41ae0ff53c` was
admitted; Marina intake was disabled after claim. Run
`run_71e248ff4212ba3905b3` had an approved plan limited to `src`, one
review PR, no merge or deployment, and a 15-minute, 200,000-token, USD 5
budget.

The original attempt reached model call `mdl_eaf4f0819885b28bd0ce9dad`
with a 55,094-token and 7,148-microdollar reservation. Restarting only the
`kint-worker` Deployment replaced Pod UID
`a38e57b1-637a-4bb8-aaf1-fcb9f7a9b710` with
`7ea32057-11b0-4165-9f99-6077824f1ed7`. The old Pod was absent and the
new Pod Ready on the later check. During the restart the operator submitted
exact-run cancel command `cmd_64d92bdd0bb9359ae835`, which Deck completed;
the run and task were cancelled.

The copied Marina journal passed SQLite `integrity_check`. It held one
cancelled execution `exec_36a96b5f0a476303fbc147a5`, one cancelled runtime
attempt `rtm_a7d58e95ddce3f68829466a3` with cleanup completed, and no
second attempt. Known used tokens and cost stayed zero while the interrupted
model call retained an unresolved reservation and unknown final usage. The
engine record retained its original instance and task IDs in cancelled state.
The kagent database still showed the original task
`01a0de5b-5a38-79e2-920d-0fcc3bf2e793` as submitted; deletion of the
instance, rather than that task row alone, supplied the termination evidence.
Read-only GitHub inspection found no branch or PR for this execution.

Afterward the saved private workflow restored the legacy repository engine
and `kint16-chat-11` profile. Deck restored `jmlero/context-lens` at revision
28, binding version 63. Marina and Deck were Ready with new task intake
disabled. The temporary native template, ModelConfig, Secret, exact
credential Role and RoleBinding were removed. The
[KINT-09 core matrix](../plans/kagent-integration.md#mandatory-failure-matrix)
still needs a worker restart without simultaneous cancellation and the other
untested cases; KINT-11 remains gated.
