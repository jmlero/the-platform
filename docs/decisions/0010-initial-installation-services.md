# Decision 0010: Initial installation services and storage

- Status: accepted; local repository and two-workload validation passed, while backup/restore remains deferred
- Date: 2026-09-17
- Scope: Deck and Marina development installation and first repository pilot

## Decisions

The product targets vanilla Kubernetes. Use the existing local Podman/Kind
installation for development and integration testing, as recorded in
[decision 0008](0008-local-podman-development-installation.md). The existing GCP
cluster is also authorized for development/testing as recorded below; the product
does not require a cloud provider.

| Area | Initial choice |
| --- | --- |
| Model inference | OpenRouter using an operator-supplied API token. The local pilot validated `z-ai/glm-5.3-flash`; other installations must validate their selected model. |
| Source control | GitHub for the acceptance repository, branches, pull requests, and configured CI checks. |
| Durable application state | Keep the existing separate Deck and Marina SQLite databases on separate Kubernetes persistent volumes. |
| Artifacts | Keep Marina's existing file artifact store on its persistent volume; Deck accesses it through the authenticated API. |
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

## Additional development/test environment — 2026-09-21

The operator authorized both local Kubernetes and the existing Kubernetes cluster
in their GCP account for development and testing. The GCP infrastructure source
of truth is `/Users/jmolero/personal/github/infra-gcp`; read its `AGENTS.md` and
`README.md` before using or changing that environment.

- Local: context `kind-marina-local`, Podman connection
  `podman-machine-default-root`, and the existing localhost installation.
- GCP: project `jmlero`, region `europe-west3`, GKE Autopilot cluster `autopilot`.
  Use the configured `gcloud` CLI, `gke-gcloud-auth-plugin`, and the
  IAM-authenticated DNS endpoint. Fetch access with
  `gcloud container clusters get-credentials autopilot --region europe-west3 --project jmlero --dns-endpoint`
  using a separate `KUBECONFIG`, then pass context
  `gke_jmlero_europe-west3_autopilot` explicitly to `kubectl`. Preserve the default
  local context and existing credentials.
- GCP's total budget is EUR 10/month under the infrastructure repo's guidance.
  Check existing spend and projected total, including workloads, storage,
  networking, telemetry, and applicable taxes, before adding billable resources.
  Keep test workloads short-lived and remove only test-owned resources after use;
  preserve existing persistent data. A budget alert is not a spending cap.

Relevant development/test deployments and service restarts in either environment
are authorized within those limits. Keep installation identities, credentials,
databases, and artifacts separate. Repository mutation authority, task intake,
and model budgets still require their existing explicit configuration and scope.
This adds an environment without migrating the local installation or changing
the first-release acceptance gate. Record the actual environment for each test;
local evidence does not establish GCP storage, network isolation, or workflow
acceptance. RUN-10 and RUN-17 remain retired.

The [2026-09-21 access check](../validation/gcp-access-2026-09-21.md) confirmed
the cluster is running and authenticated Kubernetes reads work. It records local
CLI setup; no Platform workload or acceptance exercise was run in GCP.

## Model and network interpretation

OpenRouter documents a [Responses endpoint](https://openrouter.ai/docs/api/api-reference/responses/create-responses).
The Marina configuration uses base URL `https://openrouter.ai/api/v1`, which the
gateway calls with the `/responses` suffix. The local pilot validated the pinned
harness, selected model, tool calls, streaming, and usage accounting. A different
installation or model requires its own acceptance. Keep the token in the installation's private credential mechanism and
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

## Validation and remaining boundary

The local pilot selected a disposable GitHub repository, explicit credentials,
pinned local images, `z-ai/glm-5.3-flash`, conservative rates, and concrete
network rules. The [repository acceptance](../validation/poc-mvp-2026-09-21.md)
and [two-workload acceptance](../validation/agent-11-13-2026-09-22.md) record the
result. Existing developer login is still not a cluster credential, and no other
installation inherits this evidence.

Backup/restore tasks may be revisited when the operator chooses to retain work
that needs recovery beyond the existing volumes. The broader product remains
configurable for compatible services; these are the initial pilot choices.
