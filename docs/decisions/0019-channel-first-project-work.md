# Decision 0019: Channel-first project work

- Date: 2026-09-23
- Status: accepted product direction; Deck interaction and manual lifecycle implemented, conversational agent automation pending
- Scope: Deck experience, task acceptance, context stewardship, and Marina conversation integration

## Decision

The operator selected a lean, professional conversation-first experience. Each
project represents one GitHub repository and has one main channel. Topics use
threads in that channel. Conversation is the default workspace; a compact Tasks
view and Context view sit within the project. Existing channels and historical
records remain accessible during migration. Multiple named agents may eventually
have different harnesses and models. Start with one configured agent using kagent
and one OpenRouter model; keep runtime/model configuration out of everyday chat.

The operator explicitly accepted these rules:

1. An invoked agent may create concrete Backlog drafts without another approval,
   and briefly report their creation in the conversation. Drafting never authorizes
   execution. Casual discussion may produce a draft, but not a Ready task.
2. Moving a task to Ready authorizes automatic execution once prerequisites,
   installed policy, authority, budget, capacity, and intake controls allow it.
   Do not add a second generic Start confirmation. Missing authority still needs
   a focused decision. Clear natural-language authorization may record the same
   transition after refinement; tasks always have a recorded Backlog origin.
3. The human workflow is Backlog → Ready → In progress → Done. Planning,
   verification, blockers, and Awaiting review are details of ongoing work.
   A verified result or PR awaits review. Done means the human accepted the result
   or an authorized integration observed the required merge. An agent cannot
   accept its own output. Marina still has no merge/deploy authority.
4. An agent may maintain project summaries and factual notes automatically as
   versioned reference data, with source provenance. Changing standing instructions
   or accepted decisions requires human approval. Notes cannot promote themselves
   to guidance, override authoritative sources, or change active run context.

This supersedes the equal prominence of board/conversation/inbox in decisions
0009 and 0018. Their ownership, durable history, focused preparation, explicit
bounds and immutable execution inputs remain. Simple independent tasks need no
parent work package or decomposition ceremony.

## Implemented Deck behavior

- Project sidebar and default conversation, persistent threads, compact four-stage
  task list, project-scoped context and attention, and retained archive access.
- Atomic creation of a project, channel and pending repository association. Marina
  still validates access. Creating projects does not install a runtime or grant
  another project execution authority.
- Message promotion to a Backlog draft without premature criteria or permissions;
  exact selected message references and the originating thread remain attached.
- Saving a message as a versioned reference-data note. This is currently a manual
  action; automatic summarization and agent-authored drafts are not implemented.
- Dependencies edited in Backlog with current-version and same-project checks,
  cycle rejection, and enforcement at ready discovery and atomic claim.
- Human acceptance bound to a completed run, operator and task version. The v1
  execution status `done` retains its existing meaning; optional `review` metadata
  supplies the additional human-acceptance fact. Old completed tasks await review.
  Requeue clears acceptance without changing prior run/evidence history.
- A repository prerequisite additionally requires the operator to confirm its code
  is available on the project base branch. This is an explicit human attestation,
  not an automatic Git merge observation. Merely opening or accepting an unmerged
  PR does not release dependent code work. Stale evidence blocks release.

## Remaining integration

CHAT-03 provides Deck's configured agent selection and durable invocation
record. CHAT-04–08 still need dispatch, a bounded read-only
conversation worker through Marina/kagent, durable turn recovery, clarification,
agent-proposed drafts, source-bound automatic notes, and result return to the
originating conversation. Agent notes must have a separate data-only mutation
boundary; the operator guidance endpoint must not be granted to agents.

The composer truthfully states that agent replies are not connected. This change
does not enable intake, switch the installed runtime, fabricate an agent response,
claim automatic merge detection, or satisfy KINT cutover gates. The configured
single-project execution boundary remains until separately implemented.
