# Marina on a local Kind cluster

One script installs the whole Marina platform on your Mac (or a Linux machine)
in a fresh Kind cluster. No Deck and no cloud account are needed. Without any
keys you get a working platform with a fake model; add an OpenRouter key for
real answers and a GitHub repository for code changes.

What is accepted and what is still open is in [status](../../../docs/status.md).

## What you get

| Component | What it does | Where |
| --- | --- | --- |
| Marina | The platform API (`marina.dev/v1`): Projects, Workloads, Runs, Turns, Decisions. Budgets, leases, evidence. | `marina-system`, https://marina.localhost:9443 |
| kagent | The agent engine. Runs Marina's agents and your own. | `kint-engine`, https://kagent.localhost:9443 |
| Agent Substrate | gVisor sandboxes for every agent, with an egress proxy that injects keys. | `ate-system` |
| agentgateway | Holds the model key and serves the OpenAI Responses API. | `platform-gateway` |
| Fake model | A fixed reply, so everything works without a model account. | `platform-gateway` |
| Kyverno | Rejects unpinned images, privileged Pods, Pods without memory limits or NetworkPolicy. | `kyverno` |
| OpenTelemetry Collector + Jaeger | Traces from Marina and agentgateway. | `platform-observability`, https://jaeger.localhost:9443 |
| kgateway | Ingress on `https://*.localhost:9443`. | `platform-ingress` |
| Calico | Enforces every NetworkPolicy. | `kube-system` |
| Local registry | Images you build, pulled by the node and the sandboxes. | container `marina-registry`, `localhost:5001` |

The [platform components guide](../platform/README.md) explains each shared
component in more depth.

## Requirements

