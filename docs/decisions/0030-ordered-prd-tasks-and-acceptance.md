# Decision 0030: Approve ordered PRD tasks and accept their evidenced results

- Date: 2026-10-04
- Scope: PRD-02 and PRD-03
- Builds on [0029](0029-approved-prd-publication.md) and [0028](0028-evidenced-landing-and-rebase.md)
- Acceptance is in [status](../status.md)

The chat agent proposes up to eight bounded top-level tasks from an already
published PRD. A proposal names the publication task and its exact landed commit;
Deck verifies that source belongs to the invoked project and thread. The proposal
contains descriptions and observable criteria, but no execution authority.

One explicit operator approval selects the proposal digest, installed repository
profile and a per-task budget. Deck creates the complete list atomically in Ready,
links every task to its thread and frozen PRD path/revision, and records its order
as dependencies. The first task depends on publication; subsequent tasks depend
on their predecessor. Duplicate approval, including under a new request key,
returns the original tasks. Changed input conflicts. A failed transaction creates
no partial list. Service credentials and conversational approval cannot grant
this authority.

Marina independently checks the referenced PRD commit is on the execution base,
even if an operator later edits ordinary dependencies. Derived tasks use the
existing implementation, review, verification, repair, CI and landing path; the
exact-document guard applies only to the publication task.

Deck projects the PRD, all proposals, approved tasks and exact landed results in
its originating thread. An operator accepts a digest of the current approved
result set only after every task has current landing evidence. Acceptance adds a
human message to the same thread. Follow-up requests are explicit chat invocations
that produce new proposals requiring approval; they preserve earlier tasks and
acceptance history. Approving follow-ups changes the result digest and requires
fresh PRD acceptance once that work lands. Unapproved proposals grant no work.

Proposal, task source links and acceptance persist through restart and project
export/import. Import validates proposal digests, source identity, approval and
canonical task/thread links before committing. Matching Deck, Marina and BYO
releases advertise `conversation.prd.tasks`. The gateway's strict response schema
admits typed PRD drafts and task proposals; model output remains bounded data.
