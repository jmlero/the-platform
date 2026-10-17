# Plan: Marina as a pluggable agent and application platform

- Date: 2026-10-17
- State: direction accepted in
  [decision 0032](../decisions/0032-agent-and-application-platform.md); nothing
  is built. This work comes before FLOW-01 (operator decision, 2026-10-17); the
  phases can run in any order.
- Backlog entry: PLAT-00–17 in [backlog](../backlog.md#the-platform).

## Goal

Marina becomes the platform on Kubernetes for agents and applications. One software
factory (Deck plus the repository workflow) runs on top as one workload, and
the platform runs and deploys apps directly.
The platform supplies gateway, tools, observability, evaluation, identity,
secrets, policy, app runtime and declarative configuration. Each capability
lives in a slot that can be filled by different components, so the platform also
works as a lab: swap a component, run the same work, compare the results.

## Rules for every task

1. **Marina stays the governance glue.** Leases, fences, budgets, frozen inputs,
   evidence gates and restart recovery keep working unchanged. No new component
   can widen authority or budgets, or complete work on its own.
2. **Swap at open protocols.** A2A for agents, MCP for tools, an
   OpenAI-compatible API for models, OTLP for telemetry, Gateway API for traffic,
   AGENTS.md for instructions. Do not write one adapter per product when a
   protocol exists.
3. **Any open source; CNCF projects first**, then Agentic AI Foundation (AAIF)
   and other Linux Foundation projects, then other open source such as LangChain
   and Langfuse. Self-hosted only; no SaaS control planes (for example LangSmith).
4. **Everything is declarative and in Git.** Argo CD applies Kustomize overlays
   for our own services and Helm charts for third-party components. No manual
   `kubectl apply` in the documented path. Secret values never enter Git.
5. **CRDs only where they add value; no custom operator unless needed.**
   Third-party components bring their own CRDs and controllers (Argo CD
   `Application`, Knative `Service`). The slot manifest (PLAT-03) is a file,
   because it is frozen per attempt and needs no live status. `Experiment`
   (PLAT-12) is a CRD watched by Marina itself, because live per-run status in
   `kubectl` and Argo CD adds value there.
6. **Pinned and offline at runtime.** Every component is pinned by version and
   image digest. Downloads happen at setup; agent egress stays gateway-mediated.
7. **Fits the laptop.** Each phase measures memory and CPU on local Kind and
   keeps optional components switchable off. GCP use stays within the existing
   EUR 10/month budget.
8. **Experiments never land on real main.** Evaluation runs use scratch
   repositories or branches with landing disabled.

## Component choices

| Slot | First choice | Alternative | Foundation and maturity |
| --- | --- | --- | --- |
| Config and delivery | Argo CD + Kustomize + Helm | Flux | CNCF graduated (Argo, Flux, Helm); Kustomize is a Kubernetes SIG project |
| AI gateway (models, MCP, A2A) | agentgateway | Marina's current gateway alone | AAIF |
| Traffic gateway (ingress, Gateway API) | kgateway | Envoy Gateway, Kourier for Knative only | CNCF sandbox (kgateway); Envoy graduated |
| Tools | MCP servers behind agentgateway; kagent `RemoteMCPServer` for kagent agents | Marina's built-in read tools | AAIF (MCP) |
| Telemetry pipeline | OpenTelemetry Collector and SDKs | none | CNCF graduated (2026) |
| Agent traces | Jaeger, with OpenTelemetry GenAI spans | Langfuse (postponed, PLAT-10) | Jaeger graduated |
| Metrics, traces and dashboards for apps | Prometheus, Jaeger, Perses | Grafana (not CNCF) | Prometheus and Jaeger graduated; Perses sandbox |
| Evaluation | Deterministic checks, results kept by Marina | agentevals, LangChain openevals/agentevals, Langfuse later, Kubeflow pipelines | No CNCF evaluation project exists; see PLAT-12 |
| Secrets | External Secrets Operator + OpenBao | Kubernetes Secrets only | ESO is CNCF sandbox; OpenBao is a Linux Foundation project (no CNCF secret store exists) |
| Admission policy | Kyverno (confirmed) | OPA Gatekeeper | CNCF graduated (both) |
| Workload identity | SPIFFE/SPIRE | Kubernetes ServiceAccount tokens | CNCF graduated |
| Fine-grained authorization | OpenFGA | Kyverno or OPA rules | CNCF incubating |
| Human sign-in | Keycloak | none (local opens without sign-in) | CNCF incubating |
| App runtime | Knative Serving (confirmed) | plain Deployments | CNCF graduated |

**agentgateway and kgateway: both.** They fill different slots. agentgateway is
the AI data plane: model routing, MCP federation and A2A traffic. kgateway is a
Kubernetes Gateway API controller for ingress to Deck and apps, and since v2.1 it
can also manage agentgateway through Gateway API resources. Start with standalone
agentgateway; add kgateway for ingress; let kgateway manage agentgateway only if
it keeps the configuration simpler. kagent's Substrate egress already runs an
agentgateway configuration, so the binary is already in the local footprint.

**OpenFGA is authorization, not identity.** SPIFFE/SPIRE answers "which workload
is this?", Keycloak answers "which human is this?", and OpenFGA answers "may this
agent call this tool in this project?". Deck stays the authority for task
authorization; OpenFGA must not become a second source of truth for it.

## Tasks

Order follows dependencies. Each task stores redacted evidence under
`docs/validation/` and updates [status](../status.md) when it ships.

### Phase 0: direction

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-00 | Decision record and VISION/ARCHITECTURE update: Marina is the agent and app platform, the software factory is one workload, slots and protocols are the swap points. Done in [decision 0032](../decisions/0032-agent-and-application-platform.md). | Direction recorded; docs links pass. |
| PLAT-01 | Footprint baseline of the current local installation and a resource budget per phase. | Measured CPU, memory and storage recorded for local Kind. |

### Phase 1: declarative configuration

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-02 | Argo CD on local Kind manages Deck, Marina and kagent from a Git path: Kustomize overlays for our services, Helm charts for kagent and later components, one Argo CD Application per slot. Private values come from PLAT-04, not Git. | Fresh install and upgrade from Git only; `make check-installation` passes; Argo CD shows all apps synced and healthy; restart and recovery checks still pass. |
| PLAT-03 | Later, by operator decision (2026-10-17): with one stack there is nothing to swap yet. Trigger: PLAT-12. Platform manifest `platform/v1`: one versioned YAML, mounted from a ConfigMap, that names the component in each slot (gateway, tools, telemetry, evaluation). Marina validates it offline, fails closed on unknown fields, and freezes the digest in each attempt like profiles today. | `marina check-config` covers it; changing the manifest never changes active work. |

### Phase 2: secrets and policy

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-04 | External Secrets Operator with OpenBao provides Git, model and service credentials as the files Marina and Deck already read. | No secret value in Git, ConfigMaps, logs or evidence; a rotation rehearsal (with OPS-04) works without restart where files reload. |
| PLAT-05 | Kyverno policies, audit mode first, then enforce: digest-pinned images, no privileged Pods, resource limits, required ownership labels, NetworkPolicy present in agent and verifier namespaces. | Policy reports clean for the installation; a violating test Pod is rejected in enforce mode. |

### Phase 3: gateway and MCP

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-06 | agentgateway as the model routing layer behind Marina's gateway: actor -> Marina (lease, fence, budget, usage) -> agentgateway (provider routing, retries, telemetry) -> OpenRouter. The provider key moves to agentgateway only. Selected by the model endpoint in the existing workflow configuration. | Repository and chat work pass with and without agentgateway; budget, cancellation and lost-response cases from [0023](../decisions/0023-focus-kagent-cutover.md) still pass; no direct provider route from actors. |
| PLAT-07 | kgateway as the Gateway API controller for ingress to Deck (`https://localhost:8443`) and apps. Then decide, with evidence, whether kgateway also manages agentgateway. | Deck reachable through kgateway; written decision on the second point. |
| PLAT-08 | MCP servers as tools: in-cluster MCP servers federated by agentgateway. Each profile lists allowed tools; Marina freezes the list in the attempt and counts tool calls against the budget. Start with read-only tools, including the existing repository read tools exposed over MCP. | An agent uses an allowed MCP tool; a disallowed tool is rejected at the gateway; tool output stays untrusted data; cancellation during a tool call still works. |

### Phase 4: observability

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-09 | OpenTelemetry Collector as the single pipeline. Marina emits traces and metrics with the OTel Go SDK using GenAI conventions, tagged with project, task, run and attempt IDs. kagent and agentgateway export to the same collector. The collector drops secrets and credential headers. | One task shows one connected trace from claim to landing; a secret scan of exported data is clean. |
| PLAT-10 | Later, by operator decision (2026-10-17): Langfuse needs Postgres, ClickHouse, Redis and object storage, too heavy for the laptop now. When added: agent traces, prompts, costs and scores over OTLP from the same collector. Until then agent traces go to Jaeger (PLAT-11) and experiment results stay in Marina (PLAT-12). | Trigger: an independent cluster (LATER-07) or measured spare capacity on the laptop. |
| PLAT-11 | Agent, app and platform observability: Prometheus metrics, Jaeger traces (including agent GenAI spans) and Perses dashboards. Apps get OTel auto-instrumentation through the OpenTelemetry Operator. | Dashboards for Deck, Marina, gateways and one sample app; one repository attempt and one app request trace visible end to end, with model, tokens and cost matching Marina's usage records. |

### Phase 5: evaluation

No CNCF project covers agent evaluation today. The closest open options are
agentevals (Apache-2.0, scores agents from OpenTelemetry traces), LangChain
openevals/agentevals, and Langfuse datasets and scores (postponed with PLAT-10). Kubeflow (CNCF
graduated) can run evaluation pipelines but is heavy for a laptop.

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-12 | Later, by operator decision (2026-10-17): first confirm one working stack. Trigger: PLAT-01–02, PLAT-04–09, PLAT-11 and PLAT-17 accepted. Experiments: an `Experiment` custom resource, watched by Marina, names a fixed task set on a scratch repository and two or more variants (model, gateway, engine settings). Marina runs each variant with landing disabled and records the real check result as the main score, plus review verdict, cost, tokens, time and attempts, in Marina's journal. The resource status shows progress per variant, and Marina exports a comparison report as an artifact. | Two model variants on the same tasks produce a comparable result table; nothing lands on a real main; budgets apply per run. |
| PLAT-13 | Later, with PLAT-12. Trace-based scoring with agentevals or openevals over PLAT-09 traces (trajectory and tool-use checks), as a supplementary score next to deterministic evidence. | Scores appear next to PLAT-12 results; they never complete or block a task. |

### Phase 6: identity and authorization

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-14 | Later, by operator decision. SPIFFE/SPIRE workload identity for Deck, Marina, gateways and verifier Jobs; mTLS between services where the component supports SVIDs, replacing the private installation CA there. | Services authenticate with SVIDs; existing scoped service credentials and fencing still apply. |
| PLAT-15 | Later, by operator decision (2026-10-17): the per-profile tool allowlist of PLAT-08 covers one stack. OpenFGA for tool and agent access: relations of project, agent and MCP tool, checked by the gateway on each tool call, keyed by Marina's attempt identity until PLAT-14 exists. | A tool allowed for one project is denied for another; Deck task authorization is unchanged. |
| PLAT-16 | Keycloak sign-in for Deck. Later: the local install opens without sign-in by operator decision. | Trigger: LATER-07 or another user. |

### Phase 7: apps

| ID | Outcome | Acceptance |
| --- | --- | --- |
| PLAT-17 | Knative Serving for apps and agent-backed services, with scale to zero, ingress through kgateway via `net-gateway-api` (Kourier as fallback; kgateway is not in Knative's tested list). The platform deploys apps directly: Argo CD syncs each app from its own repository's manifests on main. Marina records the deployed commit and health as evidence; the repository workflow itself still only lands commits. | A sample app built by the factory lands on main, Argo CD deploys it, Knative serves it, Marina records the deployed commit and health, and PLAT-11 shows its traces and metrics. |

## Not in this plan

- A second agent engine (LangGraph, Dapr Agents, goose) and sandbox swaps
  (Agent Sandbox, Kata). Add them as a follow-up once PLAT-03 and PLAT-12 exist,
  because then they can be compared.
- Local model serving (vLLM, llm-d, KServe): needs GPUs outside the budget.
- High availability, multiple users and backups (OPS-01–02 keep their triggers).

## Risks

1. **Laptop footprint.** kagent already runs Postgres and Substrate; Langfuse is
   postponed for this reason. PLAT-01 and the off switch in PLAT-03 contain the
   rest.
2. **Governance bypass.** A new gateway or MCP path could skip fence or budget
   checks. PLAT-06 and PLAT-08 keep Marina in the request path and rerun the
   decision 0023 failure cases.
3. **Telemetry leaks.** Prompts and code end up in traces. PLAT-09 filters
   secrets; all backends stay in-cluster.
4. **Focus.** This plan delays FLOW-01, by operator decision.
