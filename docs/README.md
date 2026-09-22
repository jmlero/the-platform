# Documentation map

Use the shortest document that answers the question:

| Need | Source |
| --- | --- |
| Product overview and development commands | [Root README](../README.md) |
| Product direction | [Marina vision](../VISION.md), [Deck vision](../VISION_DECK.md), and [dark-factory definition](../DARK_FACTORY.md) |
| Current ownership and runtime behavior | [Architecture](../ARCHITECTURE.md) |
| Active work | [Backlog](../TODO.md) |
| Public Deck–Marina contract | [Contract map](../contracts/README.md) |
| Local installation | [Podman/Kind guide](../deploy/kubernetes/local-podman/README.md) |
| Repository workflow | [Installation guide](../deploy/kubernetes/first-release/README.md) |
| Two-workload profile | [Installation guide](../deploy/kubernetes/agent-platform/README.md) |

## Decisions

Files under [decisions/](decisions/) record consequential choices and their
context. They are historical records, so later clarifications may remain in the
same file. Use the architecture and component READMEs for current behavior.

The decisions most useful for understanding the current release are:

- [0001](decisions/0001-product-boundary-monorepo.md): separate products in one
  monorepo
- [0004](decisions/0004-milestone-one-work-control-safety.md): idempotency,
  fencing, commands, and outbox safety
- [0007](decisions/0007-sovereign-first-usable-release.md): repository workflow
  and human-review boundary
- [0010](decisions/0010-initial-installation-services.md): pilot services,
  storage, environments, and budget
- [0014](decisions/0014-project-repository-and-knowledge.md): project context and
  repository ownership
- [0015](decisions/0015-marina-agent-platform.md): bounded second workload
- [0016](decisions/0016-workload-invocation-and-context-tool.md): public
  invocation and governed context reads

## Validation

Validation files are dated evidence snapshots. They support claims about a named
build and environment; they are not current operating instructions.

| Scope | Evidence |
| --- | --- |
| Repository workflow, recovery, cancellation, failures, budgets, and final local state | [2026-09-21 acceptance](validation/poc-mvp-2026-09-21.md) and [machine-readable record](validation/poc-mvp-2026-09-21.json) |
| Certificate rotation and artifact retention | [2026-09-17 operations](validation/first-release-operations-2026-09-17.md) |
| Project context and repository association | [CTX-02–08](validation/ctx-02-08-2026-09-21.md) |
| Second-harness PoC | [AGENT-02–06](validation/agent-02-06-2026-09-21.md) and [record](validation/agent-02-06-2026-09-21.json) |
| Workload API, context tool, inspection, and network boundary | [AGENT-07–10](validation/agent-07-10-2026-09-21.md), [client record](validation/agent-07-10-client-2026-09-21.json), and [network record](validation/agent-07-10-network-2026-09-21.json) |
| Combined two-workload MVP and upgrade preservation | [AGENT-11–13](validation/agent-11-13-2026-09-22.md) and [record](validation/agent-11-13-2026-09-22.json) |

Small supporting records remain beside those reports where they document a
specific credential exception, GCP access boundary, or regression fix. Earlier
preparation and provider-probe reports were removed after their results were
superseded by the final repository-workflow acceptance report.
