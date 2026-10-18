# Platform components

Shared components from the [platform components plan](../../../docs/plans/platform-components.md).
Everything here is plain Kubernetes (Helm charts, Kustomize and Gateway API),
so it runs on any conformant cluster: local Kind, GKE or EKS. Environment
details such as host ports stay in the environment's own overlay. What is
accepted is in [status](../../../docs/status.md).

Pin every chart version and image digest. Downloads happen during setup only.
Commands below use the local profile's explicit context; use your own context
on another cluster.

```sh
k() { kubectl --context kind-marina-local "$@"; }
HELM=~/.local/share/the-platform/tools/helm-v3.22.0/helm   # verified Helm 3.22.0
```

## Kyverno admission policy (PLAT-05)

Kyverno 1.19.1 from chart `kyverno/kyverno` 3.9.1, with digest-pinned images in
[kyverno/values.yaml](kyverno/values.yaml). Only the admission and reports
controllers run (about 125 MiB on local Kind). The policies use the CEL
`ValidatingPolicy` API; the legacy `ClusterPolicy` API is deprecated.

```sh
$HELM repo add kyverno https://kyverno.github.io/kyverno/
$HELM --kube-context kind-marina-local upgrade --install kyverno kyverno/kyverno \
  --version 3.9.1 -n kyverno --create-namespace \
  -f deploy/kubernetes/platform/kyverno/values.yaml --wait
k apply -k deploy/kubernetes/platform/kyverno/policies/audit
# Read the reports; enforce only when our namespaces are clean.
k get policyreports -A
k apply -k deploy/kubernetes/platform/kyverno/policies/enforce
```

The [workload baseline](kyverno/policies/base/workload-baseline.yaml) requires
for every Pod container and init container:

- an image pinned by digest;
- no privileged mode (Substrate's `atelet` in `ate-system` is exempt, because it
  runs the gVisor sandboxes);
- a memory limit;
- an `app.kubernetes.io/name` or `app` label.

| Overlay | Namespaces | Action | If Kyverno is unavailable |
| --- | --- | --- | --- |
| [audit](kyverno/policies/audit) | `ate-system`, `kgateway-system`, `kint-engine`, `kyverno`, `podcertificate-controller-system` | Report | Admission continues |
| [enforce](kyverno/policies/enforce) | `marina-system`, `marina-workers`, `platform-gateway`, `platform-ingress`, `platform-observability` | Deny | New Pods there wait until Kyverno is back |

The enforce overlay also requires a NetworkPolicy in the namespace before a Pod
can start. Policies also check Deployments, ReplicaSets, StatefulSets and Jobs
through Kyverno's automatic pod-controller rules, so a bad template is rejected
when it is applied.

Third-party findings stay visible in the audit reports instead of being
exempted. When adding a namespace, read its audit reports before moving it to
the enforce overlay.

Remove: `k delete -k deploy/kubernetes/platform/kyverno/policies`, then
`$HELM --kube-context kind-marina-local uninstall kyverno -n kyverno`.

## Traces: OpenTelemetry Collector and Jaeger (PLAT-09)

[observability](observability) runs one OpenTelemetry Collector 0.161.0
(contrib) and Jaeger 2.21.0 with in-memory storage, in `platform-observability`.
Every component sends OTLP to `otel-collector.platform-observability.svc`
(gRPC 4317, HTTP 4318). The collector deletes credential-like attribute keys and
masks bearer tokens and OpenRouter or GitHub keys before Jaeger stores anything.
Jaeger keeps at most 5,000 traces and loses them on restart.

```sh
k apply -k deploy/kubernetes/platform/observability
k -n platform-observability port-forward svc/jaeger 26686:16686   # UI at http://localhost:26686
```

Marina exports traces when `OTEL_EXPORTER_OTLP_ENDPOINT` is set; the local
profile sets it in [marina.yaml](../local-podman/marina.yaml). Each task is one
trace whose ID derives from the execution ID, so steps before and after a
restart join it: one span per workflow step, one per model call with
`gen_ai.*` model and token attributes, and a root span when the task ends.
Spans carry IDs, timing and usage only, never prompts, code or credentials.

## Model gateway: agentgateway (PLAT-06)

