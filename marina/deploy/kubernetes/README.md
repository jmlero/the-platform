# Kubernetes Deployment Baseline

This directory packages Marina's durable control service for an ordinary Kubernetes cluster. It is the first always-on substrate, not a Kubernetes-specific product API and not yet an agent runtime.

The base intentionally deploys:

- one Marina replica;
- a `ReadWriteOnce` persistent volume for the SQLite journal;
- `Recreate` rollout semantics so two control-service writers cannot overlap;
- a non-root, read-only container with all Linux capabilities dropped;
- no Kubernetes API token in this base; an installation that serves the platform API mounts one (see below);
- liveness, readiness, and startup probes backed by Marina's HTTP endpoints;
- bounded CPU, memory, and temporary storage.

Do not scale this base above one replica. Move the journal to PostgreSQL before introducing concurrent control-service replicas. Agent workloads will be separate ephemeral Jobs with their own task-scoped identity; they should not inherit this service account.

## Render

```bash
kustomize build deploy/kubernetes/base
```

The rendered image defaults to `ghcr.io/jmlero/the-platform-marina:dev`. Set an immutable image tag or digest in an environment overlay before deployment.

Marina calls no client. With a workflow configuration it serves the `marina.dev/v1` platform API and needs its service account token and the CRDs and RBAC from [`deploy/kubernetes/platform/marina-api`](../../../deploy/kubernetes/platform/marina-api); clients present their own Kubernetes tokens ([decision 0033](../../../docs/decisions/0033-marina-platform-api.md)). Configure TLS for non-loopback traffic.

## Deploy

After publishing the image and selecting a cluster, storage class and namespace:

```bash
kubectl apply -k deploy/kubernetes/base
kubectl rollout status deployment/marina
```

The first always-on cluster location remains an explicit product decision. These manifests avoid choosing a cloud, ingress, certificate system, external secret manager, or storage class on the operator's behalf.

## Storage and recovery

The journal and SQLite WAL files live under `/data`. The cluster's persistent-volume backup and restore mechanism must capture that volume consistently. During this single-replica phase, use a volume snapshot supported by the chosen storage system or stop Marina before copying the database files. A restored control service reconstructs recovery intents from its journal and platform ledger.

## Current boundary

This base runs the control-only mode. The [first-release overlay](../../../deploy/kubernetes/first-release/README.md) adds the workflow, TLS, worker Jobs, network policies and the platform API.
