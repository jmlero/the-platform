# Isolated local kagent feasibility trial

This is the KINT-01/02 evaluation profile, not a supported Marina execution
backend. It uses `kind-marina-local` on `podman-machine-default-root`. Deck and
Marina keep their own deployments, stores, credentials and disabled intake.
The [removal inventory](../../../docs/kagent-removal-inventory.md) assigns later
replacement and deletion gates. See the [acceptance report](../../../docs/validation/kint-01-02-2026-09-22.md).

## Exact candidate and ownership

[`lock.json`](lock.json) pins kagent `1.0.0-alpha1` at
`850c464726881317997c32515f8b621b949d6ce1`, Substrate `0.2.0-beta4` at
`a7701e8dc8cb63a18d63e7fe771aff5a5ecca279`, chart archive hashes and OCI digests,
ARM64 image manifests, administrative binaries and gVisor assets. It also pins
source dependency manifests, the Substrate transitive license bundle and disabled
chart dependencies. Image digests pin their transitive operating-system content;
keep upstream component/OS license notices when redistributing images. This is
an engineering dependency inventory, not an exhaustive legal audit or SBOM.

Do not substitute the moving 0.x website installation examples. This candidate
uses `kagent.dev/v1alpha3`, `kagent.api.v1alpha1` gRPC lifecycle APIs, A2A with
`a2a-go/v2 v2.5.0`, and `ate.dev/v1alpha1`. Native Codex embeds CLI `0.148.0`,
distinct from the existing Marina worker's `0.153.4`. No Marina profile changes
are made here. The separate [KINT-07/08 acceptance](../../../docs/validation/kint-07-08-2026-09-23.md)
proves pinned LangChain/BYO and the minimal question bridge on this topology.

The minimal active topology is one kagent controller, its PostgreSQL, one
Substrate API/controller/node agent, ingress and egress routers, credential
provider, certificate controller, Substrate PostgreSQL, RustFS snapshot store,
and one gVisor worker. No Redis, UI, kmcp, tool catalog, oauth2-proxy, vector
extension, tracing collector or model account is installed. The bucket-init Job
is preparation only. Three PVCs request 2.5 GiB in total; local-path capacity is
not a filesystem quota. Image/layer/gVisor caches consume additional node disk.

Substrate requires a privileged node agent, host paths under
`/var/lib/ateom-gvisor` and kubelet plugin directories, `/dev` access, and host
ports 8085/9090. This is trusted infrastructure, not authority granted to the
actor. ARM64 gVisor checkpoint/restore passed on the existing Podman/Kind node;
this says nothing about GCP compatibility. The single sample in the acceptance
report is an idle/lifecycle measurement, not a full-workload peak capacity claim.

## Authentication and isolation

The upstream kagent controller's `unsecure` mode is retained only inside this
synthetic trial: it is a caller-identity partition, **not authentication**.
There is no ingress, NodePort or UI. Administrative access is an authenticated
Kubernetes port-forward bound to `127.0.0.1`. NetworkPolicy denies worker access
to the controller, Deck, Kubernetes and the internet. Substrate uses generated
pod certificates, trust bundles and Kubernetes-issued identity, in separate
trial-owned namespaces. Its Secret reader and atespace-to-namespace credential grant are scoped to
`kint-engine`; its
controller's Secret access is scoped to `ate-system`, and Deployment/WorkerPool/
NetworkPolicy mutation is scoped to `kint-engine`. Required cluster watches and
certificate signer grants remain. Exact cluster-scoped objects are listed in
[`cluster-resources.json`](cluster-resources.json); three generated signer trust
bundles must also be removed on teardown.

Namespaces `ate-system`, `podcertificate-controller-system` and `kint-engine`
must be unused. Upstream has fixed internal references to the first two.
The installer refuses collisions rather than adopting existing resources.
The network profile pins the observed node `10.89.0.2`; a different address or
architecture fails preflight. DNS and node administrators remain trusted.

The fixture has no upstream model/Git credential. Native Codex requires Responses,
an exact DNS hostname and an API-key Secret reference. Its endpoint is the
reserved nonexistent `no-inference.invalid:9`, with a locally generated random
nonfunctional key. Do not invoke model tasks. No caller token passthrough,
memory, tools or project context is configured. This no-inference fixture does not exercise governed calls. The separate
[KINT-04 gateway recipe](GATEWAY.md) supplies synthetic model responses and tests
Marina admission, injected credentials, revocation and budgets on this same
isolated topology.

## Prepare and render

