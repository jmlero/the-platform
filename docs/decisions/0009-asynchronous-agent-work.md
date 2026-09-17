# Decision 0009: Board, conversation, and inbox for background agent work

- Status: proposed interaction model; release sequencing and detailed behavior remain open
- Date: 2026-09-17
- Scope: AppDeck and Marina product vision

## Context

Moving agents to operator-controlled Kubernetes removes the laptop dependency.
The operator also wants to create tasks or discuss work in a web interface,
invoke a coding agent there, and receive progress and results asynchronously.
Relevant context should remain available across tasks and devices.

## Proposed direction

AppDeck and Marina form one product experience while retaining their existing
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
