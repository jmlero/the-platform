# Documentation map

Use the shortest document that answers the question:

| Need | Source |
| --- | --- |
| Product overview and development commands | [Root README](../README.md) |
| Channel-first project workflow | [Decision 0019](decisions/0019-channel-first-project-work.md), [validation](validation/deck-channel-first-2026-09-23.md) |
| Current state, vision and goal | [VISION.md](../VISION.md) (the reference) |
| Current ownership and runtime behavior | [Architecture](../ARCHITECTURE.md) |
| Active work | [Backlog](../TODO.md) |
| Persistent task-linked conversations and Deck invocation | [CHAT-02 evidence](validation/chat-02-2026-09-23.md), [CHAT-03 evidence](validation/chat-03-2026-09-23.md), [interaction decision](decisions/0009-asynchronous-agent-work.md) |
| Backlog refinement, focused tasks and per-task context | [Work-preparation design](work-preparation.md), [decision 0018](decisions/0018-deck-work-preparation.md) |
| Candidate projects for a real value pilot | [Repository assessment, 2026-09-22](pilot-project-assessment-2026-09-22.md) — Context Lens selected; ubahndoku completed |
| Selected Context Lens pilot | [Task and worker preparation](../deploy/pilots/context-lens/README.md), [offline evidence and live gates](validation/context-lens-pilot-preparation-2026-09-22.md) |
| Kagent integration and Marina code removal | [Implementation plan](kagent-integration-plan.md), [adoption analysis](kagent-adoption.md) |
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

The next engine direction is [0017](decisions/0017-kagent-core-engine.md): kagent
integration and removal of redundant Marina execution code. KINT-01–06 passed
their scoped gates, including one native repository pilot; two-workload rollout
remains pending.

[0018](decisions/0018-deck-work-preparation.md) assigns backlog refinement and
focused work preparation to Deck, with planning execution and immutable context
assembly in Marina. PREP-01–08 implementation and acceptance remain pending.

## Validation

Validation files are dated evidence snapshots. They support claims about a named
build and environment; they are not current operating instructions.

| Scope | Evidence |
| --- | --- |
| Bounded read-only conversation dispatch and retained result; local engine proof pending | [CHAT-04](validation/chat-04-2026-09-23.md) |
| Persistent thread clarification and answer routing; installed engine proof pending | [CHAT-05](validation/chat-05-2026-09-23.md) |
| Idempotent conversation delegation, thread results, and linked follow-up; installed actor proof pending | [CHAT-06–08](validation/chat-06-08-2026-09-25.md) |
| Thread browser view and explicit configured-agent invocation | [CHAT-03](validation/chat-03-2026-09-23.md) |
| Channel-first projects, human result acceptance and dependency admission | [Deck channel-first evidence](validation/deck-channel-first-2026-09-23.md) |
| Draft deletion, reversible task archive and local browser/API verification | [Deck task retention](validation/deck-task-retention-2026-09-23.md) |
| One Deck-authorized native Codex repository task, verified PR head and safe local teardown | [KINT-06](validation/kint-06-2026-09-23.md) |
| Shared result validation, digest-bound engine artifact translation and independent verifier boundary | [KINT-05](validation/kint-05-2026-09-23.md) |
| Governed engine gateway; real local native Codex/Substrate HTTPS injection, rotation, revocation, bypass denial and safe teardown | [KINT-04](validation/kint-04-2026-09-22.md) |
| Pinned engine adapter, durable revision/A2A correlation and isolated local recovery | [KINT-03](validation/kint-03-2026-09-22.md) |
| Pinned kagent/Substrate, ARM64 native lifecycle, persistence, isolation and safe teardown | [KINT-01/02](validation/kint-01-02-2026-09-22.md), [lifecycle](validation/kint-01-02-lifecycle-2026-09-22.json), [resources](validation/kint-01-02-resources-2026-09-22.json), and [teardown](validation/kint-01-02-teardown-2026-09-22.json) |
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
