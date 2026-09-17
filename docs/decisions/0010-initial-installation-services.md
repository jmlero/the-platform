# Decision 0010: Initial installation services and storage

- Status: accepted initial configuration direction; credentials, model, repository, and live validation remain pending
- Date: 2026-09-17
- Scope: AppDeck and Marina development installation and first repository pilot

## Decisions

The product targets vanilla Kubernetes. Use the existing local Podman/Kind
installation for development and integration testing, as recorded in
[decision 0008](0008-local-podman-development-installation.md). No cloud-provider
selection is required.

| Area | Initial choice |
| --- | --- |
| Model inference | OpenRouter using an operator-supplied API token. Select and test a compatible model before the first live coding run. |
| Source control | GitHub for the acceptance repository, branches, pull requests, and configured CI checks. |
| Durable application state | Keep the existing separate AppDeck and Marina SQLite databases on separate Kubernetes persistent volumes. |
| Artifacts | Keep Marina's existing file artifact store on its persistent volume; AppDeck accesses it through the authenticated API. |
| Images and packaged dependencies | Use GitHub's container registry, GHCR, for published application/worker images. Preload images locally when useful. Package the dependencies needed by the acceptance checks in the worker image or repository. |
| Backups | Defer backup automation and restore drills for the current pilot. They are not an acceptance prerequisite at this stage. |
| Runtime connectivity | The running platform requires only GitHub services and OpenRouter externally. Internal service traffic, Kubernetes API access, and cluster DNS remain inside the installation. |
| Build and setup connectivity | Image building and cluster setup may download required base images, Go modules, npm packages, OS packages, and other build/setup dependencies from their normal sources. The runtime restriction does not apply to these preparation phases. |

SQLite is embedded in each application process; this requires no additional
database service. Persistent volumes retain application state across Pod
replacement. Backup deferral does not remove persistence or restart tests.
No external database, artifact service, or backup service is required.

The operator intends to build a Git service application in the future. Preserve
the existing source-control boundary so a future adapter can integrate it when
its API exists. Implementing that application or a speculative adapter is outside
the current pilot.

## Model and network interpretation

OpenRouter documents a [Responses endpoint](https://openrouter.ai/docs/api/api-reference/responses/create-responses).
The intended Marina configuration is base URL `https://openrouter.ai/api/v1`,
which the existing gateway calls with the `/responses` suffix. This is a
configuration candidate, not evidence that the pinned harness and chosen model
already work together. Validate tool calls, streaming, and usage accounting under
RUN-09. Keep the token in the installation's private credential mechanism and
mount it into the controller; do not store its value in Git or task content.

The cluster connects to OpenRouter for inference; OpenRouter in turn
[routes requests to model providers](https://openrouter.ai/docs/guides/routing/provider-selection).
This pilot therefore uses external inference, including its upstream processing,
while execution and durable application state remain in Kubernetes.

GitHub includes its API, repository transfers, and the
[GHCR container registry](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
at `ghcr.io`. RUN-02 must enumerate the actual service/download destinations
needed by this installation, including redirects, before RUN-06 validates
network enforcement. "GitHub" is a service choice, not a literal one-host rule.
Use cluster DNS and account for its configured upstream resolution without
adding an application dependency on a public DNS service.

Keep the existing worker boundary: workers reach Marina and cluster DNS;
Marina mediates model and source access. Treat node-level image pulls separately
from Pod traffic when recording and testing the installation's network behavior.

Build and preload the tools and dependencies needed during task execution.
The preparation exception does not authorize running agent Jobs or their
verification commands to fetch packages from additional external registries.
Use GHCR or locally loaded images for the application and worker runtime.

## Remaining configuration

- Choose the exact OpenRouter model and bounded test budget; supply its token
  through the private credential mechanism when preparing the live run.
- Choose the disposable GitHub repository, GHCR image namespace, and scoped
  credentials. Existing developer login alone is not a cluster credential.
- Record image versions, storage configuration, and the concrete network rules,
  then run the local checks. This decision alone marks no runtime test complete.

Backup/restore tasks may be revisited when the operator chooses to retain work
that needs recovery beyond the existing volumes. The broader product remains
configurable for other compatible services; these are the initial pilot choices.