Use a private temporary asset directory. Fetch each chart with the exact OCI
URL/version in the lock, and save it using the recorded `archive` name. Helm
`3.19.0` (Darwin ARM64), Ruby's standard YAML parser, Python 3, OpenSSL, kubectl
and Podman were used. The renderer verifies archive hashes and rejects any
container image absent from the ARM64 lock. It disables optional components,
sets one API replica, adds resource requests/limits, and narrows RBAC.

```sh
python3 scripts/render-kagent-trial.py /private/tmp/kint-20260922 \
  --helm /private/tmp/kint-20260922/darwin-arm64/helm \
  --output /private/tmp/kint-20260922/trial.json
```

The two CLI release assets must be saved as `kubectl-ate` and `kagent-cli`, made
executable, and match their hashes in the lock. Download from the pinned
Substrate and kagent GitHub releases respectively. No binary or credentials
belong in Git. Review the render before installation.

The existing Kubernetes 1.37 node serves certificate `v1` by default, but this
Substrate version calls `v1beta1`. Before installing, save the original
`/etc/kubernetes/manifests/kube-apiserver.yaml` from the Kind container outside
its static-manifest directory. Change only its empty `--runtime-config=` argument
to `--runtime-config=certificates.k8s.io/v1beta1=true`, using the explicit root
Podman connection. Wait for `/readyz` and verify the beta discovery endpoint.
The API-server restart is a shared prerequisite, not a trial-owned deployment;
restore the exact saved file after teardown. Do not recreate the cluster or
change feature gates indiscriminately. Kubernetes warns these beta certificate
APIs disappear in 1.40; this lock is not qualified for that upgrade.

```sh
python3 scripts/render-kagent-trial.py /private/tmp/kint-20260922 \
  --helm /private/tmp/kint-20260922/darwin-arm64/helm \
  --output /private/tmp/kint-20260922/trial.json --preflight
python3 scripts/install-kagent-trial.py /private/tmp/kint-20260922 \
  --helm /private/tmp/kint-20260922/darwin-arm64/helm
```

The installer creates new namespaces, network policies, CRDs, CA/JWT pools,
authentication configuration and the locked components. It never enables Marina
intake. It intentionally is not an upgrade/adoption tool: a second invocation
against existing namespaces fails. Inspect partial installs before cleaning them
up; never rerun against unrelated resources by removing collision checks.

## Lifecycle exercise and runtime gate

Create `kint-dummy-model` in `kint-engine` with a random value under key `key`
using a private input stream, then apply [`fixture.json`](fixture.json). Wait
for the template/harness pair's Ready condition. During this preparation phase,
only atelet has a temporary internet-egress exception for digest-checked image
and sandbox downloads. Node image pulls are also setup traffic.

```sh
kubectl --context kind-marina-local apply -f deploy/kubernetes/kagent-trial/fixture.json
kubectl --context kind-marina-local -n ate-system delete networkpolicy kint-preparation-downloads
kubectl --context kind-marina-local -n kint-engine port-forward \
  --address 127.0.0.1 service/kint-engine-controller 18083:8083
```

After the template is Ready and preparation egress is closed, create a synthetic
instance with `kagent-cli --api-url http://127.0.0.1:18083 -n kint-engine -o json
create agent-instance --harness kint-codex --agent-template kint-fixture
--request-id kint-02-lifecycle-20260922`. Save its ID, context, revision and route.
Replace trial controllers, worker, node agent and both PostgreSQL Pods. Stop
RustFS to zero replicas, wait for termination, then restore one replica to avoid
concurrent writers. Reconnect the port-forward and replay the same key: the same
instance must return. Inspect and administratively resume only this synthetic
Substrate actor, then suspend it. This proves snapshot restoration without a
model call; it does not advertise Marina pause/resume support.

Repeat network/RBAC probes, inspect PVC identities, measure kubelet statistics
and node-cache disk use, and confirm no temporary preparation policy remains.
Do not mark KINT-02 complete from Ready Pods alone.

## Teardown

Suspend/delete synthetic actors first. Delete `kint-worker` while the Substrate
controller still runs, then remove the three trial namespaces. Remove only the
inventoried objects bearing `the-platform.dev/trial=kint-02`, deleting the
SandboxConfig before its CRD. Remove the generated trust bundles with signers
`egress-mitm.ate.dev/mitm`, `podidentity.podcert.ate.dev/identity` and
`servicedns.podcert.ate.dev/identity` after their controllers stop. Preserve the
Kubernetes API-serving trust bundle. Never use a cluster-wide unqualified delete
or force-remove data-protection finalizers.

Restore the saved API-server manifest, close task-owned port-forwards and verify
Deck at `https://localhost:8443`. Compare original Deployment and PVC UIDs, image
pins and intake arguments. Preparation images/caches may remain for reuse;
do not prune unrelated Podman/containerd images or kubelet directories. Full
installation upgrade/history rollback remains KINT-10.
