# Native agent and verifier installation

Use the [local Podman/Kind guide](../local-podman/README.md) and
[permanent engine guide](../local-podman-kagent/README.md). This configuration
selects native Codex for repository work, a text-only independent reviewer,
a separate clean verifier, and optional conversation using the same BYO image.
The retired diagnostic profile and custom
Codex/Python worker images are removed. Current acceptance belongs in
[status](../../../docs/status.md).

## Build and pin

```sh
make check
make conformance
make -C marina build
podman --connection podman-machine-default-root build -f deck/Dockerfile -t localhost/the-platform/deck:RELEASE deck
podman --connection podman-machine-default-root build -f marina/Dockerfile -t localhost/the-platform/marina:RELEASE marina
podman --connection podman-machine-default-root build --ignorefile marina/Dockerfile.verifier.dockerignore -f marina/Dockerfile.verifier -t localhost/the-platform/marina-verifier:RELEASE .
podman --connection podman-machine-default-root build --ignorefile marina/Dockerfile.verifier.dockerignore -f marina/Dockerfile.verifier --target native-codex -t localhost/the-platform/marina-codex:RELEASE .
podman --connection podman-machine-default-root build -f marina/Dockerfile.conversation-byo -t localhost/the-platform/marina-conversation-byo:RELEASE marina
```

Replace `RELEASE`. Import Deck, Marina and verifier OCI archives into the existing
Kind node and pin their **containerd manifest digests**. A Podman image ID or an
imported archive index is not that digest. Keep prior releases for rollback.
Serve the pinned native Codex and BYO images through the local registry as the
permanent engine guide describes. The verifier and `native-codex` target share
the same project toolchain: pinned Node.js 24 and Go 1.27.1, frozen Go module and
Deck npm caches, and Chromium matching the locked Playwright version. The native
target retains the pinned upstream kagent adapter and Codex CLI, entry point and
unprivileged identity. It still runs through kagent's isolated runtime.

All dependency and browser downloads happen during the image build. Runtime
defaults disable Go/npm downloads and Playwright browser installation. In a fresh
checkout run `npm --prefix deck/web ci --offline` before the frontend checks; the
npm cache and browser binaries are readable by the non-root user with a read-only
root filesystem. Changing a lockfile requires rebuilding both images. For another
project, package its frozen test dependencies in its own image and give its native
Harness the same toolchain; this image's caches are for The Platform.

Before installation, run the selected unit and browser checks in each image with
`podman --connection podman-machine-default-root run --network=none`, a writable
test-owned `/work` and `/tmp`, `--read-only --user 65532:65532`, and
`-e GOCACHE=/tmp/go-cache` (or a writable test-owned `/data`). Verify
`node --version`, `go version`, `git --version`, and `codex --version` in the native
image. The verifier has no Codex executable. Chromium must launch and the checks
must pass from the frozen caches; a missing dependency fails instead of downloading.

## Fresh installation or data-preserving upgrade

