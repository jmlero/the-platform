# Decision 0018: Deck owns backlog refinement and focused work preparation

- Date: 2026-09-22
- Status: accepted product direction; implementation planned
- Scope: Deck backlog, task decomposition, context selection, and Marina handoff

## Channel-first clarification, 2026-09-23

[Decision 0019](0019-channel-first-project-work.md) supersedes the board-first
presentation: one repository/project channel is the workspace. Agents may create
Backlog drafts and maintain reference notes; Ready is the authorization point;
Done requires human acceptance. Standing instructions and accepted decisions
still require human approval. Existing ownership and execution safeguards remain.

## Context

The operator explicitly wants Deck to own the flow from a backlog outcome to
small, focused tasks with the right context for agents. Deck must support
preparing work, not only displaying executions and their results.

[Software Factories, Light and Dark](https://www.oreilly.com/radar/software-factories-light-and-dark/)
argues for short, verifiable agent loops, human judgment upstream in design and
architecture, and limiting generation to verification capacity. We apply those
principles to work preparation; the article is a design reference, not an
execution policy or a source of numeric task-size limits.

## Decision

**Deck owns what work should happen and how it is divided and prepared. Marina
owns how authorized work executes and recovers.** Implement this across both
products while keeping Deck the authoritative backlog and work-control surface.

- Deck owns prioritized outcomes, versioned decomposition proposals, accepted
  child tasks, dependencies, acceptance criteria, context selections, budget
  allocations, authorization, and review capacity.
- Marina runs bounded planning assistance as well as implementation, through the
  selected agent engine. A planning agent returns a proposal; it cannot authorize
  its children, grant permissions, or mutate the authoritative backlog directly.
- Deck owns the task's context brief: which explicit sources are relevant and
  why. Marina resolves those references, adds required execution inputs, and
  persists the immutable package actually delivered to the worker. Git retains
  ownership of repository instructions and source.
- A ready child has one observable outcome, bounded scope and exclusions, an
  explicit verification method, selected context, a compatible installed profile,
  and budget/authority within the parent allocation. Small means independently
  understandable and verifiable, not an arbitrary line or token count.
- Work already covered by explicit authorization can proceed through deterministic
  gates. Request a focused decision only for missing intent, authority, or a
  policy-required judgment. Plan acceptance and permission to execute are
  distinct facts; one deliberate operator action may record both.
- Begin with one level of decomposition and sequential execution. Preserve the
  supported human-review PR boundary. Parent completion is not inferred merely
  from successful child runs or agent assertions.

The [implementation design](../plans/work-preparation.md) defines records, lifecycle,
context, contract changes, failure behavior, and PREP-01–08 acceptance criteria.

## Relationship to existing direction

This extends [0014](0014-project-repository-and-knowledge.md): explicit,
versioned sources remain authoritative; execution packages remain Marina-owned.
It complements [0009](0009-asynchronous-agent-work.md): board and conversation
entry points must use the same preparation and authorization records.
It preserves [0017](0017-kagent-core-engine.md): kagent is the target execution
engine, not the backlog owner. New planning execution uses that integration
rather than creating another model client or agent runtime inside Deck.

The term "dark factory" in this repository means unattended authorized work.
This decision does not remove human architectural judgment or grant merge,
deployment, production, recursive delegation, or concurrent-task authority.

## Alternatives and delivery boundary

Putting decomposition entirely in Marina would hide backlog choices and create
competing task truth. Calling planning models directly from Deck would duplicate
execution, budgets, and recovery. Sending a whole backlog and full conversation
to every worker would lose task focus and explicit-source provenance.

Use the existing Go/SQLite products, public HTTP/JSON contract, tasks, decisions,
profiles, and context packages. Add only the missing preparation records and
guards. All described additions are planned; current tasks and run-plan revisions
are not evidence that decomposition or automatic dispatch exists. No runtime or
public capability changes as part of this documentation decision.
