# Context Lens pilot preparation

The operator selected Context Lens on 2026-09-22 and confirmed ubahndoku is
completed. See the [assessment](https://github.com/jmlero/the-platform/blob/744204a/docs/pilot-project-assessment-2026-09-22.md).
This directory prepares the first product task, not a new platform capability.

## Bounded outcome

CLP-01 introduces a pure versioned serializer/validator for privacy-safe session
summaries. CLP-02 will add explicit snapshot export; CLP-03 will compare two
reports. Each subsequent task depends on the previous reviewed change being
available in the repository base. Keep one implementation and one independent
verification attempt, with human review/merge between tasks.

The proposed first-task ceiling is 15 minutes, 100,000 admitted tokens and
USD 0.15 at the installation's configured rates. The draft [task](task.json)
remains in backlog with no execution authority. No platform task may be released
until the profile, exact repository/context, offline checks and authority are
ready. Existing unrelated ready tasks must not be admitted accidentally.

## Files and trust

- [Dockerfile](Dockerfile) extends the existing pinned local worker and installs
  the lockfile dependencies at image build time. It copies an exact Git archive
  into the image as the baseline.
- [check.sh](check.sh) checks manifest identity, copies packaged dependencies into
  the disposable work directory, and runs the existing baseline or independent
  report acceptance. No runtime package downloads are allowed.
- [trusted/report.test.mts](trusted/report.test.mts) is external acceptance for
  candidate `shared/report.ts`. It must fail on the baseline's missing module.
  The worker may inspect it but cannot modify the image's trusted test.
- [ci-proposal.yml](ci-proposal.yml) is a proposed target-repository CI workflow.
  It is not installed or running. This release deliberately forbids agent changes
  to `.github/workflows`, so it requires a separate maintainer bootstrap change.

The worker may change only `shared/report.ts` and `server/report.test.ts`.
Existing source, checks, manifests, instructions and hosting configuration remain
outside that task's patch scope. The app stays local-only; do not publish the
local log reader as a hosted website because a Sites metadata file exists.

## Build and preflight

Baseline: `jmlero/context-lens` at
`5c7868925d255cb49d6b4b6e457adac0249975cf`.

Prepare an empty build directory with that Git archive under `baseline/`, then
copy this Dockerfile, check script and `trusted/` into it. Build with the existing
`podman-machine-default-root` connection. Preserve unrelated images and services.
Test the immutable image with read-only root, no network, a writable `/work`
and user 65532, copying the baseline into `/work/repo`. The baseline check must
pass; the new contract must fail specifically because `shared/report.ts` is
absent, not because of missing dependencies or test infrastructure.

Before installing: confirm no active worker/execution; preserve the current
private configuration; import the image into the dedicated Kind node and pin its
containerd manifest digest. Use the selected Git credential, never a developer
login. Inspect repository access, explicit task sources and revisioned binding.
Do not delete prior fixture history or run the unrelated ready `test` task.

After the maintainer CI bootstrap is accepted, configure trusted checks as:

```json
[
  {"id":"context-lens-baseline","directory":".","command":["/opt/context-lens/check","baseline"]},
  {"id":"context-lens-report","directory":".","command":["/opt/context-lens/check","report"]}
]
```

Require the observed exact GitHub CI check name; do not infer it from the workflow
display title. Retain installed plan approval. Authorize only the selected task
with source-read, source-write and PR-create authority, then restrict intake to
its task ID. Restore disabled intake after a terminal result. Separate services,
stores, leases, fencing, gateway access and evidence rules remain unchanged.

A passing preparation check is not a platform-produced feature. Record the actual
Deck task/run, context digest, image digest, independent verification, PR/CI head,
usage and terminal outcome before reporting a value-pilot result.
