# Context Lens pilot preparation — 2026-09-22

## Scope and decision

The operator selected Context Lens as the value pilot and confirmed ubahndoku
is completed. The [assessment](https://github.com/jmlero/the-platform/blob/744204a/docs/pilot-project-assessment-2026-09-22.md) records
that correction. [Pilot assets](../../deploy/pilots/context-lens/README.md) define
a three-task report/export/comparison increment.

This report proves preparation only. No model ran, no product implementation was
produced by Marina, and no Context Lens commit, push, PR, or deployment occurred.

## Observed baseline

- Repository: `jmlero/context-lens`.
- Inspected/tested commit: `5c7868925d255cb49d6b4b6e457adac0249975cf`.
- Local tracked checkout was clean and matched the GitHub branch.
- Host baseline: 13 tests, TypeScript check, lint and build passed using existing
  installed dependencies and a temporary Git archive.
- The actual installed Marina Git credential could read the repository and its
  branch; repository metadata reported push permission. No credential was copied
  into task content, source, evidence or an image.
- Deck and Marina were healthy in `kind-marina-local`; no worker Job/Pod was active.
  Marina was configured with `serve --intake-enabled=false`.
- Existing repository binding remained `jmlero/marina-acceptance`, version 14.
  An unrelated ready task named `test` was observed and left untouched.

## Offline worker proof

Built `localhost/the-platform/marina-context-lens:pilot-20260922` on the existing
`podman-machine-default-root` connection. `npm ci` installed 493 packages from
the selected repository lockfile during image setup only.

| Identity | Value |
| --- | --- |
| Base worker Podman manifest | `sha256:debcd4ed9692b5a960b9b48fa7fb9c11672c75e9ec8f57460c99eb441070d2bd` |
| Pilot Podman manifest | `sha256:367427ecb0e968a205af0c7e5bb56404625f1e21393dc22bff92024d6edf8eb5` |
| Pilot image ID | `cffee44468b2de54bc77616dd612788181147d5d515594d0cd0f6a9f3cd8fc49` |
| Runtime Node version | `v22.23.2` |

A disposable container ran with `--network none`, read-only root, user/group
65532, dropped capabilities, no-new-privileges, two CPUs, 4 GiB memory and a
256-process limit. A new anonymous `/work` volume supplied writable workspace
and was removed with the container. The remote Podman tmpfs option did not apply
the requested writable permissions, so the proof used this disposable volume;
no existing volume was reused or removed.

`/opt/context-lens/check baseline` passed all 13 tests, typecheck, lint and build
without external network access. Vite's existing greater-than-500-kB chunk warning
remained nonfatal. This proves the application dependencies can be packaged for
the isolated runtime, not that the image has been imported into Kind or used by
a real Marina attempt.

`/opt/context-lens/check report` failed with `ERR_MODULE_NOT_FOUND` for candidate
`/work/repo/shared/report.ts`, the exact expected missing feature. The harness
explicitly required that nonzero result and module identity and printed
`EXPECTED_RED_REPORT_CONTRACT`. The trusted acceptance file loads from the
candidate checkout; it does not test a hidden implementation baked into the image.

## Deck draft and remaining gates

Created one draft through Deck's operator HTTP API:

- Task: `tsk_c723190cc08ce495d5d1`.
- Title: `Context Lens CLP-01: versioned privacy-safe session reports`.
- State: `backlog`.
- Execution authority: empty.
- Proposed ceilings: 15 minutes, two attempts, 100,000 admitted tokens,
  USD 0.15 at configured rates, concurrency one.
- Scope: only `shared/report.ts` and `server/report.test.ts`.

Task creation did not enable intake or authorize model/source operations.
The exact draft lives in [task.json](../../deploy/pilots/context-lens/task.json).
It states the intended repository explicitly; do not release it while the
installation is still bound to the disposable fixture.

Remaining prerequisites for the real run:

1. Publish and accept the small maintainer CI bootstrap change prepared in
   [ci-proposal.yml](../../deploy/pilots/context-lens/ci-proposal.yml). Marina's
   current workflow cannot edit `.github/workflows`, by design. Confirm the exact
   successful check name at the bootstrap head.
2. Import/pin the worker's containerd manifest, preserve the prior installation,
   update checks/allowed paths and the versioned Context Lens repository binding,
   and register exact task sources without inheriting acceptance-fixture guidance.
3. Grant the chosen task its source/PR authority and bounded budget, approve its
   concrete execution plan, and admit only that task ID.
4. Inspect the separate implementation and verification attempts, PR/CI head,
   evidence and usage. Restore disabled intake after completion or a blocker.

No acceptance item is marked complete merely because the draft or worker image
exists. Product value and the remaining report/export/comparison tasks are still
unproven.
