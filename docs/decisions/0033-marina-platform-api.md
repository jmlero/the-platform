# Decision 0033: Marina owns the platform API; Deck becomes a client

- Date: 2026-10-17
- Status: accepted direction; not implemented (MAR-01–11 in the backlog)
- Scope: product boundary between Marina and its clients. Supersedes the
  ownership parts of [decision 0001](0001-product-boundary-monorepo.md) (Deck owns
  the contract), [0004](0004-milestone-one-work-control-safety.md) and
  [0006](0006-versioned-deck-ledger.md) (Deck's ledger is authoritative for
  runs, leases and fences). Builds on
  [decision 0032](0032-agent-and-application-platform.md).

## Context

Marina was built as Deck's client: it polls Deck, claims a task, holds a lease
and reports back. That makes the platform depend on one steering tool and one
factory. The operator wants Marina to be a product on its own that any factory,
UI or CLI uses through one API, like an extension of Kubernetes.

## Decision

The operator decided:

1. **Marina owns the API and execution state.** Runs, budgets, attempts, agent
   turns, the decisions Marina needs answered, evidence, repository bindings and
   execution profiles live in Marina. Marina calls no client.
2. **Deck is a client.** Deck keeps the human side (projects UI, chat threads,
   PRDs, backlog and acceptance) and submits runs and turns to Marina.
3. **Platform invariants versus factory choices.** Marina always enforces:
   budgets and limits, sandbox and credential isolation, idempotency, audit, and
   no completion without independent evidence. Everything else is a factory
   choice made in a workload profile or run: which checks, whether a review runs,
   pull request or landing on main, repair limits, PRDs.
4. **Two API surfaces, one model.** An HTTP/JSON API with an OpenAPI contract for
   UIs, CLIs and live events, and Kubernetes custom resources (Project, Workload,
   Run, Turn) that Marina watches and reconciles. Resource status mirrors
   Marina's journal, which stays the record used for recovery.
5. **Kubernetes identity and tenancy.** Each project lives in a namespace. Both
   surfaces use Kubernetes identities: the HTTP API accepts Kubernetes tokens and
   checks the same RBAC as the resources. CLI, UI and other clients get access
   through normal Roles.
6. **Switch directly.** No long dual-path period: the new API replaces the
   Deck-provider path in one cutover; old records stay readable.
7. **Start now**, before the remaining PLAT tasks.
8. **No engine or sandbox in the API.** The API speaks in workloads, runs, turns,
   budgets and evidence and never names kagent, Agent Substrate, gVisor, Pods or
   Jobs, so the runtime can be replaced later without clients noticing.

## Consequences

- The tasks are in the [Marina platform API plan](../plans/marina-platform-api.md).
- The contract moves from `deck/api/` to `marina/api/`. Until the cutover ships,
  [status](../status.md) and the current Deck contract describe what works.
- Marina gains a controller for its custom resources. This is the reconciliation
  need that decision 0032 required before adding custom resources.
- Deck's work-control provider endpoints and Marina's Deck client are removed at
  the cutover.
