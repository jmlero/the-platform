# Decision 0025: Durable repair attempts within the original budget

- Date: 2026-10-01
- Status: implementation choice for AUTO-02–06; acceptance is tracked in [status](../status.md)
- Scope: Marina repository workflow and its existing Deck HTTP contract
- Builds on [0020](0020-bounded-repair-policy.md), [0022](0022-autonomous-software-factory.md) and [0024](0024-per-attempt-engine-templates.md)

## Decision

Keep the three-failure limit from 0020 separate from the version-1 task budget.
`max_attempts` continues to count runtime starts, including independent verification.
Changing it to count only implementations would silently enlarge existing authority.

Each repair uses a native kagent diagnosis attempt, a native implementation
attempt, and a separate clean verification Job. The installation ceiling is
eight runtime starts; the frozen task can authorize fewer.

| Frozen `max_attempts` | Work that can fit, subject to token, cost and time limits |
| --- | --- |
| 2 | Initial implementation and clean verification |
| 5 | Initial implementation, verification and one complete repair |
| 8 | Initial implementation, verification and up to two complete repairs |

The third failed implementation blocks even if a task offered a larger runtime
budget. A failure cannot reset the deadline, usage, reservations, task intent,
context package, installation profile, original base or authority. Unknown usage
retains its reservation and blocks repair. The gateway admits every model call
against the same execution budget.

## Diagnosis before implementation

Persist each failed clean check or required CI observation with its exact commit
and artifact digest. Publish failed verification and CI evidence to Deck before
diagnosis. Treat all output, logs and previous agent text as untrusted data.

A separate `repair_plan` attempt returns only a bounded diagnosis, proposed
approach, matching failure digest and `can_repair` decision. It has read access
through the existing source gateway and no Git publication credentials. Source
changes and check results are invalid planning output. Marina persists the plan
before admitting another implementation. Missing or mismatched plans and an
explicit refusal block. A normalized exact repeat of a failed approach also
blocks; whether a differently worded approach is substantively different remains
the agent's judgment, visible to the operator as specified by 0020. Semantic
review remains AUTO-08.

Runtime starts, commits and evidence have stable keys per repair round. Original
attempt keys and empty runtime-stage serialization remain unchanged. Recovery
uses the recorded attempt and keys instead of dispatching replacement work.
No journal migration or Deck protocol change is required.

## Publication and evidence

Repair commits extend the same task branch from its recorded head. A moved task
branch or original base blocks before diagnosis; publication is guarded by the
expected head. `repository/v2` lands only the latest independently verified,
CI-passing commit through guarded fast-forward. `repository/v1` retains its
existing PR and updates that branch rather than opening another PR.

Earlier evidence remains history with its original commit and identities. Each
repair clears the active evidence selection and requires fresh verification at
the new commit. Task-branch head checks also precede final evidence and completion.

The existing guarded run blocker publishes failure history, evidence links,
diagnoses and operator options when repair stops. It grants no continuation or
budget change: a new authorized task is needed after a terminal blocker. Human
review changes, invalid implementation output, policy denial, cancellation and
infrastructure failures keep their existing stop behavior; this slice implements
verification and required-CI feedback only.

## Acceptance

[The validation report](../validation/auto-02-06-2026-10-01.md) distinguishes unit
and real-Deck-process checks from native kagent/model/Git acceptance on local
Kind. Passing deterministic checks alone does not establish the installed loop.
