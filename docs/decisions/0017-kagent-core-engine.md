# Decision 0017: Make kagent Marina's core agent engine

- Date: 2026-09-22
- Status: accepted implementation direction; integration, rollout and removal pending; cutover gates narrowed and the diagnostic workload retired by [decision 0023](0023-focus-kagent-cutover.md)
- Scope: Marina engine ownership and KINT-01–15

## Decision

The operator accepted planning the integration of kagent and removal of redundant
Marina code after reviewing the [adoption analysis](https://github.com/jmlero/the-platform/blob/744204a/docs/kagent-adoption.md).
Make kagent the target core agent engine, with Marina retaining the governed
workflow and interface to Deck. Prefer upstream capabilities wherever they meet
the required behavior. Delivery includes deleting replaced implementation, not
just installing another service.

Deck retains human intent, authorization, decisions and visible results. Marina
retains Deck leases/fences, immutable workflow context, bounded authority/budgets,
source publication, independent verification, evidence admission and durable
cross-system reconciliation. kagent owns harness execution, engine interactions
and runtime lifecycle. Integrate through public APIs and keep separate stores.

The [implementation and removal plan](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md) defines
dependencies, file-level removal candidates, acceptance, cutover and rollback.
Its stable KINT tasks are tracked in [the backlog](../backlog.md). Reuse native Codex;
preserve the chosen LangChain workload through BYO initially. A deterministic
verification Job may remain without constituting a second agent engine.

## Relationship to earlier decisions

- [0010](0010-initial-installation-services.md): the current pilot configuration
  remains supported. The planned engine adds dependencies such as PostgreSQL and
  Substrate; exact installation needs must pass local feasibility and existing
  spending/authority constraints before rollout. Deck/Marina stores remain separate.
- [0015](0015-marina-agent-platform.md): supersedes the future direction of owning
  generic agent hosting in Marina. Its completed two-workload proof remains valid,
  and LangChain remains the selected diagnostic harness until explicitly replaced.
- [0016](0016-workload-invocation-and-context-tool.md): preserve governed invocation,
  exact profiles, immutable context, usage and inspection semantics while replacing
  their execution mechanism. An engine endpoint cannot bypass Deck authorization.
- [0009](0009-asynchronous-agent-work.md): conversation work should reuse kagent's
  engine facilities while Deck retains the product interaction and ledger.

## Delivery boundary

This decision establishes direction, not implemented capability or successful
acceptance. No code, schema or runtime is removed by this documentation change.
Start with the pinned candidate in the analysis, validate the public APIs and
local dependencies, prove both workflows and failure behavior, drain existing
work, cut over new profiles, then remove the obsolete paths.

Do not silently weaken evidence, fencing, budgets, termination or source rules
to fit the dependency. A failed gate blocks cutover/removal and needs a concrete
remedy or an evidence-backed reconsideration. Ordinary work within the accepted
direction does not require repeatedly deciding whether to adopt.

No production deployment, new spending authority, external repository mutation,
merge authority or additional supported workflow is granted by this plan.
Temporary dual-engine operation ends with KINT-12–14. Preserve history and prior
release artifacts for rollback rather than maintaining two engines indefinitely.
