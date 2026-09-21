# GCP development/test access — 2026-09-21

Scope: verify access to the additional environment authorized by the operator,
as recorded in [decision 0010](../decisions/0010-initial-installation-services.md).

## Environment and result

- Infrastructure source: `/Users/jmolero/personal/github/infra-gcp`.
- Project `jmlero`, region `europe-west3`, cluster `autopilot`.
- `gcloud container clusters describe autopilot --region europe-west3 --project jmlero`
  returned `RUNNING`, Autopilot enabled, DNS access enabled, and IP endpoint
  access disabled.
- Fetched the DNS endpoint configuration into a private temporary kubeconfig.
  An authenticated `kubectl get namespaces -o name` with explicit context
  `gke_jmlero_europe-west3_autopilot` succeeded.
- The default kubeconfig remained on `kind-marina-local`.

## Local CLI prerequisite

The GKE authentication plugin was missing. Installed it with
`gcloud components install gke-gcloud-auth-plugin --quiet`, following
[Google's access instructions](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/cluster-access-for-kubectl#install_plugin).
On this Mac it lives in `/opt/homebrew/share/google-cloud-sdk/bin`; include that
directory in the command's `PATH` when running GKE `kubectl` commands. The check
used the configured Google Cloud credentials without printing tokens or changing
the default Kubernetes context.

This was a read-only cluster check. No workloads, storage, networking, or
Terraform resources were changed. GCP spend was not assessed because no billable
resources were added. The infrastructure repo's EUR 10/month total budget still
applies before deploying tests. This does not establish Platform installation,
network-policy, persistence, or workflow acceptance on GCP.
