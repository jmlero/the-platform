# Local profile with Marina from its Helm chart

This profile runs Marina from the [Marina chart](../../../marina/deploy/helm/marina/README.md)
(MAR-11) and keeps Deck, the shared namespaces, Secrets, ConfigMaps, the Marina
PVC and the CRDs in the [kagent profile](../local-podman-kagent/README.md). Use
only `kind-marina-local` on `podman-machine-default-root`.

[kustomization.yaml](kustomization.yaml) removes from its render every object
the chart owns. `kubectl apply` does not prune, so those objects stay on the
cluster until Helm adopts them. [marina-values.yaml](marina-values.yaml) holds
the shared local chart values; the private installation adds `marina-values.yaml`
with the image digest, intake, host aliases and site egress.

## Move an installation to the chart

With no work active and a data copy taken as in the
[cutover steps](../../../docs/plans/marina-platform-api.md#mar-10-cutover-steps):

1. Mark the objects the chart will own for adoption. For each object the chart
   renders (`helm template` lists them), add the label
   `app.kubernetes.io/managed-by=Helm` and the annotations
   `meta.helm.sh/release-name=marina` and
   `meta.helm.sh/release-namespace=marina-system`.
2. Install: `helm upgrade --install marina marina/deploy/helm/marina -n marina-system
   -f deploy/kubernetes/local-podman-helm/marina-values.yaml -f <private>/marina-values.yaml`.
   Marina restarts once on the same PVC.
3. In the private Kustomization, use this directory as the resource, and remove
   the patches that targeted the Marina Deployment (their content moved to the
   private chart values). Apply it; Deck does not restart unless its config changed.
4. Delete the two policies the chart folds into its own:
   `marina-to-kagent` and `marina-from-kagent-egress` in `marina-system`.
5. Check `/readyz`, `helm status marina`, `kubectl get projects,workloads -n <project namespace>`
   and that Deck still shows its history.

Roll back with `helm rollback`, or by reapplying the previous private
Kustomization after `helm uninstall --keep-history marina` (the PVC is not
managed by the chart and stays).
