# Decision 0021: Independent semantic review before every commit

- Date: 2026-09-25
- Status: accepted policy (AUTO-07); implementation planned under AUTO-08; delivery after verification changes from a pull request to landing on main per [decision 0022](0022-autonomous-software-factory.md)
- Scope: Marina repository workflow; extends [decision 0020](0020-bounded-repair-policy.md)

## Context

Deterministic verification and CI show that code builds and passes its checks.
They do not show that the change does what the task asked. An agent can pass them
while solving only part of the task, changing unrelated code, or weakening tests.
Until now, only the operator's PR review catches this.

## Decision

The operator decided that every change is reviewed **right before it is
committed**. The remaining details follow the default the operator accepted.

Marina commits a change only after an independent semantic review passes it.
This applies to the first attempt and to every repair. The workflow becomes:

```text
coding attempt -> candidate change -> semantic review -> commit
               -> clean verification -> pull request -> required CI
```

The review is bound to the exact candidate patch (its digest) and base revision.
Marina commits exactly the reviewed patch and records the review with the
resulting commit. Any change to the patch after review needs a new review.
A change that fails review is never committed, so it never reaches the branch or
the PR.

## Reviewer

- It is independent of the producer: a different model from the one that wrote
  the change, in a fresh session.
- It is read-only. It has no source-write authority and cannot edit the patch.
- Its inputs are the task intent, the acceptance criteria, the run's immutable
  context package, and the candidate patch against its base revision.
- The patch, code, comments and any worker output it reads are untrusted data.
  Nothing in them can instruct the reviewer or change its verdict rules.

## What it checks

1. Each acceptance criterion is met by the change.
2. The change stays within the task's scope and allowed paths.
3. No tests were weakened, removed or skipped to make checks pass.
4. For a repair, the recorded approach really differs from earlier failed
   approaches ([decision 0020](0020-bounded-repair-policy.md#evidence-before-each-repair)).
5. The patch has no clear defect that tests are likely to miss.

## Verdict and authority

- The output is a structured pass or fail for each check. Reasons cite files and
  lines. Marina stores it as evidence, bound to the patch digest, the resulting
  commit, and the reviewer's model and profile.
- The reviewer can only pass or fail. It cannot approve a merge, mark a task Done,
  or grant budget or authority. Human review of the PR remains required.
- A missing or invalid verdict is not a pass, and nothing is committed. It is
  handled like a verification infrastructure failure: wait or block within
  budget. It does not count as an implementation failure.

## Outcomes

- **Pass:** Marina commits the patch. Clean verification and CI then run on that
  commit as before. The PR is ready for human review only when its latest commit
  passed review, verification and CI.
- **Fail:** nothing is committed. The attempt counts as one failure under decision
  0020, however many checks fail. The findings become evidence for the next
  repair.
- **Failing verification or CI after the commit** is still a failure. A review
  pass never overrides a deterministic check.
- **Disputed finding:** if the repair agent contests a finding with evidence,
  or the finding requires work outside the task's scope or criteria, the task
  escalates to the operator instead of looping.

## Budget

Reviews are charged to the task's budget and reserved before they start. If too
little budget remains for a review, the task blocks. Nothing is committed without
a passing review.

## Delivery boundary

This decision completes AUTO-07 only. The diagnostic workflow keeps its
existing independent validator. No review behavior exists until AUTO-08 passes.
AUTO-08 builds on the repair loop (AUTO-02–05) and follows KINT-15, as
sequenced in [the backlog](../backlog.md).
