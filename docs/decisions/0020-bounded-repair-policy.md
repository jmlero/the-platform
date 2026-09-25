# Decision 0020: Bounded repair policy for the repository workflow

- Date: 2026-09-24
- Status: accepted policy (AUTO-01); implementation planned under AUTO-02–06
- Scope: Marina repository workflow, one repository and one active task

## Decision

The operator set the limit at three failures and required each new attempt to
start from evidence and try a different approach. The remaining details were
delegated and are recorded here.

A task may fail at most **three times**: the first implementation attempt plus
up to two repair attempts. The third failure blocks the task and escalates to the
operator. The operator confirmed this reading on 2026-09-25. The task budget's
`max_attempts` can lower this limit but never raise it.

## Failures that allow a repair attempt

Each of these counts as one failure:

- deterministic verification failed;
- a required CI check failed at the current PR head;
- human review requested changes;
- the worker finished without a valid change or result.

## Failures that escalate immediately

These block the task without a repair attempt and follow the existing
[failure rules](../../ARCHITECTURE.md#failure-behavior):

- time, token or cost budget exhausted, or too little budget left for another attempt;
- authority or policy denial, including changes outside the allowed paths;
- credential or authentication errors;
- lost lease or fence, or operator cancellation;
- PR closed, or its head changed by someone other than Marina;
- uncertain usage (the reservation is kept in full).

Infrastructure outages wait or block within budget as before. They do not count
as failures because they say nothing about the approach.

## Evidence before each repair

The user's rule: after each failure, find evidence, then try a different way.

1. Collect the failure evidence from its source: verification output, CI results
   at the exact head, and review comments. All of it is untrusted data, not
   instructions.
2. Record a short diagnosis: what failed, the likely cause, and the approach
   used by every earlier attempt.
3. Record the approach of the new attempt. It must differ from every earlier
   failed approach and be justified by the collected evidence.
4. If no new, evidence-based approach exists, escalate now instead of spending the
   remaining attempts. This includes cases where the evidence points outside the
   task's scope or authority.

Marina refuses to start a repair attempt without a recorded diagnosis and
approach. Whether the approach really differs is the agent's judgment, which the
operator can see. [Decision 0021](0021-semantic-review-gate.md) adds an
independent review right before every commit.

## Budget, evidence and publication

- All attempts share the task's original budget. A failure never grants new budget
  or authority; only an explicit operator budget change can.
- Each repair pushes new commits to the same branch and PR. It never opens a
  second PR.
- Each new head needs fresh clean verification and CI. Earlier evidence stays as
  history and cannot complete the task.
- Repairs keep the same immutable task intent, profile and context package.

## Escalation

When the limit is reached or a failure escalates immediately, Deck receives one
focused decision. It lists every attempt with its failure evidence, diagnosis and
approach, and the options: narrow the scope, raise the budget, fix it by hand, or
cancel.

## Delivery boundary

This decision completes AUTO-01 only. No repair behavior exists until AUTO-02–06
pass. The current workflow still makes one attempt. Engine-dependent repair work
follows KINT-15, as sequenced in [TODO.md](../../TODO.md). Merge and deployment
remain human authority.
