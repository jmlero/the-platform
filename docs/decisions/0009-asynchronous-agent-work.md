# Decision 0009: Board, conversation, and inbox for background agent work

- Status: proposed interaction model; release sequencing and detailed behavior remain open
- Date: 2026-09-17
- Scope: Deck and Marina product vision

## Context

Moving agents to operator-controlled Kubernetes removes the laptop dependency.
The operator also wants to create tasks or discuss work in a web interface,
invoke a coding agent there, and receive progress and results asynchronously.
Relevant context should remain available across tasks and devices.

## Proposed direction

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

## Delivery boundary

This records a product proposal, not implemented mention handling or a change to
the public protocol. [Decision 0007](0007-sovereign-first-usable-release.md)
continues to govern the first usable release. Existing channel messages require
explicit operator promotion into a task. Conversational dispatch, automatic
context selection, and additional inbox behavior need separately defined
acceptance criteria before implementation.

The proposal should be evaluated by how easily the operator can delegate,
disconnect, and later understand a result or answer a necessary question, including
how much context they had to repeat. A different interface is useful only if it
improves that workflow.
