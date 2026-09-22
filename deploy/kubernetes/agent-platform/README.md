# Two-workload agent-platform installation

This profile combines the existing Codex repository workflow and the bounded
LangChain diagnostic workload. It reuses the [local workflow overlay](../local-podman-workflow/kustomization.yaml),
separate Deck/Marina PVCs and identities, attempt credentials, TLS, RBAC and worker
network policy. No third service, store or deployment controller is needed.
The supported local environment is `kind-marina-local` on Podman's
`podman-machine-default-root` connection, with the Mac running and Deck at
`https://localhost:8443`.

Repository work uses one implementation and one clean verification attempt,
ending at a PR ready for human review. Analysis uses one isolated attempt and
an independent Go validator over the fixed synthetic diagnostic fixture.
`langchain-create-agent/v2` adds selected immutable context and the single
`read_context_source` gateway tool. It does not provide general semantic review,
an arbitrary tool catalog, source mutation, pause/resume, merge or deployment.
See the [profile contract](../../../marina/docs/workloads/profiles-v1.md).

## Build and pin

Run the repository checks before building:

```sh
make check
make conformance
make check-installation
make -C marina build
podman --connection podman-machine-default-root build -f deck/Dockerfile -t localhost/the-platform/deck:RELEASE deck
podman --connection podman-machine-default-root build -f marina/Dockerfile -t localhost/the-platform/marina:RELEASE marina
podman --connection podman-machine-default-root build -f marina/Dockerfile.worker --build-arg CODEX_VERSION=0.153.4 -t localhost/the-platform/marina-worker:RELEASE marina
podman --connection podman-machine-default-root build -f marina/Dockerfile.analysis-worker -t localhost/the-platform/marina-analysis:RELEASE marina
```

Choose a release tag and replace `RELEASE`. Each image is independent. Both
workers package their Go worker and all execution dependencies at build time.
Analysis pins Python libraries and verifies package hashes in
[requirements.txt](../../../marina/harnesses/langchain/requirements.txt).
The coding fixture uses the image's Node interpreter and built-in test runner.
No package installation is allowed from a running worker.

For each image, save an OCI archive, copy it to the existing Kind node and import
it into containerd. Use the same explicit Podman connection for every command:

```sh
podman --connection podman-machine-default-root save --format oci-archive -o /private/tmp/marina-analysis.tar localhost/the-platform/marina-analysis:RELEASE
podman --connection podman-machine-default-root cp /private/tmp/marina-analysis.tar marina-local-control-plane:/tmp/marina-analysis.tar
podman --connection podman-machine-default-root exec marina-local-control-plane ctr -n k8s.io images import /tmp/marina-analysis.tar
podman --connection podman-machine-default-root exec marina-local-control-plane ctr -n k8s.io images ls
podman --connection podman-machine-default-root exec marina-local-control-plane ctr -n k8s.io images tag localhost/the-platform/marina-analysis:RELEASE localhost/the-platform/marina-analysis@sha256:IMPORTED_DIGEST
```

Repeat for Deck, Marina and the coding worker. Pin the **imported containerd
manifest digest**, which can differ from the Podman image/config digest. Retain
the previous images until upgrade acceptance is complete.

## Prepare or upgrade

For a fresh installation, first use the [local installation guide](../local-podman/README.md)
and [repository configuration](../first-release/README.md). Configure the selected
model, nonzero admission price rates, private credential references, repository,
offline checks and required CI. Switch to the local workflow overlay with intake
disabled. An existing installation must reuse its private directory, CA,
credentials, service identity, `appdeck-data` override where present, Marina PVC,
and site-specific egress/host-alias configuration.

Before an upgrade, disable new intake, allow already journaled work to settle,
and verify that no active worker remains. Disabling intake alone does not stop
active work. Changing a pinned profile during execution blocks that execution;
drain first to preserve its progress. Keep old Ready tasks pinned: they must
block if their selected profile is unavailable, never silently adopt the upgrade.

Set the new Deck and Marina digest references in the existing private
`kustomization.yaml`; set the coding image in `workflow.json`'s `kubernetes.image`.
Then preview the analysis upgrade (paths below use the existing local install):

```sh
python3 scripts/prepare-agent-installation.py ~/.local/share/the-platform/marina-local/installation --analysis-image localhost/the-platform/marina-analysis@sha256:IMPORTED_DIGEST
```

This invokes the current Go configuration validator without reading credentials
or contacting any provider. It retains existing fixture, limits, Git/model
settings and approval policy, upgrades the analysis harness/tools, increments
its revision when changed, and adds required capabilities to the existing
project-scoped service identity. Repeating the same input is idempotent.