- An ARM64 Mac (Apple silicon) or ARM64 Linux. The engine images are pinned for ARM64 only.
- Podman 6 (tested) or Docker (supported by Kind, not yet tested here; see [Docker](#docker)).
  The container VM needs **8 GB RAM** and **30 GB free disk** (60 GB with GitHub mode).
- `kind` 0.33, `kubectl`, `helm` 3, `openssl`, `ruby`, `python3`, `curl`.

On a Mac with Podman, use a rootful machine; the gVisor sandboxes need privileged containers:

```sh
podman machine init --cpus 6 --memory 8192 --disk-size 100 --rootful   # once
podman machine start
export CONTAINER_CONNECTION=podman-machine-default-root   # only if your machine is not rootful by default
```

## Quick start

```sh
python3 scripts/marina-local.py up       # 10 to 20 minutes the first time (downloads), about 5 after
python3 scripts/marina-local.py verify   # about 1 minute
```

`verify` checks the ingress, runs a Marina Turn through kagent in a sandbox,
builds and runs [your own agent](#deploy-your-own-agent), and confirms that
Kyverno rejects an unpinned image. Each line says PASS or FAIL.

Your private files (CA, keys, generated Marina config) are in
`~/.local/share/the-platform/clusters/marina`. To open the web pages without
warnings, trust the generated CA:

```sh
security add-trusted-cert -r trustRoot -p ssl -k ~/Library/Keychains/login.keychain-db \
  ~/.local/share/the-platform/clusters/marina/ca.crt
```

`up` is safe to run again; it keeps the private files and updates everything
else. It never changes your current kubectl context; use `--context kind-marina`.

## What `up` does

1. Creates the private CA, service certificates and random keys.
2. Creates the Kind cluster from [kind.yaml](kind.yaml): one node, Calico,
   the beta certificate API that Substrate needs, ports 9443 (ingress) and
   5001 (registry) on 127.0.0.1 only.
3. Starts the registry inside the node's network and builds and pushes the
   Marina and Marina chat-agent images.
4. Installs kagent and Substrate from the pinned [lock](../kagent-trial/lock.json)
   (chart and binary checksums are checked), lets the sandboxes pull from the
   registry, and makes the egress proxy trust the private CA.
5. Installs Kyverno (audit), the collector and Jaeger, agentgateway
   ([overlay](gateway/kustomization.yaml)), the [fake model](fake-model/fake-model.yaml),
   Gateway API, kgateway and the [ingress](ingress/kustomization.yaml).
6. Installs Marina with its [Helm chart](../../../marina/deploy/helm/marina/README.md)
   and [these values](marina-values.yaml), and registers Marina's agents with kagent.
7. Creates the `demo` namespace with Project `hello`, Workload `chat` and a
   ServiceAccount `operator` bound to `marina-admin`.
8. Turns on Kyverno enforcement for the platform's namespaces and closes the
   setup download route for the sandboxes.

## Use Marina

Marina's API is Kubernetes-native. With `kubectl`:

```sh
kubectl --context kind-marina -n demo apply -f - <<'EOF'
apiVersion: marina.dev/v1
kind: Turn
metadata: {name: hi, namespace: demo}
spec:
  project: hello
  workload: chat
  thread:
    id: thr_hi
    messages: [{id: msg_1, role: human, body: "Say hello."}]
  budget: {timeMinutes: 5, maxTokens: 20000}
EOF
kubectl --context kind-marina -n demo get turn hi -o jsonpath='{.status.phase}: {.status.reply.answer}'
```

The same objects over HTTP/JSON, with a Kubernetes token:

```sh
CA=~/.local/share/the-platform/clusters/marina/ca.crt
TOKEN=$(kubectl --context kind-marina -n demo create token operator)
curl --cacert $CA -H "Authorization: Bearer $TOKEN" \
  https://marina.localhost:9443/apis/marina.dev/v1/namespaces/demo/turns/hi/events
```

Without `--github-repo` the Project stays in `Binding`; that is expected,
because chat needs no repository. The contract is in [marina/api](../../../marina/api/README.md).

## Deploy your own agent

An agent is any container that speaks A2A on port 80. [examples/hello-agent](examples/hello-agent)
is a 100-line Go agent that answers each message with one model call. You
register it with two kagent objects: a **Harness** (which image to run, always
in a gVisor sandbox) and an **AgentTemplate** (model and system prompt). See
[agent.yaml](examples/hello-agent/agent.yaml).

```sh
cd deploy/kubernetes/marina-local/examples/hello-agent
podman build -t localhost/hello-agent:v1 .                          # or docker build
podman push --tls-verify=false localhost/hello-agent:v1 localhost:5001/hello-agent:v1   # docker push needs no flag
NODE=$(kubectl --context kind-marina get node marina-control-plane -o jsonpath='{.status.addresses[0].address}')
DIGEST=$(curl -sI -H 'Accept: application/vnd.oci.image.manifest.v1+json' http://localhost:5001/v2/hello-agent/manifests/v1 | awk 'tolower($1)=="docker-content-digest:" {print $2}' | tr -d '\r')
sed "s|REGISTRY_IMAGE|$NODE:5000/hello-agent@$DIGEST|" agent.yaml | kubectl --context kind-marina apply -f -
```

Talk to it with the pinned kagent CLI (downloaded by `up`):

```sh
KAGENT=~/.local/share/the-platform/clusters/marina/downloads/engine/kagent-cli
kubectl --context kind-marina -n kint-engine port-forward svc/kint-engine-controller 18083:8083 &
API="--api-url http://127.0.0.1:18083 --gateway-url http://127.0.0.1:18083 -n kint-engine"
$KAGENT $API create agent-instance --harness hello-agent --agent-template hello-agent
$KAGENT $API invoke --agent-instance <id from above> --task "Say hello"
```

Or open https://kagent.localhost:9443 and chat with it there.

The sandbox never sees a model key: the agent sends a placeholder, and the
egress proxy swaps in the key from the Secret that the ModelConfig names. The
sandbox can reach only the egress proxy; the proxy can reach only Marina, the
fake model and agentgateway.

**Real model for your agent.** Point the ModelConfig at agentgateway and use the
`agents` client key:

```sh
kubectl --context kind-marina -n kint-engine create secret generic hello-agent-model \
  --from-file=key=$HOME/.local/share/the-platform/clusters/marina/agentgateway-agents-key \
  --dry-run=client -o yaml | kubectl --context kind-marina apply -f -
kubectl --context kind-marina -n kint-engine patch modelconfig hello-agent --type=merge -p \
  '{"spec":{"model":"openai/gpt-5-mini","openAI":{"baseUrl":"https://agentgateway.platform-gateway.svc/v1"}}}'
```

Your own agents are not governed by Marina's budgets; their spend is limited
only by the `agents` key, which you can rotate in the `agentgateway-clients` Secret.

## Real model (OpenRouter)

```sh
printf '%s' 'sk-or-...' > ~/.openrouter-key && chmod 600 ~/.openrouter-key
python3 scripts/marina-local.py up --model-key-file ~/.openrouter-key   # optional: --model openai/gpt-5-mini
```

Marina then calls `https://agentgateway.platform-gateway.svc/v1`; only
agentgateway holds the key. Write key files without a trailing newline:
agentgateway compares keys byte for byte.

## GitHub mode: agents change code

Marina's repository workflow: plan, implement with Codex in a sandbox,
independent review by a second model, checks in a clean verification Job,
then a pull request.

```sh
python3 scripts/marina-local.py up --model-key-file ~/.openrouter-key \
  --github-repo you/your-repo --github-token-file ~/.github-token \
  --check "npm test" --allowed-path src --allowed-path tests
```

The token needs contents and pull-request access to that one repository.
`--check` is a command Marina runs in a clean Job to verify each change;
`--allowed-path` names the folders agents may change (the whole repository
is not allowed). This adds the Codex harness, the verification image (about
4 GB) and Workload `change`, and the Project binds to your repository.

Create a Run:

```sh
kubectl --context kind-marina apply -f - <<'EOF'
apiVersion: marina.dev/v1
kind: Run
metadata: {name: run-1, namespace: demo}
spec:
  project: hello
  workload: change
  title: Reject an empty range
  description: clamp(min, max) must reject min > max.
  acceptanceCriteria: A unit test covers min > max and passes.
  budget: {timeMinutes: 30, maxTokens: 150000, maxAttempts: 4, maxCostMinor: 100, currency: USD}
EOF
```

New work waits (`Pending`) until you open intake. Admit just this Run:

```sh
helm --kube-context kind-marina upgrade marina marina/deploy/helm/marina -n marina-system \
  --reuse-values --set intake.enabled=true --set intake.runID=demo.run-1
```

Marina pins the base commit, writes a plan and asks for approval: the Run goes
to `WaitingDecision` and Decision `run-1-plan-1` appears. Answer it through the
API (`POST .../decisions/run-1-plan-1/answer`), or cancel the Run
(`POST .../runs/run-1/cancel`; both need an `Idempotency-Key` header). Set
`intake.enabled=false` again afterwards. See the
[API contract](../../../marina/api/README.md) and the
[workload profiles](../../../marina/docs/workloads/profiles-v1.md).

## Docker

Kind supports Docker as its default provider, and the script works the same way
(`--provider docker`, or it picks Docker when Podman is missing). The registry
is reached on `localhost:5001`, which Docker accepts without TLS. This setup was
tested with Podman only. The open question for Docker Desktop is its Linux
kernel: gVisor runs in `systrap` mode, which should work but has not been tried.

## Day to day

- **After the Mac sleeps**, the VM clock can lag, which breaks leases and
  deadlines. Compare `podman exec marina-control-plane date -u` with `date -u`;
  fix it with `podman machine ssh "sudo date -u -s '$(date -u +%Y-%m-%dT%H:%M:%SZ)'"`.
- **After a VM restart**, start the node, then the registry:
  `podman start marina-control-plane && podman start marina-registry`. The node
  can come back with a new address, which the sandbox policies and image
  addresses use; run `up` again (with the same options) to follow it.
- **Logs**: `kubectl --context kind-marina -n marina-system logs deploy/marina`;
  the agent's own output is in the `kint-worker` Pod in `kint-engine`.
- **Remove everything**: `python3 scripts/marina-local.py down`. Private files stay.
