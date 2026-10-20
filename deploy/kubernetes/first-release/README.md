# First usable repository workflow

Marina runs bounded repository changes through native Codex on kagent, then a
separate clean verification Job. The selected delivery profile either opens a
PR (`repository/v1`) or waits for required CI and guarded main landing
(`repository/v2`). Current supported behavior and limits are in
[status](../../../docs/status.md). Engine success alone cannot complete a task.

The real-process suite checks workflow recovery and the Deck contract. Repeat
installation-specific native engine, model, Git/CI, network and failure acceptance
on the selected environment; deterministic runtime doubles do not establish it.

For the current pilot, [decision 0010](../../../docs/decisions/0010-initial-installation-services.md)
selects OpenRouter, GitHub/GHCR, and the existing Kubernetes-volume-backed SQLite
and artifact stores. Backups and restore drills are deferred for this pilot.
The generic configuration instructions below remain applicable to other installations.
Image builds and cluster setup may download required dependencies normally; the
running platform requires only GitHub services and OpenRouter externally. Package
worker/check dependencies before execution. Each installation must configure and
validate its exact model, repository, credentials, and budget; preparing those
values does not enable intake.

The [2026-09-21 local acceptance report](../../../docs/validation/poc-mvp-2026-09-21.md)
records the complete installation, network, live workflow, restart, cancellation,
failure, and budget results. The private disposable repository is
[jmlero/marina-acceptance](https://github.com/jmlero/marina-acceptance); its
[fixture source](acceptance-repository/) and pinned CI workflow are maintained
here. [NETWORK.md](NETWORK.md) enumerates the selected destinations, data flows,
DNS/IP enforcement limits and reproducible local probes.

Use the acceptance procedure below for a new installation and the
[operations runbook](OPERATIONS.md) for certificate renewal, credential rotation,
artifact retention, and failure recovery. `python3 scripts/check-installation.py
<private-installation>` reports configuration blockers without exposing Secret
data or changing intake. A passing preflight does not replace live acceptance.

## Boundaries

Deck and Marina have separate images, service accounts and SQLite PVCs.
Marina owns the platform API, its ledger and artifact bytes on its volume
([decision 0033](../../../docs/decisions/0033-marina-platform-api.md)). Deck is a
client: it presents its projected Kubernetes service account token, Marina checks
it with TokenReview and SubjectAccessReview, and `factory.yaml` binds Deck to
`marina-factory` in each project namespace. Marina calls no client. Deck downloads
registered evidence through Marina's API. Workers have disposable disks,
no Kubernetes API token, no Git write token and no upstream model credential.
They receive a credential restricted to one attempt. Marina checks the run's
live authority on gateway requests and repeatedly during long streams, and
stops calls when it is unavailable.

Both service connections and the worker gateway use TLS. Worker network policy
permits only Marina and cluster DNS. Kubernetes must have a CNI that enforces
NetworkPolicy; install the policies before admitting tasks. DNS itself can carry
data: configure cluster DNS to permit only required internal names for this
namespace if strict isolation is required. Node administrators remain trusted.
For hostile repositories use dedicated nodes and a supported sandbox RuntimeClass.

Kubernetes ownership alone does not keep source code private. The operator must
choose Git, model, registry, storage, backup and DNS locations. The model gateway
accepts self-contained text/tool requests to one explicit Responses-compatible
endpoint and disables remote provider tools and stored conversations. Compatibility
with your chosen model must be tested; there is no implicit hosted-model fallback.
For an installation with no external inference, use an internal endpoint and an
egress allowlist covering only the API server, internal Git/model and DNS. The
sample restricts worker egress; controller egress destinations must be restricted
in your site overlay because their addresses depend on your installation.

The first Git adapter supports GitHub's API and GitHub Enterprise Server. Use
`https://api.github.com` for GitHub, or your GHES API URL ending in `/api/v3`.
Other Git services need another adapter. Give the controller repository-scoped
contents and pull-request write access, and checks/statuses/reviews read access.
Branch protection remains authoritative; Marina never force-pushes or merges.

## Prepare locally

Requirements: Go 1.27+, Node 22, a container builder/registry, kubectl with
Kustomize support, Python 3, OpenSSL, and Kubernetes with persistent volumes,
NetworkPolicy support and cluster DNS. The sample uses two fixed namespaces:
`marina-system` and `marina-workers`, with one active task at a time.

Build independent Deck, Marina and verifier images. Install the pinned kagent
engine and native Codex/BYO Harnesses using the
[permanent local guide](../local-podman-kagent/README.md).

```sh
podman build -f deck/Dockerfile -t YOUR_REGISTRY/deck:YOUR_RELEASE deck
podman build -f marina/Dockerfile -t YOUR_REGISTRY/marina:YOUR_RELEASE marina
podman build --ignorefile marina/Dockerfile.verifier.dockerignore -f marina/Dockerfile.verifier -t YOUR_REGISTRY/marina-verifier:YOUR_RELEASE .
python3 scripts/prepare-installation.py /private/tmp/marina-install
```

Keep that generated directory private and outside Git. It contains credentials
and a local CA. The script creates separate TLS keys and 90-day certificates;
use your managed internal CA and certificate renewal for ongoing operation.
`operator-token` is the browser login credential. Populate `git-token` and
`model-token` without pasting secrets into task descriptions. For an internal
endpoint without authentication, set `model.token_file` to an empty string.
Use a CA bundle containing your other internal roots when Git/model use private
certificates; keep the installation CA in the same bundle.

Edit `workflow.json` and `factory.yaml` together. `project_id` is
`<namespace>.<project>` of the Marina Project in `factory.yaml`, its Workloads must
use the workflow's models and choices, and `deck-marina-projects.json` maps Deck's
project to them. In `workflow.json` set Git locations and identity, producer and different reviewer model locations/names/rates,
verifier image **digest**, exact native engine selection, trusted commands, allowed paths, required CI names and
budget. Set the independently built Deck and Marina images in the generated
`kustomization.yaml`. Push/load your images using your registry's normal process.
The generated workflow selects OpenRouter
[`openai/gpt-6-luna`](https://openrouter.ai/openai/gpt-6-luna) with its published
USD 0.10/M input and USD 0.50/M output token rates as the starting configuration.
Review current pricing and validate this model with the installed harnesses before
enabling intake; existing installations keep their explicit workflow setting
until updated separately.
The worker cannot download dependencies from the internet. Vendor dependencies
in the repository or extend the image with pre-fetched dependencies and tools.
The verifier image includes Node and Git. Extend it with prepackaged dependencies
for other trusted checks; commands must work without public egress.

The sample permits `src` and `tests`; change these to your actual bounded scope.
Git metadata, Codex configuration, AGENTS.md and GitHub workflow files cannot be
changed by this release. Checks are commands from installation configuration,
not commands supplied by the coding agent. A patch is limited to 30 regular
files and 2 MiB. Individual result artifacts are limited to 4 MiB.

`required_ci` contains the exact check/status names that must report success on
the published head. A missing check waits until the task deadline; it never
counts as success. An explicitly empty list means only the configured local
checks gate completion. Failed clean checks or required CI can enter the bounded
repair loop when the frozen task has enough runtime starts and model budget;
see the [profile contract](../../../marina/docs/workloads/profiles-v1.md).
Requested review changes block. Pause/resume and deployment are outside this
workflow. Cancel is supported; installed acceptance is tracked in [status](../../../docs/status.md).

Token admission reserves a conservative byte-based input estimate plus capped
output before forwarding a request. Unknown responses retain their reservations;
reported upstream usage is durable. This is a request admission budget, not a
guarantee about a provider's billing or tokenizer. For a cost cap, configure
nonzero rates in micro currency units per million tokens and use the same
currency on the task. A zero cost limit disables that cap; the token/time caps
still apply. Keep a provider-side spending limit where available.

## Review and install

Render locally first. The rendered file contains Secret data, so keep it private.

```sh
kubectl kustomize /private/tmp/marina-install > /private/tmp/marina-install/rendered.yaml
```

Review the image references, PVC/storage class, namespaces, RBAC, network policies,
TLS trust, configured destinations and all remaining `YOUR_` placeholders.
The two SQLite deployments use one replica and Recreate updates. Do not scale
them horizontally. When you choose to install on the selected cluster:

```sh
kubectl --context YOUR_CONTEXT apply -k /private/tmp/marina-install
# A fresh cluster rejects the Project and Workloads until the CRDs exist; apply again.
kubectl --context YOUR_CONTEXT apply -k /private/tmp/marina-install
kubectl --context YOUR_CONTEXT -n marina-system rollout status deployment/deck
kubectl --context YOUR_CONTEXT -n marina-system rollout status deployment/marina
kubectl --context YOUR_CONTEXT -n marina-system port-forward service/deck 8080:8080
```

Trust the generated CA locally, open `https://localhost:8080`, and sign in using
`operator-token`. For a permanent URL, configure your ingress with TLS and its
upstream trust for Deck; label its namespace `marina.dev/operator-ingress=true`.
No public ingress is created by the sample. Keep secure cookies enabled.

## Installation acceptance

1. Start with a disposable repository and a small known regression. Confirm the
   native harness and verifier dependencies and chosen model work through the gateway.
2. Discover the installed workload profile and invoke it with observable acceptance
   criteria and its required authority. Allow at least three runtime starts for
   implementation, independent review and clean verification; set realistic
   token, cost and time limits. Preserve the installed plan approval policy.
3. Approve the displayed plan. Inspect the worker's namespace, UID, read-only
   root, absent service-account token, resource limits and network restrictions.
   Confirm direct internet/API-server access from the worker is denied.
4. Restart Marina during execution. Confirm the same engine attempt is recovered and that
   only one logical source publication occurs. Restart again while waiting for CI.
5. Confirm verification uses a separate clean Job at the exact commit, required CI
   is green, and Deck links to the delivered commit/PR and digest-checked evidence.
6. With a three-start task budget, run a failing check and confirm the task blocks
   with its evidence. For repair acceptance, authorize seven or eleven starts,
   trigger a failed clean check and required CI separately, and confirm a saved
   diagnosis precedes each repair, with fresh review and verification at the new commit.
   Move the task branch after verification and confirm old evidence cannot
   complete the task. See the [local AUTO acceptance evidence](../../../docs/validation/auto-02-08-local-2026-10-01.md).
   Also reject a weakened test before commit, repair it through a recorded new
   approach, and retain the exact candidate, review verdict and original budget
   across restart. A malformed or unavailable reviewer must block publication.
7. Cancel a running task in Deck. Confirm model/source requests lose authority,
   the process exits, the engine instance/template/credential are cleaned up, and cancellation is
   acknowledged only after termination is observed.
8. Test provider outage and expired task budget. They must not permit continued
   model or publication authority. Inspect retained artifacts after Job cleanup.

Automated backups remain deferred. For a release rollback,
back up each SQLite database **with its WAL consistently**, and Marina's artifact
directory and identity/configuration separately. For this initial single-replica
release, stop new tasks, wait for active work to finish, stop the deployments and
take volume snapshots before restoring to an isolated environment. Deck's
logical export is useful but does not replace Marina's journal/artifact backup.
Verify schema compatibility and a restore on isolated test volumes before
starting an older binary. See the
[schema-aware local runbook](../local-podman-kagent/README.md#schema-aware-rollback).

Repository checks: `make check`, `make conformance`, `make build`. Passing these
does not replace the selected installation's network, model and recovery checks.
