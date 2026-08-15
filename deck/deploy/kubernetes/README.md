# Kubernetes Deployment Baseline

This directory packages AppDeck as an ordinary, single-replica Kubernetes application with its own durable SQLite volume. It is the first cluster deployment baseline, not a claim that Kubernetes resources are AppDeck's public product model.

The base intentionally deploys:

- one AppDeck replica with `Recreate` rollout semantics;
- a `ReadWriteOnce` persistent volume mounted at `/data`;
- the demo workspace and Atlas demo agent disabled;
- a non-root, read-only container with all Linux capabilities dropped;
- no Kubernetes API token, because AppDeck does not call the Kubernetes API;
- startup and liveness probes backed by `/healthz`;
- a readiness probe backed by `/readyz`, which checks the SQLite store and configured service-authorization policy;
- bounded CPU, memory, and temporary storage;
- a cluster-internal Service and no ingress.

Do not scale this base above one replica. Move AppDeck to PostgreSQL before introducing concurrent application replicas.

## Render

```bash
kustomize build deploy/kubernetes/base
```

The rendered image defaults to `ghcr.io/jmlero/the-platform-appdeck:dev`. Set an immutable tag or digest in an environment overlay before deployment.

## Deploy

After selecting a cluster, storage class, namespace, and immutable image:

```bash
kubectl apply -k deploy/kubernetes/base
kubectl rollout status deployment/appdeck
```

The first always-on cluster location remains an explicit product decision. These manifests avoid selecting a cloud, ingress controller, certificate system, external secret manager, storage class, or backup product.

## Authentication boundary

The base deliberately creates neither an ingress nor an `APPDECK_SERVICE_AUTH_FILE`. Human sessions are not implemented, so the operator routes must not be exposed to an untrusted network. An environment overlay may mount the selected Marina service-authorization policy as a read-only Secret and set `APPDECK_SERVICE_AUTH_FILE` to that file only when the matching encrypted service transport is also configured.

For trusted diagnostics, use local port forwarding rather than publishing the unauthenticated operator surface:

```bash
kubectl port-forward service/appdeck 8080:8080
```

## Storage and recovery

The database and SQLite WAL files live under `/data`. The cluster's backup and restore mechanism must capture the volume consistently. During the single-replica phase, use a storage-system volume snapshot or stop AppDeck before copying database files. After restoration, verify `/readyz` and reconcile Marina against the exact existing tasks and runs; do not repair cross-product state with timestamp-based last-write-wins logic.

## Current boundary

This base makes AppDeck deployable while the operator's laptop is offline, but it is not the complete paired installation. Human authentication, the Marina credential Secret, encrypted service transport, network policies, backups, and ingress belong in later decisions and environment overlays. The base does not deploy Marina or any agent runtime.