[gateway](gateway) runs agentgateway 1.6.0 standalone in `platform-gateway`. It
holds the OpenRouter key and serves the OpenAI Responses API through its
`openrouter` provider. Callers need a client key (`apiKey` in strict mode) and
TLS from the installation CA. Marina's gateway stays in front: it still checks
the lease, fence and budget on every call, then forwards to
`https://agentgateway.platform-gateway.svc/v1`. agentgateway sends its own
traces to the collector, with model and status but no prompt text.

The secrets come from private files, never Git:

```sh
I=~/.local/share/the-platform/marina-local/installation   # private installation
# Certificate for agentgateway.platform-gateway.svc, signed by the installation CA.
k apply -f deploy/kubernetes/platform/gateway/namespace.yaml
k -n platform-gateway create secret tls agentgateway-tls --cert=$I/agentgateway.crt --key=$I/agentgateway.key
k -n platform-gateway create secret generic agentgateway-provider --from-file=api-key=$I/model-token
k -n platform-gateway create secret generic agentgateway-clients --from-file=marina=$I/agentgateway-client-key
k apply -k deploy/kubernetes/platform/gateway
```

Then set `model.endpoint` and `reviewer_model.endpoint` in the private
`workflow.json` to `https://agentgateway.platform-gateway.svc/v1`, their
`token_file` to `/run/marina-secrets/agentgateway-client-key`, add that file to
the `marina-credentials` secret, apply the private installation and restart
Marina. Check offline first with `marina check-config` and
`scripts/check-installation.py`.

Only Marina may connect. agentgateway may reach DNS, the collector, Jaeger's
MCP endpoint and HTTPS outside the cluster; narrow the last rule to the
provider's addresses in an environment overlay if they are stable.

## MCP tools (PLAT-08)

The same agentgateway serves `/mcp` and federates MCP servers. The first target
is Jaeger's built-in, read-only trace tools. agentgateway allows only
`get_services`, `search_traces`, `get_trace_topology` and `get_trace_errors`
and hides the rest from `tools/list`.

Agents never call MCP servers themselves. Marina's chat gateway opens the MCP
session, offers the allowed tools next to the repository read tools, and runs
each call under the turn's authority check, call and round limits and 64 KiB
result budget. Enable it in the private `workflow.json`, only on a Marina image
that includes PLAT-08 (older images reject the unknown field):

```json
"conversation": {
  "mcp": {
    "endpoint": "https://agentgateway.platform-gateway.svc/mcp",
    "token_file": "/run/marina-secrets/agentgateway-client-key",
    "tools": ["get_services", "search_traces", "get_trace_topology", "get_trace_errors"]
  }
}
```

## Ingress: kgateway (PLAT-07)

kgateway 2.4.5 is the Gateway API controller, with Envoy as its proxy.
kgateway no longer manages agentgateway (since 2.2), so the two stay separate:
kgateway for ingress, agentgateway for model and tool traffic. Install the
Gateway API 1.6.1 standard CRDs, then the charts:

```sh
k apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.1/standard-install.yaml
# sha256 24d931f22abd8e40c973264319ead7cfa09d0fb7716b7ab1ee2ff174cb063a73
$HELM --kube-context kind-marina-local upgrade --install kgateway-crds \
  oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds --version v2.4.5 -n kgateway-system --create-namespace --wait
$HELM --kube-context kind-marina-local upgrade --install kgateway \
  oci://cr.kgateway.dev/kgateway-dev/charts/kgateway --version v2.4.5 -n kgateway-system \
  -f deploy/kubernetes/platform/ingress/values.yaml --wait
k apply -k deploy/kubernetes/platform/ingress-local-kind   # other clusters: ingress
```

[ingress](ingress) defines the `platform` Gateway in `platform-ingress` with
one TLS listener on 8443 in passthrough mode, so Deck keeps its own TLS. A
`TLSRoute` sends SNI `localhost` to Deck; an unknown host name is refused.
GatewayParameters pin the Envoy image by digest and set limits, so the proxy
passes the Kyverno policies. kgateway labels the proxy Pods with the Gateway
name, so its NetworkPolicies select `gateway.networking.k8s.io/gateway-name`.

On local Kind, [ingress-local-kind](ingress-local-kind) gives the Gateway node
port 30443, which `kind.yaml` publishes as `https://localhost:8443`. Deck's own
Service is ClusterIP. On EKS or GKE, use a LoadBalancer Service and the real
host name in an overlay instead.
