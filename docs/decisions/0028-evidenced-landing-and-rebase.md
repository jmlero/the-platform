# Decision 0028: Evidenced task completion and bounded rebase

- Date: 2026-10-03
- Status: implementation choice for LAND-02–03; acceptance is in [status](../status.md)
- Scope: Deck's additive HTTP/JSON v1 contract and Marina's repository workflow
- Builds on [0022](0022-autonomous-software-factory.md), [0025](0025-repair-attempts-and-budget.md) and [0026](0026-read-only-review-and-exact-patch.md)

## Completion and prerequisites

Providers advertising `run.complete.landed` accept an optional exact landing
observation on the existing completion request. It identifies the authorized
repository, base branch, commit and observation time. Completion requires current
independent semantic review, clean test and landing/required-CI evidence at that
commit. The current task's acceptance criteria must still match the run's frozen
criteria. Deck atomically completes the run and task and retains the landing,
run and selected evidence identities. It does not create a human acceptance.

The existing guard, fencing and idempotency checks apply. Restart and exact
request replay recover the original observation; a changed replay conflicts.
Ordinary version-1 completion retains its human acceptance behavior.

Deck releases landed prerequisites only for the same repository and base branch
with current, unexpired selected evidence. Failed earlier attempts remain
history. Marina observes that each prerequisite commit is contained in the exact
base used for execution; a ledger observation alone cannot establish Git ancestry.
Task requeue clears the old landing. Export/import preserves and validates it.

## Main movement

If main moves before landing, Marina retains the old branch, head and evidence,
then admits a native Git rebase onto the new exact revision within the original
runtime, model, cost and time limits. It uses a fresh scoped branch so no ref
needs a force-push. The frozen task, context and original failure history remain
unchanged. A conflict blocks with retained evidence and an explanation.

The rebased candidate needs a fresh independent exact-patch review before
commit, a new clean verification Job and required CI at the resulting exact
commit. Earlier passing evidence cannot complete it. Repeated main movement
consumes the same bounded runtime allowance. A moved task branch still blocks.

After a lost landing response, Marina first observes whether the original
verified commit is already on main, including as an ancestor of its current
head. It still requires that commit's required CI. This avoids duplicate
publication and unnecessary rebase work. Marina rechecks containment before
final evidence and completion.

## Offline checks

Native Codex and the independent verifier derive from the same prepackaged
project toolchain. Image preparation owns dependency and browser downloads;
execution uses frozen Go/npm caches and Chromium with runtime downloads
disabled. The producer runs the frozen checks before returning its candidate;
its report never replaces the independent Job or required CI.

Deck's browser suite uses its built embedded frontend and a disposable real
Deck process. Service-API fixtures provide deterministic chat and run outcomes;
those fixtures establish UI behavior, not live model or Git-provider acceptance.
