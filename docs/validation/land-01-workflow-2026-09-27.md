# LAND-01: versioned landing workflow checks

- Date: 2026-09-27
- Environment: local product tests and real Deck processes with deterministic
  Git/runtime doubles; no local Kind landing task.
- Result: Marina can select `repository/v2` with a fixed
  `source.fast_forward` grant, wait for required CI on the exact verified task
  commit, and make one guarded fast-forward update of main. The old
  `repository/v1` pull-request path still passes. LAND-01 remains at Verify.

The new optional `delivery: fast_forward` configuration requires a repository
binding and at least one required CI check. Deck accepts the v2 catalog and
Ready delegation only with the matching profile, digest, bounded budget and
authority. Marina carries the profile through worker launch, checks that the
task branch and base have not moved, observes CI on the exact head, then calls
the existing non-forced GitHub fast-forward port. It records source-bound
evidence and completes the run only after landing. A lost response after the
fake source applied the update was reconciled across a controller/journal
restart without a second main update.

`make check` passed the CLI, Deck backend/frontend, Marina and installation
checks. `make conformance` passed real Deck/CLI/Marina process tests, including
v1 PR completion, v2 invocation and completion, moved-base refusal, failed or
missing required CI, and failed clean verification. The source-control
connection-drop test in the earlier [foundation report](land-01-foundation-2026-09-26.md)
also passed in the Marina suite. Deterministic Git/runtime doubles do not prove
GitHub branch protection, live model execution or a local Kind task.

The acceptance repository `jmlero/marina-acceptance` remains private. GitHub
Free rejected branch-protection enforcement for its private main branch. The
operator chose to keep it private and defer the protected-CI proof. No public
repository was created. The required live Kind task, with branch CI enforced
before main can advance, has therefore not been accepted.
