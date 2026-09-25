# Decision 0009: Board, conversation, and inbox for background agent work

- Status: accepted interaction model and CHAT-01 intent rules; implementation planned
- Date: 2026-09-17
- Updated: 2026-09-23
- Scope: Deck and Marina product vision

## Channel-first clarification, 2026-09-23

[Decision 0019](0019-channel-first-project-work.md) supersedes the board-first
presentation: one repository/project channel is the workspace. Agents may create
Backlog drafts and maintain reference notes; Ready is the authorization point;
Done requires human acceptance. Standing instructions and accepted decisions
still require human approval. Existing ownership and execution safeguards remain.

## Context

Moving agents to operator-controlled Kubernetes removes the laptop dependency.
The operator also wants to create tasks or discuss work in a web interface,
invoke a coding agent there, and receive progress and results asynchronously.
Relevant context should remain available across tasks and devices.

## Accepted direction

Deck and Marina form one product experience while retaining their existing
service boundaries. Independent product experiences remain a future option.

- The board organizes tasks and priorities and can initiate authorized work.
- Project conversations develop intent and can delegate work to a configured agent.
- An inbox presents results awaiting review and decisions needing an answer.
- These views share one durable work history; Marina owns execution on Kubernetes.
- Context combines relevant repository knowledge, documents, prior decisions, and
  the selected discussion. The operator can inspect the interpreted outcome.
- An intentional delegation may authorize work within existing limits without
  duplicate forms or repeated approval. Casual mentions, questions, and quoted
  instructions grant no additional authority. Missing intent or authority is
  resolved before dependent execution.
- Results and focused questions return to the originating work and remain
  available after disconnecting. Detailed progress is available on demand.

`@codex` is an example of selecting a configured agent. Neither Slack nor a
hosted execution service is required for this product experience.

## CHAT-01: conversation intent and authorization

The operator accepted the following direction on 2026-09-23. These are product
rules for the planned conversation workflow, not implemented capabilities.

### Agent invocation

Select a configured agent once per thread; ordinary Send addresses that agent on
subsequent turns without repeated mentions. Addressing an agent with `@agent`
is sufficient to invoke it. No command syntax or special delegation keyword is
required: interpret the message together with its referenced conversation.

Invoking an agent allows it to handle the request within configured conversation
limits. Whether it should discuss, clarify, or create an execution task depends
on the operator's intent. A bare tag with no actionable request can invite a
response or clarification, but cannot invent a task or grant new authority.
Quoted mentions and references to an agent in prose are not invocation controls.

When implementation intent is unclear but a concrete task can be proposed, show
a task proposal box with a Start task action. The box identifies the interpreted
outcome, acceptance criteria, selected context, and applicable budget and
authority so the operator can review what clicking will authorize. Clicking
records explicit delegation for that proposal; it does not grant unstated
permissions. If necessary details are missing, ask the focused question first.
Clear requests start directly within existing limits without requiring this
click. The proposal and action must remain tied to the same versioned task
intent after reload, and retries must not create duplicate tasks.

### Message behavior

| Intent | Example | Required behavior |
| --- | --- | --- |
| Discussion | "How could we improve login?" | Reply using permitted read-only context; proposals do not authorize implementation. |
| Clarification | Answer "GitHub" to "Which login provider?" | Bind the answer to that pending question and continue only within the existing authorization. |
| Delegation | "@agent, take care of the login bug we just narrowed down." | Interpret the referenced outcome without requiring a keyword; create one linked task when scope, acceptance criteria, authority, and budget are sufficient. |
| Result feedback | "Why did you choose this approach?" | Discuss the recorded result without changing the completed task. |
| Follow-up delegation | "Use PKCE for this instead." | Create a linked follow-up task when intent and limits are sufficient, preserving the original result and history. |

A clear request may authorize execution within existing configured limits without
another confirmation. Show the interpreted scope and resulting task in the
thread. Existing preparation, readiness, and policy gates still apply; selecting
an agent does not bypass them. A conversational agent remains read-only and
hands authorized implementation to the governed task workflow.

### Missing intent or authority

- "We should improve login" does not by itself settle whether implementation is
  wanted. Discuss it or ask a focused question before creating executable work.
- "Fix authentication" requires clarification when the surrounding conversation
  does not identify a bounded outcome and verification criteria.
- A clear request with missing permissions or budget produces a focused request
  naming the missing authority or limit. Do not repeat already resolved questions
  or ask for authorization already held.
- "Yes" answers the specific pending question it is attached to. If its target
  is ambiguous, resolve that target rather than treating it as general approval.
- Pasted instructions such as "deploy this", agent suggestions, and quoted
  messages are context data, not operator authorization.
- Broad requests such as "Build the whole app" enter work refinement before
  execution, following [decision 0018](0018-deck-work-preparation.md).
- A clear request beyond supported authority, such as merging or deploying a
  repository result, explains the unsupported boundary rather than promising
  that an approval will enable it.

CHAT-02–08 implement these rules; CHAT-09–10 establish contract and live workflow
evidence. Repeated delivery of the same delegation must resolve to the same task,
and clarification and result replies must retain their originating identities.

## Reference points