Save the reviewed configuration with the same command plus `--apply`. Private
originals are retained under `agent-upgrade-<digest>/` in the installation
directory. No credential is rotated, database touched, intake enabled or resource
deployed. Use `--identity` for a differently named existing service identity and
`--marina-binary` for an alternate current validator binary.

```sh
python3 scripts/check-installation.py ~/.local/share/the-platform/marina-local/installation
kubectl kustomize ~/.local/share/the-platform/marina-local/installation > /private/tmp/agent-installation-rendered.yaml
```

Keep the render private: it contains Secrets. Verify digest pins, unchanged PVCs,
single replicas with Recreate updates, scoped identities and egress, and effective
Marina arguments `[serve, --intake-enabled=false]`. Apply only to the authorized
local test environment:

```sh
kubectl --context kind-marina-local apply -k ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local -n marina-system rollout restart deployment/marina
kubectl --context kind-marina-local -n marina-system rollout status deployment/deck --timeout=60s
kubectl --context kind-marina-local -n marina-system rollout status deployment/marina --timeout=60s
```

Deck applies additive migrations in its own store; Marina retains its separate
journal. Restart Marina even when only `workflow.json` changed: its stable-name
ConfigMap does not trigger a rollout or reload configuration. Verify historical tasks, evidence downloads, stable Marina identity and
both bound PVC identities after the upgrade. Binary rollback across a new schema
is not established by this procedure; restore the previous configuration only
with a compatible binary. Never remove or reinitialize PVCs to repair an upgrade.

After changing execution-profile settings, refresh the repository association in
Deck before invoking new coding work. Submit the same installed repository and
branch with the current request/binding versions. If Marina reports that the
binding changed during upgrade, refresh and resubmit with the returned version.
Wait for the association's `connected` status before discovery/invocation. This explicit
version check preserves old task inputs; a stale selection must block. Historical
completed work and its evidence remain accessible.

## Invoke and inspect

Use the [operator API client](../../../marina/docs/workloads/api-v1.md) to discover
both profiles, retain the exact invocation JSON/key, submit it, replay it and
inspect the same task. A catalog expires without Marina refresh; ensure
`workload.publish` is granted to the configured service identity. Invocation
does not enable intake. For controlled acceptance, temporarily select only the
new task with `[serve, --intake-task-id, TASK_ID]`, then restore disabled intake
as soon as it is claimed. Preserve the installed plan-approval policy.

Inspect task context, workload/harness identity, progress, commands, confirmed
usage, unresolved reservations and downloaded evidence. Analysis validation must
bind fixture/profile/output/context digests. Repository evidence must bind the
separate verifier and required CI to the current PR head. Never treat a model
report, submitted cancel command, or pending usage reservation as completion.

## Isolation, recovery and acceptance

Workers have no Kubernetes token, upstream model token or Git write credential.
Only the attempt gateway and cluster DNS are reachable under the installed
network policies; node administrators and DNS remain trusted boundaries. This
is Pod/network isolation, not a VM sandbox. The tool accepts only an exact URI
from the frozen package and checks live authority, allowlist, expiry and explicit
revocation outside LangChain. Source revisions do not replace active inputs.

For outages, keep the original task/run/attempt IDs and inspect retained state.
Restart each control service separately; inspect the existing Job before any
new start. Cancellation acknowledges only observed worker termination. Missing
or invalid evidence, expired authority and exhausted budgets must never yield
success. Unknown model usage retains its reservation. A blocked or cancelled
run requires newly authorized work; do not edit either database.

Use the existing [operations runbook](../first-release/OPERATIONS.md) for
certificate/credential rotation, diagnostics and stuck workers. The offline
`marina diagnostics` export correlates execution, attempt, model/tool and artifact
records without exposing credentials or source text. Artifact retention remains
an explicit maintenance operation, and pilot backup/restore drills remain deferred.

The [AGENT-11 through AGENT-13 acceptance](../../../docs/validation/agent-11-13-2026-09-22.md)
records passing `make check`, `make conformance`, `make check-installation`, live
API-invoked Jobs for both profiles, and failure/recovery evidence. The date-scoped
[acceptance helper](../../../scripts/validate-agent-poc.py) accepts `--mvp` for the
2026-09-22 proof, with explicit image loading, installation, source registration,
task invocation, restart/cancel/budget exercises and result capture. It is a
bounded local test utility, not the installation interface. Restore intake to
disabled, deactivate only test-created context sources, and verify test runtime
cleanup at the end.
