# First usable repository workflow

This release implements a bounded repository fix: explicit task authorization →
immutable plan and optional approval → isolated coding Job → scoped branch and
commit → independent verification Job at that commit → one PR → required CI and
review checks → evidence-backed completion in Deck. Completion means ready
for human review. It does not mean merged, deployed, or production-approved.

The real-process suite restarts Marina's journal between workflow steps and
checks completion, lost commit-response recovery, failed verification, changed
heads, missing CI and missing authority. A selected cluster, worker image,
repository and model still require the installation acceptance procedure below.

For the current pilot, [decision 0010](../../../docs/decisions/0010-initial-installation-services.md)
selects OpenRouter, GitHub/GHCR, and the existing Kubernetes-volume-backed SQLite
and artifact stores. Backups and restore drills are deferred for this pilot.
The generic configuration instructions below remain applicable to other installations.
Image builds and cluster setup may download required dependencies normally; the
running platform requires only GitHub services and OpenRouter externally. Package
worker/check dependencies before execution. The exact OpenRouter model, repository,
and credentials still need configuration; these choices do not enable intake.

The [2026-09-17 local acceptance report](../../../docs/validation/run-01-18-2026-09-17.md)
records completed installation/image/network/restart checks and the remaining
live-run prerequisites. The private disposable repository is
[jmlero/marina-acceptance](https://github.com/jmlero/marina-acceptance); its
[fixture source](acceptance-repository/) and pinned CI workflow are maintained
here. [NETWORK.md](NETWORK.md) enumerates the selected destinations, data flows,
DNS/IP enforcement limits and reproducible local probes.

## Boundaries

Deck and Marina have separate images, service accounts and SQLite PVCs.
Marina owns artifact bytes on its volume; Deck downloads registered evidence
through an authenticated, digest-checking proxy. Workers have disposable disks,
no Kubernetes API token, no Git write token and no upstream model credential.
They receive a credential restricted to one attempt. Marina checks the live
Deck lease/fence on gateway requests and repeatedly during long streams.
Worker processes stop when the authority watchdog loses contact.

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

Requirements: Go 1.26, Node 22, a container builder/registry, kubectl with
Kustomize support, Python 3, OpenSSL, and Kubernetes with persistent volumes,
NetworkPolicy support and cluster DNS. The sample uses two fixed namespaces:
`marina-system` and `marina-workers`, with one active task at a time.

Build independent images. Choose and pin a tested Codex CLI version; it is the
coding harness, with the configured endpoint supplying inference.
The command explicitly disables web search and image attachments for the text-only
gateway, using the [Codex configuration contract](https://learn.chatgpt.com/docs/config-file/config-reference).

```sh
podman build -f deck/Dockerfile -t YOUR_REGISTRY/deck:YOUR_RELEASE deck
podman build -f marina/Dockerfile -t YOUR_REGISTRY/marina:YOUR_RELEASE marina
podman build -f marina/Dockerfile.worker --build-arg CODEX_VERSION=YOUR_TESTED_VERSION -t YOUR_REGISTRY/marina-worker:YOUR_RELEASE marina
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

Edit `workflow.json`: Git locations and identity, model location/name/rates,
worker image **digest**, trusted commands, allowed paths, required CI names and
budget. Set the independently built Deck and Marina images in the generated
`kustomization.yaml`. Push/load your images using your registry's normal process.
The worker cannot download dependencies from the internet. Vendor dependencies
in the repository or extend the image with pre-fetched dependencies and tools.
Go uses `/opt/marina-cache/go`, `GOPROXY=off`, `GOTOOLCHAIN=local` and a disposable
build cache. Other check commands must likewise work without public egress.

The sample permits `src` and `tests`; change these to your actual bounded scope.
Git metadata, Codex configuration, AGENTS.md and GitHub workflow files cannot be
changed by this release. Checks are commands from installation configuration,
not commands supplied by the coding agent. A patch is limited to 30 regular
files and 2 MiB. Individual result artifacts are limited to 4 MiB.

`required_ci` contains the exact check/status names that must report success on
the published head. A missing check waits until the task deadline; it never
counts as success. An explicitly empty list means only the configured local
checks gate completion. Failed checks or requested changes block the task;
create a focused follow-up task. Automatic revision loops, pause/resume, merge
and deployment are outside this first slice. Cancel is supported.

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
   worker's tools/dependencies and chosen model work through the gateway.
2. In Deck create a task with observable acceptance criteria. Check **Allow
   Marina to change the configured repository and open a PR**, allow at least
   two attempts, set a realistic token/time limit, and place it in Ready.
3. Approve the displayed plan. Inspect the worker's namespace, UID, read-only
   root, absent service-account token, resource limits and network restrictions.
   Confirm direct internet/API-server access from the worker is denied.
4. Restart Marina during execution. Confirm the same Job is recovered and that
   only one branch/commit/PR is created. Restart again while waiting for CI.
5. Confirm verification uses a second clean Job at the PR head, required CI is
   green, and Deck links to the PR and downloadable digest-checked evidence.
6. Run a failing check and confirm the task blocks with its evidence. Move the
   PR head after verification and confirm old evidence cannot complete the task.
7. Cancel a running task in Deck. Confirm model/source requests lose authority,
   the process exits, the Job/credential are cleaned up, and cancellation is
   acknowledged only after termination is observed.
8. Test provider outage and expired task budget. They must not permit continued
   model or publication authority. Inspect retained artifacts after Job cleanup.

Backup/restore is outside the current pilot's acceptance scope. When activated,
back up each SQLite database **with its WAL consistently**, and Marina's artifact
directory and identity/configuration separately. For this initial single-replica
release, stop new tasks, wait for active work to finish, stop the deployments and
take volume snapshots before restoring to an isolated environment. Deck's
logical export is useful but does not replace Marina's journal/artifact backup.
Verify a restore before treating that backup as a recovery mechanism.

Repository checks: `make check`, `make conformance`, `make build`. Passing these
does not replace the selected installation's network, model and recovery checks.
