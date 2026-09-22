# Local Podman/Kind installation

This profile runs Deck and Marina in the dedicated `kind-marina-local` context
on the `podman-machine-default-root` connection. It preserves the ordinary
rootless Podman connection and its containers.

Deck is available at **https://localhost:8443**. The operator token is stored at
`~/.local/share/the-platform/marina-local/installation/operator-token`.
Copy it without printing it:

```sh
pbcopy < ~/.local/share/the-platform/marina-local/installation/operator-token
```

The installation is localhost-only and uses the Mac as its host, so the Mac must
remain running. Laptop-off and LAN/second-device acceptance are outside this
profile. New task intake stays disabled except during a bounded exercise.

The validated private overlay contains pinned Deck, Marina, coding-worker, and
analysis-worker images; the disposable acceptance repository; offline checks;
and explicit Git/model credentials and budget rates. Credentials live in private
files and are never inferred from developer logins. See the
[repository acceptance](../../../docs/validation/poc-mvp-2026-09-21.md) and
[two-workload acceptance](../../../docs/validation/agent-11-13-2026-09-22.md).

## Inspect and start

```sh
kubectl --context kind-marina-local -n marina-system get pods,pvc,svc
podman --connection podman-machine-default-root ps
sh scripts/local-podman-start.sh
```

The optional LaunchAgent installed by
`python3 scripts/install-local-podman-startup.py` starts the existing VM and
cluster at login. Its log is
`~/.local/share/the-platform/marina-local/startup.log`. It does not recreate a
deleted cluster or change Podman's default connection.

Deck and Marina use separate `standard` local-path PVCs in the Kind node. They
survive service and node restarts. Deleting the cluster or Podman VM removes this
storage; take a backup first if the data matters. Backup/restore automation is
outside the current pilot.

## Reproduce on a Mac

The accepted environment used an ARM64 Mac, Podman 6.1.1, an 8 GiB VM, Kind
0.33.0, the node image pinned in `kind.yaml`, Calico 3.32.2, and Codex CLI
0.153.4. Acceptance reports record the exact application image and profile
digests.

Install Kind if needed. Inspect running containers before resizing or restarting
an existing Podman VM.

```sh
export KIND_EXPERIMENTAL_PROVIDER=podman
export CONTAINER_CONNECTION=podman-machine-default-root
kind create cluster --config deploy/kubernetes/local-podman/kind.yaml
curl -fsSL https://raw.githubusercontent.com/projectcalico/calico/v3.32.2/manifests/calico.yaml -o /private/tmp/marina-calico.yaml
kubectl --context kind-marina-local apply --server-side -f /private/tmp/marina-calico.yaml
```

Kind's default CNI is disabled so Calico can enforce NetworkPolicy.
`deck-host-ingress.yaml` permits the observed macOS Podman gateway
`192.168.127.1/32`. If it differs, inspect
`podman machine ssh podman-machine-default ip -j route` and change only that
address. The published Deck port remains bound to `127.0.0.1`.

Build and load the base images without a remote registry:

```sh
podman build -f deck/Dockerfile -t localhost/the-platform/deck:local deck
podman build -f marina/Dockerfile -t localhost/the-platform/marina:local marina
podman build -f marina/Dockerfile.worker --build-arg CODEX_VERSION=0.153.4 -t localhost/the-platform/marina-worker:local marina
podman save --format docker-archive -o /private/tmp/marina-local-images.tar localhost/the-platform/deck:local localhost/the-platform/marina:local
kind load image-archive --name marina-local /private/tmp/marina-local-images.tar
podman save --format oci-archive -o /private/tmp/marina-local-worker.tar localhost/the-platform/marina-worker:local
kind load image-archive --name marina-local /private/tmp/marina-local-worker.tar
```

Prepare credentials once. The helper refuses to overwrite an existing directory;
reuse it on upgrades to preserve credentials and trust.

```sh
python3 scripts/prepare-installation.py --local-podman ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local apply -k ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local -n marina-system rollout status deployment/deck
kubectl --context kind-marina-local -n marina-system rollout status deployment/marina
```

Trust the generated **localhost leaf certificate only** in the user keychain:

```sh
security add-trusted-cert -r trustAsRoot -p ssl -s localhost -k ~/Library/Keychains/login.keychain-db ~/.local/share/the-platform/marina-local/installation/deck.crt
```

Other clients can use
`--cacert ~/.local/share/the-platform/marina-local/installation/ca.crt`.
Leaf certificates last 90 days. Follow the
[operations runbook](../first-release/OPERATIONS.md) to renew certificates
without replacing service identities or credentials.

## Verify the base

```sh
python3 scripts/verify-local-podman-network.py
python3 scripts/verify-local-podman-ledger.py ~/.local/share/the-platform/marina-local/installation
```

The network check creates temporary unrestricted and isolated Jobs and verifies
worker tools, Marina TLS, absent service-account credentials, and denied
API/internet access. The ledger check restarts both services and proves that the
operator session, a task, and Marina's identity survive. Intake must remain
disabled.

## Configure repository execution

Follow the [repository workflow guide](../first-release/README.md). Populate the
private workflow with the GitHub/GHES repository, model endpoint, credential
references, trusted checks, allowed paths, required CI, and budgets. Pin workers
by the **imported containerd manifest digest**, which can differ from Podman's
digest after archive conversion.

Use the [repository-workflow profile](../local-podman-workflow/kustomization.yaml).
In the private `kustomization.yaml`, change only the resource path ending in
`deploy/kubernetes/local-podman` to
`deploy/kubernetes/local-podman-workflow`. Preserve pinned images, credentials,
the existing Deck PVC override, and the paired controller egress/host-alias
files. This gives Marina its controller service-account token while workers keep
their separate identity and no API token.

Run preflight, render privately, apply the reviewed overlay, and confirm readiness
with intake still disabled. Enable intake only for the selected task, then restore
`args: [serve, --intake-enabled=false]`. Disabling intake does not pause active
work, so settle journaled executions first.

Before trusting a new repository, model, worker image, or network policy, repeat
the [installation acceptance procedure](../first-release/README.md#installation-acceptance).
Use [NETWORK.md](../first-release/NETWORK.md) when provider addresses change.
For the diagnostic profile and its analysis image, follow the
[two-workload guide](../agent-platform/README.md).
