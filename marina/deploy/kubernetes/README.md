# Kubernetes Deployment Baseline

This directory packages Marina's durable control service for an ordinary Kubernetes cluster. It is the first always-on substrate, not a Kubernetes-specific product API and not yet an agent runtime.

The base intentionally deploys:

- one Marina replica;
- a `ReadWriteOnce` persistent volume for the SQLite journal;
- `Recreate` rollout semantics so two control-service writers cannot overlap;
- a non-root, read-only container with all Linux capabilities dropped;
- no Kubernetes API token, because the current control service does not need one;
- liveness, readiness, and startup probes backed by Marina's HTTP endpoints;
- bounded CPU, memory, and temporary storage.

Do not scale this base above one replica. Move the journal to PostgreSQL before introducing concurrent control-service replicas. Agent workloads will be separate ephemeral Jobs with their own task-scoped identity; they should not inherit this service account.

## Render

```bash
kustomize build deploy/kubernetes/base
```

The rendered image defaults to `ghcr.io/jmlero/the-platform-marina:dev`. Set an immutable image tag or digest in an environment overlay before deployment. The base also assumes AppDeck is reachable as `http://appdeck:8080`; patch `MARINA_APPDECK_URL` when its Service has another name or lives outside the namespace.

## Deploy

After publishing the image and selecting a cluster, storage class, namespace, and AppDeck endpoint:

```bash
kubectl apply -k deploy/kubernetes/base
kubectl rollout status deployment/marina
```

The first always-on cluster location remains an explicit product decision. These manifests avoid choosing a cloud, ingress, certificate system, external secret manager, or storage class on the operator's behalf.

## Storage and recovery

The journal and SQLite WAL files live under `/data`. The cluster's persistent-volume backup and restore mechanism must capture that volume consistently. During this single-replica phase, use a volume snapshot supported by the chosen storage system or stop Marina before copying the database files. A restored control service will reconstruct recovery intents and verify the exact AppDeck runs before renewing any lease.

## Current boundary

This base keeps Marina alive when the operator's laptop is off, but it does not create Kubernetes Jobs for agents yet. The next runtime increment must define the provider-neutral start, inspect, resume, cancel, artifact, and termination contract before mapping it to a Kubernetes Job.
