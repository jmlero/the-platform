# Decision 0032: Marina becomes a pluggable agent and application platform

- Date: 2026-10-17
- Status: accepted product direction; not implemented (PLAT-00–17 in the backlog)
- Scope: product direction for Marina and the installation. Supersedes the
  "the platform does not deploy" part of
  [decision 0022](0022-autonomous-software-factory.md) and the retirement reason
  of LATER-03. Reopens the single-engine part of
  [decision 0017](0017-kagent-core-engine.md).

## Context

Marina was built as the execution side of one software factory: one engine, one
model gateway, one Git provider, one verifier, each wired in directly. The
operator restated the goal: Marina is the platform on Kubernetes for agents and
applications, and software factories run on top of it. The platform should cover
what agents and apps need (runtime, sandboxing, gateway, tools, observability,
evaluation, identity, secrets, policy, configuration) and serve as a lab where
components can be swapped, tested and compared.

## Decision

The operator decided:

1. **Marina is the agent and application platform.** Each capability is a slot
   that different components can fill. Swapping a component and comparing
   results is a first-class use, not a side effect.
2. **The software factory is one workload on the platform.** Deck and the
   repository workflow form one factory. Its landing gates do not change.
3. **The platform runs and deploys apps directly.** Apps run on the platform,
   deployed declaratively from Git with Argo CD and run on Knative Serving. CI stays
   on GitHub. The repository workflow itself still lands commits and does not
   deploy; deployment follows from Git.
4. **Any open-source component is allowed; CNCF projects come first** when
   choosing the tech stack, then other foundation projects (Agentic AI
   Foundation, Linux Foundation), then other open source. Self-hosted only; no
   SaaS control planes.
5. **Declarative configuration.** Argo CD, Kustomize and Helm, from Git. Add a
   custom resource only where it adds value over a versioned manifest file (for
   example live status for experiments). No custom operator unless one is
   needed.
6. **Kyverno is the admission policy engine.**
7. **Workload identity (SPIFFE/SPIRE) comes later.**
8. **kagent stays the default engine**; other engines may be added as options in
   the runtime slot.

Marina's governance stays the glue: leases, fences, budgets, frozen inputs,
evidence gates and restart recovery apply to every component. Components are
swapped at open protocols (A2A, MCP, OpenAI-compatible model APIs, OTLP, Gateway
API) rather than through one adapter per product. No component can widen
authority or budgets or complete work by itself.

## Consequences

- VISION, ARCHITECTURE and Marina's agent guidance describe the platform as the
  target. [Status](../status.md) is unchanged until capabilities ship.
- The tasks are in the [platform components plan](../plans/platform-components.md).
  The platform work comes before FLOW-01; its phases can run in any order.
- The single-process, single-SQLite rule still applies to Deck and Marina
  themselves. Third-party components bring their own stores, so the laptop
  footprint must be measured and optional components can be switched off.
