# Decision 0029: Approve exact PRD text before repository publication

- Date: 2026-10-03
- Status: implementation choice for PRD-01; acceptance is in [status](../status.md)
- Scope: Deck conversation drafts, operator approval and Marina repository work
- Builds on [0022](0022-autonomous-software-factory.md), [0027](0027-read-only-repository-context-for-chat.md) and [0028](0028-evidenced-landing-and-rebase.md)

## Draft and approval

The read-only chat agent can propose a bounded Markdown PRD with a title and a
normalized path under `docs/prd/`. It publishes that draft in its canonical Deck
thread, without a task, budget or source authority. Deck binds the exact text and
path to the invocation's repository binding and records a digest. Later turns can
read that retained draft when discussing revisions; a revision is a new draft.

An operator reviews the complete draft in the thread and explicitly approves its
digest, installed `repository/v2` profile and bounded execution budget. Deck
atomically records the approver, creates one Ready publication task and links it
to the thread. The task retains the exact document, destination and approval.
Service credentials cannot approve a PRD. A chat model's interpretation of approval
is not the typed approval record.

Changed text, repository bindings, unavailable profiles and changed replay
budgets are rejected. Repeated approval returns the original task even with a
new request key; restart and project export/import retain that identity.
Approval authorizes publishing the document only. Deriving or implementing its
product tasks remains PRD-02.

## Publication

The publication task follows the existing native implementation, independent
review, clean verification, required exact-commit CI and evidenced landing path.
Marina passes the approved document as typed frozen input and rejects a candidate
unless it writes exactly that path and those bytes as an ordinary file, with no
other changes. It checks again before branch creation and source publication.
Rebase and recovery preserve the same approval and require fresh existing gates.

Deck holds the proposal and approval history. Git becomes authoritative for the
published PRD; the landed task identifies its exact commit and the retained
approval identifies its path. No second PRD execution engine, direct chat Git
writer or separate completion mechanism is introduced.

The additive v1 behavior is advertised as `conversation.prd.draft`. Publishing a
draft uses the existing `conversation.outcome.publish` service grant; the new
approval endpoint is operator-only. Matching Deck, Marina and conversation BYO
releases are required. The installation must allow `docs/prd/` in its repository
file scope; the feature does not broaden that scope automatically.
