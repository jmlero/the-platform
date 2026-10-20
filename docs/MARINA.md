# Marina

Marina is a platform for running AI agents safely on Kubernetes. You give it
work, it runs agents in locked sandboxes within budgets you set, and it accepts
a result only with proof: passing checks, an independent review, a record of
what happened. One command installs all of it on your Mac.

A visual tour of the same content: [marina-visual.html](marina-visual.html)
(open it in a browser).

## In one minute

| | |
| --- | --- |
| **What it is** | The control layer for agents. It has its own API (`marina.dev/v1`) that you use with `kubectl` or HTTP. |
| **What you ask it for** | A **Turn**: one chat answer. A **Run**: a code change in a GitHub repository, from plan to pull request. |
| **What it guarantees** | Budgets (time, tokens, cost) per task. Agents never see your keys. Agents only reach what they are allowed to. Work survives restarts. A human approves the plan. |
| **Who uses it** | You (with `kubectl`), the Deck web app, the `platformctl` CLI or your own apps. Marina calls none of them. |

## What is inside

Marina is one layer of a stack of open-source projects. Each one has one job.

| Layer | Project | What it does for you |
| --- | --- | --- |
| Control | **Marina** | The API: Projects, Workloads, Runs, Turns and Decisions. Keeps budgets, leases and evidence in its own journal. |
| Agents | **kagent** | Runs agents: Marina's own and yours. Has a web UI to chat with them. |
| Sandboxes | **Agent Substrate** (gVisor) | Every agent runs in a gVisor sandbox, isolated from the machine. Its egress proxy adds keys to outgoing calls, so the agent never holds them. |
| Models | **agentgateway** | The only place the model key lives. Speaks the OpenAI Responses API, here in front of OpenRouter. |
| Models | **Fake model** | A fixed answer, so everything works without any model account. |
| Rules | **Kyverno** | Refuses unsafe Pods: images without a pinned digest, privileged Pods, Pods without limits or network policy. |
| Rules | **Calico** | Enforces every network policy: each part talks only to what it needs. |
| Insight | **OpenTelemetry Collector + Jaeger** | Collects traces from Marina and agentgateway and shows them. |
| Entry | **kgateway** | The front door: all web pages on `https://*.localhost:9443`. |
| Base | **Kind** + local registry | Kubernetes in a container on your Mac, plus a registry for images you build. |

The engine pieces are pinned to exact versions in
[lock.json](../deploy/kubernetes/kagent-trial/lock.json). More on the shared
components: [platform components](../deploy/kubernetes/platform/README.md).

## Install (quick start)

You need an Apple silicon Mac, [Podman](https://podman.io) (Docker should work
but is untested), and `kind`, `kubectl`, `helm`, `openssl`, `ruby`, `python3`.

```sh
podman machine init --cpus 6 --memory 8192 --disk-size 100 --rootful   # once
podman machine start
python3 scripts/marina-local.py up       # 10 to 20 min the first time, about 5 after
python3 scripts/marina-local.py verify   # about 1 min, every line should say PASS
```

That's it. Without keys you get the fake model. Optional extras:

| Want | Add to `up` |
| --- | --- |
| Real model answers (OpenRouter) | `--model-key-file ~/.openrouter-key` |
| Agents that change code in your repo | `--github-repo you/repo --github-token-file ~/.github-token --check "npm test" --allowed-path src` |

To remove everything: `python3 scripts/marina-local.py down` (your keys and CA
are kept in `~/.local/share/the-platform/clusters/marina`).

## Dashboards and access

Trust the local CA once, so the browser shows no warnings:

```sh
security add-trusted-cert -r trustRoot -p ssl -k ~/Library/Keychains/login.keychain-db \
  ~/.local/share/the-platform/clusters/marina/ca.crt
```

| Open | What you see |
| --- | --- |
| https://kagent.localhost:9443 | kagent UI: your agents, chat with them. |
| https://jaeger.localhost:9443 | Jaeger: traces of each request through Marina and the model gateway. |
| https://marina.localhost:9443 | Marina's API (JSON, no web page). Needs a Kubernetes token; see below. |
| `kubectl --context kind-marina ...` | Everything, including Marina's objects: `kubectl get projects,workloads,runs,turns,decisions -A`. |

## Use it

Ask the chat agent a question with a **Turn**:

```sh
kubectl --context kind-marina -n demo apply -f - <<'EOF'
apiVersion: marina.dev/v1
kind: Turn
metadata: {name: hi, namespace: demo}
spec:
  project: hello
  workload: chat
  thread: {id: thr_hi, messages: [{id: msg_1, role: human, body: "Say hello."}]}
  budget: {timeMinutes: 5, maxTokens: 20000}
EOF
kubectl --context kind-marina -n demo get turn hi -o jsonpath='{.status.reply.answer}'
```

The same over HTTP:

```sh
TOKEN=$(kubectl --context kind-marina -n demo create token operator)
curl --cacert ~/.local/share/the-platform/clusters/marina/ca.crt -H "Authorization: Bearer $TOKEN" \
  https://marina.localhost:9443/apis/marina.dev/v1/namespaces/demo/turns/hi
```

The words you will see:

| Object | Meaning |
| --- | --- |
| **Project** | One product, bound to one GitHub repository. |
| **Workload** | A kind of work for a Project, with its model, limits and rules (for example `chat` or `change`). |
| **Turn** | One chat question and its answer. |
| **Run** | One code change: plan, implement, review, check, pull request. |
| **Decision** | A question for a human, such as "approve this plan?". |

Code changes (Runs), intake and approvals are explained step by step in the
[install guide](../deploy/kubernetes/marina-local/README.md#github-mode-agents-change-code).

## Deploy your own agent

Any container that speaks the A2A protocol on port 80 can be an agent. It runs
in the same gVisor sandbox as Marina's agents.

1. **Write it.** [examples/hello-agent](../deploy/kubernetes/marina-local/examples/hello-agent)
   is a 100-line Go agent: it reads its model settings from kagent and makes one
   model call per message.
2. **Build and push** to the local registry:
   `podman build -t localhost/hello-agent:v1 .` then
   `podman push --tls-verify=false localhost/hello-agent:v1 localhost:5001/hello-agent:v1`.
3. **Register it** with two kagent objects in [agent.yaml](../deploy/kubernetes/marina-local/examples/hello-agent/agent.yaml):
   a **Harness** (which image to run) and an **AgentTemplate** (model and
   instructions). Apply them with `kubectl`.
4. **Talk to it** in the kagent UI, or with the `kagent` CLI.

The agent never holds a model key: it sends a placeholder, and the sandbox's
egress proxy swaps in the real key. `verify` does all four steps for you. The
exact commands are in the
[install guide](../deploy/kubernetes/marina-local/README.md#deploy-your-own-agent).

## Good to know

- **After the Mac sleeps**, the VM clock can lag. Check with
  `podman exec marina-control-plane date -u`.
- **After a restart**, start the node and the registry
  (`podman start marina-control-plane marina-registry`), then run `up` again
  with the same options: the node may have a new address.
- **What is proven and what is not:** see [status](status.md). The full
  install guide: [Marina on a local Kind cluster](../deploy/kubernetes/marina-local/README.md).
