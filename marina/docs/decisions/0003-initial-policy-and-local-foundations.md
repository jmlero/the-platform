# 0003: Use Typed, Versioned Policy Snapshots for the Initial Local Safety Gates

- Status: Accepted as a reversible starting decision
- Date: 2026-08-16

## Context

Marina needs deterministic policy decisions before it can safely enable runtime or source-control mutation. The same local development slice also needs provider-neutral runtime, context, source-control, verification, signal, and recovery contracts that can be tested without selecting or deploying a Kubernetes runtime.

The initial policy evaluator must bind decisions to an exact project, policy version, change class, environment, and risk class; enforce bounded authority and budgets; and deny merge and release unless those actions are explicitly granted. A general-purpose policy service or language would add a dependency and an operational boundary before the first rules require it.

## Options considered

### Typed Go policy with immutable JSON snapshots

Uses explicit structs and a deterministic evaluator in the Marina core. Accepted snapshots are content-addressed and persisted in the execution journal. This is small, inspectable, and locally testable, but intentionally less expressive than a policy language.

### OPA and Rego

Provides a mature policy language and tooling, but introduces another evaluator, rule packaging lifecycle, and security boundary before Marina has a proven rule set.

### CEL or a custom expression language

Would permit configurable expressions with a smaller surface than Rego, but still requires defining variable schemas, evaluation limits, compatibility, error semantics, and safe rule distribution now.

## Decision

Use typed, versioned Go policy snapshots and a deterministic in-process evaluator for the initial safety gates.

- Scope every snapshot by project, version, change class, environment, and risk.
- Bind each request to an exact policy version and frozen task authority.
- Enforce ceilings for attempts, tokens, cost, concurrency, credential lifetime, and blast radius.
- Represent actions as a closed set. Merge and release remain denied unless explicitly present in the selected policy.
- Persist the accepted snapshot and digest; never reinterpret an active execution against a silently changed policy.
- Keep runtime, source-control, context, signal, and verification contracts provider-neutral. Their local implementations and tests do not enable external mutation by themselves.

## Consequences

- Policy decisions are reproducible without a network service or Kubernetes cluster.
- Adding a new action or budget dimension requires a reviewed code and schema change.
- The current controller must explicitly invoke the evaluator at each consequential gate before that gate can be marked enabled; merely defining the evaluator does not authorize runtime or source mutation.
- Kubernetes Job construction, workload identity, credential issuance, cluster limits, delivery, and real pull-request mutation remain deferred.

## Revisit when

Reconsider the representation when projects need independently distributed rules, non-developer policy authors, substantially more expressive conditions, or compatibility across multiple Marina implementations. Migration must preserve exact historical snapshots and deterministic replay.
