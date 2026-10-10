# Decision 0027: Frozen repository and project context for chat

- Date: 2026-10-03
- Status: implementation choice for CHAT-11; acceptance is tracked in [status](../status.md)
- Scope: Deck conversation invocation and Marina's bounded conversation profile
- Builds on [0014](0014-project-repository-and-knowledge.md) and [0024](0024-per-attempt-engine-templates.md)

## Decision

Reuse the existing project repository binding and explicit source registrations.
Deck captures their selected revisions atomically with the operator's invocation.
The project-scoped `conversation.context.read` route resolves that exact selection
and reports current revocation. Editing sources or changing the repository does
not replace an invocation's recorded inputs. Existing invocations without a
snapshot return a missing-context response rather than acquiring today's sources.

Marina resolves the recorded repository binding, pins its branch to a commit,
and saves the complete regular-file index and selected source content in immutable
artifacts before engine dispatch. Recorded inputs survive restart. Selected
guidance retains its explicit trust and provenance; conversation, other repository
files and tool output remain data and cannot enlarge authority or budget.

The existing conversation actor continues to call only Marina's model gateway.
The gateway supplies the frozen context and offers two fixed model tools:
`list_repository_files` and `read_repository_file`. Reads use only the recorded
repository and commit. Links, submodules, incomplete trees and oversized or
non-text reads are denied. There is no shell, arbitrary network, Git publication
or repository mutation tool, and the actor receives no provider credentials.

Bound tool rounds, results and each file read under the existing turn deadline
and aggregate token limit. Reserve the logical call before accessing the model;
sum usage from intermediate responses and the final answer. Missing usage retains
the reservation and prevents another call. A clarification follow-up shares the
original limits and frozen repository. Recheck selected-source validity before
model calls, repository reads and publication; revocation stops further use.

This is additive to Deck's version-1 contract. OpenAPI, capability discovery,
fixtures, service grants and consumer conformance change together. Repository
access informs answers and future PRDs; it does not authorize repository work or
alter the separately governed task-delegation path.
