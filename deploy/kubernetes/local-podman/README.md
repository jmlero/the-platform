# Local Podman installation

Deck and Marina run in a dedicated `kind-marina-local` Kubernetes context on
the `podman-machine-default-root` connection. The ordinary rootless Podman
connection and its containers are preserved. Everything below uses the CLI.

Open **https://localhost:8443**. The login token is in
`~/.local/share/the-platform/marina-local/installation/operator-token`.
To copy it without printing it in terminal history or logs:

```sh
pbcopy < ~/.local/share/the-platform/marina-local/installation/operator-token
```

This profile runs authenticated TLS services with **new task intake disabled**.
The mounted `workflow.json` is an inactive template until a repository and model
endpoint are chosen. No model credential or Git credential is inferred from the
operator's development tools. No external coding run is implied by a healthy
local installation.

The pilot now selects OpenRouter and GitHub/GHCR under
[decision 0010](../../../docs/decisions/0010-initial-installation-services.md).
The exact model, repository, and credentials remain to be configured and tested;
this decision does not change the existing disabled-intake installation. SQLite
databases and artifacts stay on their existing Kubernetes volumes. Backup/restore
work is deferred for now.

## Inspect and start

```sh
kubectl --context kind-marina-local -n marina-system get pods,pvc,svc
podman --connection podman-machine-default-root ps
sh scripts/local-podman-start.sh
```

The optional user LaunchAgent installed by
`python3 scripts/install-local-podman-startup.py` starts the existing VM and
cluster at login. Its log is
`~/.local/share/the-platform/marina-local/startup.log`. It does not recreate a
deleted cluster or alter Podman's default connection. Running the start script
manually is sufficient when no LaunchAgent is installed.

Both databases use separate `standard` local-path PVCs inside the Kind node.
They survive service and node restarts. Deleting the cluster or Podman VM removes
this local storage; export/backup before doing either. A sleeping or powered-off
Mac cannot execute work. This is a development installation, not the always-on
production acceptance gate.

## Reproduce on a Mac

The validated installation uses an ARM64 Mac, Podman 6.1.1, an 8 GiB VM,
Kind 0.33.0, the node image pinned in `kind.yaml`, Calico 3.32.2 and Codex CLI
0.153.4. The worker's real model compatibility is still unverified.

Install Kind if necessary (`brew install kind`). Before resizing an existing
Podman VM, inspect its running containers and account for the interruption.
The local cluster uses the existing root connection explicitly:

```sh
export KIND_EXPERIMENTAL_PROVIDER=podman
export CONTAINER_CONNECTION=podman-machine-default-root
kind create cluster --config deploy/kubernetes/local-podman/kind.yaml
curl -fsSL https://raw.githubusercontent.com/projectcalico/calico/v3.32.2/manifests/calico.yaml -o /private/tmp/marina-calico.yaml
kubectl --context kind-marina-local apply --server-side -f /private/tmp/marina-calico.yaml
```

The [Kind configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
disables its default CNI; [Calico's Kind installation](https://docs.tigera.io/calico/latest/getting-started/kubernetes/kind)
provides network policy enforcement. `deck-host-ingress.yaml` allows only the
observed macOS Podman forwarding gateway, `192.168.127.1/32`. If it differs,
inspect `podman machine ssh podman-machine-default ip -j route` and adjust that
one address. The published host port remains bound to `127.0.0.1`.

Build and load images without a remote registry:

```sh
podman build -f deck/Dockerfile -t localhost/the-platform/deck:local deck
podman build -f marina/Dockerfile -t localhost/the-platform/marina:local marina
podman build -f marina/Dockerfile.worker --build-arg CODEX_VERSION=0.153.4 -t localhost/the-platform/marina-worker:local marina
podman save --format docker-archive -o /private/tmp/marina-local-images.tar localhost/the-platform/deck:local localhost/the-platform/marina:local
kind load image-archive --name marina-local /private/tmp/marina-local-images.tar
podman save --format oci-archive -o /private/tmp/marina-local-worker.tar localhost/the-platform/marina-worker:local
kind load image-archive --name marina-local /private/tmp/marina-local-worker.tar
```

Prepare credentials once; the preparation command refuses to overwrite an
existing directory. Reuse it on upgrades to preserve credentials and trust.

```sh
python3 scripts/prepare-installation.py --local-podman ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local apply -k ~/.local/share/the-platform/marina-local/installation
kubectl --context kind-marina-local -n marina-system rollout status deployment/deck
kubectl --context kind-marina-local -n marina-system rollout status deployment/marina
```

Trust the generated **leaf certificate for localhost only** in your user keychain:

```sh
security add-trusted-cert -r trustAsRoot -p ssl -s localhost -k ~/Library/Keychains/login.keychain-db ~/.local/share/the-platform/marina-local/installation/deck.crt
```

Clients using their own trust store should supply
`--cacert ~/.local/share/the-platform/marina-local/installation/ca.crt`.
The leaf certificates last 90 days. Renew them before expiry and update the
Secrets and local leaf trust; do not regenerate operator/service credentials.

## Verify

```sh
python3 scripts/verify-local-podman-network.py
python3 scripts/verify-local-podman-ledger.py ~/.local/share/the-platform/marina-local/installation
```

The network check launches two temporary Jobs: an unrestricted baseline and an
isolated worker. It checks installed tools, TLS to Marina, absent service-account
credentials, and blocked API/internet connections. Jobs expire automatically.
The ledger check restarts both Deployments and proves persistence of an operator
session, a task and Marina's identity. It verifies intake is disabled and leaves
its harmless verification task cancelled.

## Enable repository execution later

Follow the [repository installation acceptance](../first-release/README.md).
Populate the private workflow's GitHub/GHES repository, explicit model endpoint,
credentials, trusted checks, allowed paths and required CI. Pin the worker using
the **imported containerd manifest digest**, which can differ from Podman's
digest after archive conversion. Register the digest reference in containerd
before using it without a registry.

In a reviewed installation overlay, set `MARINA_CONFIG_FILE` to
`/etc/marina/workflow.json`, remove `--intake-enabled=false`, enable Marina's
service-account token, and add explicit controller egress for the Kubernetes
API and chosen Git/model endpoints. Preserve worker isolation. Then complete
the real coding, cancellation and recovery acceptance checks before relying on
autonomous work.

## Verified on 2026-09-09

The local installation has running Deck/Marina Deployments, bound PVCs and a
loaded worker image. Real worker tools and network isolation, authenticated TLS,
operator login, disabled intake and both service restarts were checked. No real
repository change, model inference or pull request has been executed.
