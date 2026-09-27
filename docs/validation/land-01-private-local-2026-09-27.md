# LAND-01: private local Kind landing run

- Date: 2026-09-27
- Environment: `kind-marina-local` on `podman-machine-default-root`; Deck at
  `https://localhost:8443`; private disposable repository
  `jmlero/marina-acceptance`; existing Kubernetes Job repository runtime.
  Deck image manifest `sha256:2140c6d2216cd1ef0ce6ba0b32bef4300e952a9139bc017afb5c482ac7958f0f`
  and Marina image manifest
  `sha256:be61e2d7163ea9ed04af4559e3957da83bf210fddbb90f94bd5e4f92f899f942`.
- Result: one v2 repository task completed after clean verification, successful
  task-branch GitHub CI and an exact fast-forward of `main`. The repo remained
  private and **main was not protected**, so this is partial live evidence and
  does not satisfy the protected-CI acceptance requirement.

The acceptance workflow was changed in private-repository commit
`2d27cc3f410cc197e0e19902b9d7e1c8457269f2` to run its pinned
`acceptance-regression` job on `marina/**` pushes as well as main. The main
fixture's intentional clamp defect made CI fail at that base commit. No public
repository was created. Deck's project association was temporarily set to
`jmlero/marina-acceptance`; Marina published `repository/v2` with the fixed
`source.read`, `source.write`, `source.fast_forward` grant and intake was
restricted to the one test task.

The first task, `tsk_82a792d114fd85c29a05`, used a 50,000-token cap. After
plan approval its implementation worker's initial model request exceeded the
remaining reservation; Deck recorded `model.budget_exhausted`, blocked the run,
and no branch or commit was published. Intake was disabled before a fresh task.
This is an installed budget-denial observation on the existing repository
runtime, not KINT-09's required native kagent proof.

The second task, `tsk_402c5e5297262113e5f9`, used the installed profile's
200,000-token cap. Its reviewed plan pinned main at `2d27cc3`, allowed only
`src`, specified a separate clean verification Job and an exact-base
fast-forward, and prohibited force-push and deployment. After approval, the
worker published commit `bc34e2e87fa0da792b669ea206095ab8c1a8146c` on
`marina/exec_08a02e135fe9989166ac6d0c`. GitHub's
[`acceptance-regression` task-branch job](https://github.com/jmlero/marina-acceptance/actions/runs/36318207933/job/108616831292)
passed on that exact SHA. The earlier CI run on the branch's base SHA failed;
Marina did not mistake it for the head result. Marina then advanced main from
`2d27cc3` to `bc34e2e` without a force update. Deck run
`run_0a6c1d5f556100f1a610` completed with verification evidence bound to
`bc34e2e`; task status became Done. GitHub main and task branch both resolved
to the same SHA. The landed commit changed only `src/clamp.cjs`, had
`jmlero@hotmail.com` as author and committer, and all seven Node tests passed
in a fresh local checkout. Targeted intake was disabled after completion.

GitHub Free would not enforce branch protection on this private repository.
The operator chose to keep it private and defer that proof. This run therefore
shows Marina's behavior with observed CI, but cannot prove that an independent
GitHub rule would reject a main update lacking CI. It also uses the legacy
repository Job runtime and does not close KINT-09 or authorize KINT-11 cutover.
