# Pilot destinations and local enforcement

The pilot uses GitHub, GHCR and OpenRouter under [decision 0010](../../../docs/decisions/0010-initial-installation-services.md).
The [2026-09-17 acceptance report](../../../docs/validation/run-01-18-2026-09-17.md)
records observed local results. This document distinguishes controller/worker
traffic, node image loading, and build/setup downloads.

## Application runtime

| Caller | Destination | Data and purpose |
| --- | --- | --- |
| Operator browser | `https://localhost:8443` for the local installation | Login token exchange, session cookies, task intent, decisions, results and evidence. Bound to loopback; the current test scope does not require LAN or second-device access. |
| Marina | `https://deck.marina-system.svc:8080` | Scoped service authentication, task/lease/authority reads and ledger projections. |
| Deck and isolated workers | `https://marina.marina-system.svc:8090` | Authenticated artifacts, attempt inputs/results, source reads and model requests; TLS uses the installation CA. |
| Marina | `https://kubernetes.default.svc:443` and its observed API endpoint | Job/Pod/configuration lifecycle under Marina's service identity. The local endpoint is `10.89.0.2:6443`; the service is `10.96.0.1:443`. Workers have neither this access nor an API token. |
| Marina | `https://api.github.com/repos/jmlero/marina-acceptance/...` | Git refs, trees/blobs, commits, PR publication, CI statuses and review observations. Controller-only credential, never mounted in workers. The local pilot uses the operator-approved [classic-token exception](../../../docs/validation/github-credential-2026-09-21.md). |
| Marina source gateway | `https://github.com/jmlero/marina-acceptance.git/info/refs?service=git-upload-pack` and `/git-upload-pack` | Authenticated Git smart-HTTP reads of the pinned repository for workers. No Git LFS, submodule downloads or archive service is required by the fixture. |
| Marina model gateway | `https://openrouter.ai/api/v1/responses` | Task/code/tool input, generated output and usage. OpenRouter credential stays in Marina. The local pilot uses `z-ai/glm-5.3-flash` within the €5 total test budget; [live evidence and accounting limits](../../../docs/validation/poc-mvp-2026-09-21.md). |
| Platform Pods | Cluster DNS, UDP/TCP 53 in `kube-system` | Internal names and external DNS queries. This is a separate allowed channel, not a guarantee that workers cannot send data in DNS queries. |

The source and GitHub API clients and model gateway reject redirects instead of
forwarding credentials to another host. Application code uses the configured
HTTPS names even when the local installation pins their resolved IPs. Certificate
verification and TLS server names remain enabled.

OpenRouter documents the [Responses endpoint](https://openrouter.ai/docs/api/api-reference/responses/create-responses).
Its [provider routing](https://openrouter.ai/docs/guides/routing/provider-selection)
can send requests to upstream model providers. No fallback to another Marina
endpoint does **not** mean no upstream provider routing. This is external
inference; neither Kubernetes ownership nor `store: false` proves local-only
processing, a particular processing region, or an upstream retention policy.
No model request was made during the local network probes.

## Node, build and CI traffic

| Phase | Destinations | Treatment |
| --- | --- | --- |
| This local image installation | Host-to-Podman/Kind image archive import | All three application images are loaded locally and selected by imported manifest digest. No GHCR pull is needed for these checks. |
| Future published image pulls | `ghcr.io:443`, including `/v2/` and token exchange; blob delivery can use `pkg-containers.githubusercontent.com:443` | Node/registry-client traffic, separate from Pod NetworkPolicy. Inspect the selected image's actual redirect chain before remote acceptance. No GHCR image publication or authenticated pull was validated here. |
| Image build | Docker Hub base images, Debian package mirrors, npm registry, Go module proxy/checksum services as required by the existing Dockerfiles | Preparation traffic. Packages and tools are installed before workers start. |
| Cluster setup | Kind/node images, Kubernetes image registry, Calico images/manifests and local-path storage components | Preparation traffic. Existing local components were reused. |
| GitHub-hosted fixture CI | GitHub's runner provisioning and the pinned checkout action; GitHub-hosted runner Node.js | Runs outside the installation. The check itself is `node --test tests/clamp.test.cjs` with no package download. |

GitHub documents [GHCR](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)
and the [package/container download domains](https://docs.github.com/en/actions/reference/runners/larger-runners).
Do not add every GitHub-related domain to controller egress: the fixture uses only
`api.github.com` and `github.com`. Registry access is denied from controller and
worker probes; image pulling belongs to the node.

CoreDNS forwards external questions through the Kind node's resolver. On the
observed local profile, the node uses Podman DNS at `10.89.0.1` and
`fc00:f853:ccd:e793::1`; the Podman VM forwards to the Mac gateway
`192.168.127.1`. The Mac's upstream resolver is an installation dependency, not
a direct worker connection to a new public DNS service. DNS restriction to only
internal names is not implemented by these NetworkPolicies.

Deck and Marina retain separate SQLite databases on separate `standard`
ReadWriteOnce PVCs. Marina's artifacts remain on its own volume. No external
database, artifact service or backup service is involved.

## Reproduce the local policy and probes

Vanilla Kubernetes NetworkPolicy selects addresses, not HTTPS hostnames. The
first probes caught a real mismatch: GitHub answers changed between resolution
and use (`api.github.com` ended in `.5` or `.6`; `github.com` in `.3` or `.4`).
Sampling DNS repeatedly did not eliminate it. The local profile therefore pairs
an explicit IP policy with a Deployment `hostAliases` patch generated from the
same observation. This is a bounded local installation choice, not permanent
provider discovery or a policy valid for every cluster.

```sh
python3 scripts/prepare-local-podman-egress.py /private/tmp/marina-egress-new
```

The command reads only the dedicated Kind context and Podman node. It refuses
missing or non-public provider addresses, writes `policy.json`,
`marina-destinations.json` and timestamped `destinations.json` outside Git, and
changes no cluster resources. Review them together. In the private installation
overlay, add `policy.json` under `resources` and `marina-destinations.json` under
`patches: - path:`. Keep the existing internal Deck/DNS egress policy. Apply the
overlay with intake disabled and wait for Marina's rollout before probing.

```sh
python3 scripts/verify-local-podman-acceptance.py \
  localhost/the-platform/marina-worker@sha256:1f7217c2a81066b297a198daee855a3aabeee3469ed8726d8fac4765fc62b12b
```

Three disposable Jobs establish an unrestricted baseline, test the actual
controller labels/address mapping, and test worker isolation plus the offline
fixture. The controller probe has an always-failing readiness probe so it cannot
receive Marina Service traffic. Each Job has a deadline, resource limits,
read-only root, unprivileged UID, no API token, and only temporary storage. The
script removes its uniquely named Jobs/ConfigMaps and never calls a model,
changes source, or creates a Deck execution.

Address retirement or an API endpoint change requires regenerating **both**
files and rerunning the probes. A stale snapshot fails closed. Do not broaden the
policy to `0.0.0.0/0` to work around failures. The IPs can be shared with other
services; this is IP enforcement plus trusted controller configuration/TLS,
not a hostname-aware firewall. A future independent host needs its own address,
DNS, CNI and node-image-pull validation.