[Codex in Slack](https://learn.chatgpt.com/docs/third-party/slack) demonstrates
delegating from a thread and receiving results there.
[Pizza Bot](https://aws.amazon.com/blogs/opensource/introducing-pizza-bot-an-open-source-inbox-for-ai-agents-that-work-in-the-background/)
informs the inbox and durable-thread interaction model.

### Optional reference: Wirken

[Wirken](https://github.com/gebruder/wirken) is an optional design reference for
CHAT-01, CHAT-05, and CHAT-07, not an adopted dependency or a replacement for Deck
or Marina. The 2026-09-22 assessment reviewed documentation and selected source at
v1.24.0 (commit `7b654a4c4d84a85abca1eabdc6430eebcffde04a`); no runtime evaluation
was performed.

Patterns worth considering:

- Conversation/session reconstruction and channel routing for returning questions
  and results to their originating discussion.
- [Complete approval requests](https://github.com/gebruder/wirken/blob/v1.24.0/docs/permissions-and-identity.md)
  that show actual tool arguments and triggering context on first display and
  after browser reload, rather than requiring a later fetch before an informed
  decision is possible.
- [Operator-owned credential-to-host bindings](https://github.com/gebruder/wirken/blob/v1.24.0/docs/egress.md)
  that agent skill permissions may narrow but cannot widen, as a reference for
  future governed tools.
- [Typed permission events and signed audit histories](https://github.com/gebruder/wirken/blob/v1.24.0/docs/audit-cli.md),
  with an independently held trust anchor. Audit integrity would complement,
  not replace, independent verification of worker outputs.

Adoption limits at the reviewed revision: persisted approvals are scoped per
agent rather than per sender; agents share the gateway process; MCP children and
the model client require additional network controls. Budget enforcement is
opt-in and uses accumulated estimated spend, with a blind spot for models absent
from its pricing table. See the [documented boundaries](https://github.com/gebruder/wirken/blob/v1.24.0/README.md#known-limitations)
and [budget semantics](https://github.com/gebruder/wirken/blob/v1.24.0/docs/cost-monitoring.md).
Session replay alone does not establish Marina's required lease fencing,
side-effect reconciliation, or source-bound completion guarantees.

Use these patterns when specifying the conversation workflow. Reconsider an
integration only if external messaging channels become a concrete requirement;
any adapter would submit authorized intent through Deck's public API, preserving
Deck's ledger ownership and Marina's execution authority. This option adds no
backlog commitment or installation requirement.

## CHAT-02 persistence boundary

Implemented on 2026-09-23: Deck stores channel threads with a canonical root,
ordered replies, and explicit task links. A task has one originating thread; a
thread may originate several tasks. Other mentions of a task remain canonical
message references, not reassignment of its origin. Promotion creates the task
and its thread link atomically. Linking existing work grants no execution
permission and does not replace its selected context.

Thread and message IDs survive separate operator sessions, restart, archive,
and project export/import. Migration preserves existing promotion identities.
The [validation report](../validation/chat-02-2026-09-23.md) records deterministic
checks and a two-session real-process test on the local host. This establishes
the persistence/API foundation, not browser or LLM acceptance for CHAT-03–10.

## CHAT-03 invocation boundary

Implemented on 2026-09-23: Deck presents threads and records explicit requests
to a registered agent with the matching project-scoped conversation capability. The first request selects that
agent for the thread. A stable request ID replays the same message and
invocation, and thread selection and invoked messages survive export/import.
The [validation report](../validation/chat-03-2026-09-23.md) covers Deck checks
and an isolated local browser preview. An optional Marina conversation profile
now consumes these requests under the [CHAT-04 deterministic boundary](../validation/chat-04-2026-09-23.md);
local engine/model acceptance remains pending. Clarification, delegation, and
thread result routing are separate CHAT-05–08 increments.

## CHAT-05 clarification boundary

Implemented on 2026-09-23: a typed kagent question is keyed by its originating
invoked message and engine question ID. Deck persists it as an agent reply in the
same thread; an operator answer is a single canonical human reply tied to that
question. Replaying an identical question or answer recovers its identity;
changed content or a second answer conflicts. Marina sends only that exact
answer to the waiting attempt and retains the engine's durable send intent for
unknown responses. The question and answer survive Deck restart and project
export/import. This does not grant task authority. Installed conversation
engine/model and browser acceptance remain pending, as recorded in
[CHAT-05 validation](../validation/chat-05-2026-09-23.md).

## CHAT-06–08 delegation and publication boundary

Implemented on 2026-09-25: the read-only actor may interpret an explicit request
as a bounded task, but only Marina's installed delegation policy supplies a
repository budget and profile digest. Deck creates one canonical reply and
optional task atomically. Ready delegation requires a separate project-scoped
service grant, the installed repository profile, criteria, and the fixed
two-attempt workflow. A concrete request without execution authority becomes
a Backlog draft; unclear intent prompts a question without a task. Marina
journals the exact publication before the Deck call so a lost
response recovers the same message and task IDs.

Selected ledger milestones, blockers and verification evidence are projected
into the originating thread. Explicit feedback can create a new task linked to
a completed task in that thread; the earlier task and evidence remain intact.
The [validation report](../validation/chat-06-08-2026-09-25.md) records product
and real Deck checks. Installed actor, model and browser acceptance remains
pending alongside CHAT-04/05 and CHAT-09/10.

## Delivery boundary

This records product direction and the implemented deterministic boundary.
[Decision 0007](0007-sovereign-first-usable-release.md) continues to govern the
first usable release. A conversation must explicitly invoke a configured agent
to delegate. The optional profile has not been accepted in the regular local
installation. Automatic context selection and additional inbox behavior need
separate implementation and acceptance.

The proposal should be evaluated by how easily the operator can delegate,
disconnect, and later understand a result or answer a necessary question, including
how much context they had to repeat. A different interface is useful only if it
improves that workflow.