Prepare a new private directory once with `scripts/prepare-installation.py`.
Existing installations reuse their directory, CA, credentials,
Deck/Marina PVCs and site egress configuration. Use the permanent local engine
overlay and the version-1 [workflow example](../first-release/workflow.example.json).
Configure the repository, producer and different reviewer models, explicit
credential references, offline checks, allowed paths, required CI, approval
policy and bounded model rates.
While work is drained, update the installed `kint-codex` Harness's
`spec.workload.image` to the registry digest of the newly built `native-codex`
target. Keep the existing `codex` configuration, worker pool and snapshot policy.
Let the engine prepare that image before admitting a task, close any temporary
setup egress, and update the Marina Project and Workloads for the new profile.
`reviewer_model` has the same fields as `model`; choose its own model ID and
actual price rates in the same currency. Update the installed
`kint-conversation-byo` Harness to the newly built BYO image digest, even when
conversation is disabled. Older BYO images do not implement repository review.
For configured conversation, use matching Deck/Marina/BYO releases that implement
the frozen repository and selected-context protocol from
[decision 0027](../../../docs/decisions/0027-read-only-repository-context-for-chat.md).
Conversation receives only the fixed bounded read tools described in the
[profile contract](../../../marina/docs/workloads/profiles-v1.md#conversation-selection).
The [profile contract](../../../marina/docs/workloads/profiles-v1.md) owns the
runtime-start budgets and complete-input boundary. Existing tasks keep their
frozen budget; the upgrade does not add review starts to it.

Before upgrading, disable new intake and drain active work. Save private
configuration, exact release image references and complete consistent stores,
including SQLite WALs and Marina artifacts. Preserve the engine's PostgreSQL and
snapshot storage. Never reinitialize a PVC to repair an upgrade.

Before enabling conversation, map a conversation Workload for the project in
`deck-marina-projects.json`; `scripts/check-installation.py` rejects a configured
conversation profile without one. An older invocation without a context snapshot
returns missing context rather than using today's repository or sources.

Preview the configuration removal with the current Marina validator:

```sh
python3 scripts/prepare-kagent-installation.py ~/.local/share/the-platform/marina-local/installation --verifier-image localhost/the-platform/marina-verifier@sha256:IMPORTED_DIGEST --marina-binary marina/bin/marina
```

The helper requires the already selected native repository engine. It removes
retired diagnostic settings, replaces the verifier image and preserves repository,
model, budgets, approvals and credentials. It keeps a private
backup when `--apply` is supplied. It does not touch a database, deploy resources,
rotate a credential or enable intake. Replace `IMPORTED_DIGEST` before use.

Set current Deck/Marina image pins in the private Kustomization, apply the reviewed
configuration with `--apply`, then check and render it privately:

```sh
python3 scripts/check-installation.py ~/.local/share/the-platform/marina-local/installation
kubectl kustomize ~/.local/share/the-platform/marina-local/installation > /private/tmp/kint-installation-rendered.yaml
kubectl --context kind-marina-local apply -k ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local -n marina-system rollout restart deployment/marina
kubectl --context kind-marina-local -n marina-system rollout status deployment/deck --timeout=60s
kubectl --context kind-marina-local -n marina-system rollout status deployment/marina --timeout=60s
```

The render contains Secrets. A stable-name workflow ConfigMap does not trigger a
restart, so restart Marina after a workflow-only change. Verify historical tasks,
conversation IDs, model reservations and digest-checked artifacts. Both services
reject applied migrations unknown to their binary. An older release must use a
compatible retained snapshot; follow the permanent guide's schema rollback gate.
Update the Marina Project and Workloads after changing profile
settings; Deck picks them up within 30 seconds. Stale tasks retain their original inputs and must block rather than
silently adopting a new profile.

## Acceptance and operation

Discover the installed profile through the [operator API](../../../marina/docs/workloads/api-v1.md),
retain its exact ID/digest and invocation JSON/key, and authorize only the selected
budget and scope. Temporarily use `serve --intake-task-id <id>`, then restore
`serve --intake-enabled=false` after claim. Preserve the plan approval policy.
Explicit chat invocations are independent of repository intake.

Repeat live native repository work, a conversation reply and the affected failure
cases after a change. Exact-patch review is required before every commit;
independent verifier checks and exact-commit CI are required before guarded main
landing. Missing artifacts, lost authority or
unknown model usage cannot count as success. Engine cancellation must remain
available during a Deck outage; cleanup failures remain visible and retryable.
Inspect the same recorded instance before recovery instead of resending work.
Use the [operations runbook](../first-release/OPERATIONS.md) for certificates,
credentials, diagnostics and artifact retention. Backup automation remains deferred.

### PRD publication preparation

PRD-01 requires matching Deck, Marina and conversation BYO images. Deck migration
`021_prd_drafts.sql` adds durable proposal/approval fields; retain the compatible
pre-upgrade stores for rollback. The provider advertises `conversation.prd.draft`
and Marina requires it when conversation is configured. Draft publication uses
the conversation Turn's outcome; only the operator can approve.

Before enabling PRD publication, explicitly include `docs/prd` in the repository's
`allowed_paths`, retain `delivery: fast_forward`, review the configured checks,
prepare the image snapshots, and refresh the repository/profile association.
These changes do not authorize model calls, Git writes or task intake on their
own. An operator approves the exact draft and a bounded publication budget in its
thread. Use the ordinary selected-task acceptance procedure and verify the exact
approved bytes at the landed commit. See [current acceptance](../../../docs/status.md).

### Ordered PRD tasks and project profiles

The matching follow-up release applies Deck migration `022_prd_tasks.sql` and
advertises `conversation.prd.tasks`. Upgrade Deck, Marina and BYO together; retain
the compatible stores and the previous release pins before replacing images.
The conversation gateway's strict response schema must include both `prd` and
`prd_tasks`. Approving a task proposal grants a bounded budget for each task;
it does not expand the installed repository file scope or concurrency.

For more projects, the private workflow file can contain
`{"version":1,"projects":[...complete workflow profiles...]}`. Configure each
project's Git/model credential references, repository, checks, scope, required CI
and packaged toolchains explicitly. Mount each referenced credential file and add
that project to the existing Marina service policy's appropriate grants. Shared
service TLS, signing and artifact settings must agree across profiles. Run the
current `marina check-config` and installation preflight before applying. The
legacy upgrade helper accepts one profile; prepare a multi-profile upgrade by
editing and validating that complete private document. Keep intake disabled until
the selected project's bounded acceptance case is authorized. One repository task
runs across the whole installation. See [decision 0031](../../../docs/decisions/0031-explicit-per-project-profiles.md).
