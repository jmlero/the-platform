# Vision

What The Platform is for and why it exists. How it is built is in
[ARCHITECTURE.md](ARCHITECTURE.md); what works today is in [status.md](status.md);
the order of work is in [backlog.md](backlog.md).

Last reviewed: 2026-09-25.

## In one sentence

The Platform is an **autonomous software factory on my own Kubernetes**: I shape
the product in a chat, and agents build it with minimal supervision.

## What

### Two loops

- **Outer loop: mine.** I explore ideas with the project agent in the chat. Out
  of those conversations come PRDs and top-level tasks. PRDs live in the
  project's repository. Later, the outer loop also covers the context and
  knowledge bases that agents work with.
- **Inner loop: the agents'.** Agents take tasks and turn them into working,
  tested code on main: plan, code, test, get feedback, fix, until every check
  passes. They improve their work from the feedback on their own, within limits.

```text
  outer loop (me + chat agent)            inner loop (agents on Kubernetes)
  explore -> PRD -> top-level tasks ────> plan -> code -> test -> fix
     ^                                                 │      ^
     │                                                 v      │ feedback
     └──── see how the project evolves <──── land on main <── checks
```

### Landing on main, without pull requests

I don't review code, so there are no pull requests. An agent's change lands
directly on main after two checks pass:

1. **Platform check.** Unit and end-to-end tests run in a clean environment, and
   an independent reviewer checks the change against its task. A failure goes
   back to the agent as feedback, and it tries again, up to a limit.
2. **GitHub check.** The project's CI must pass on that exact commit before main
   moves. A failure here is also feedback for the agent.

Tests are what make this safe. Every change comes with unit and end-to-end tests,
and the reviewer rejects changes that weaken them. If the agent cannot pass the
checks within its limits, it stops and tells me why.

CI and deployment stay on GitHub. The platform does not deploy.

### The product experience

Three columns, kept simple:

- **Left: projects.** One project is one Git repository.
- **Middle: what is happening.** Three short lists: in progress (what agents are
  doing now), to do, and recently landed on main. Nothing more for now.
- **Right: the chat** with the project agent, for exploring ideas and creating
  PRDs.

Tasks finish on their own once they land on main with evidence. I accept at the
PRD level: does the product now do what the PRD describes?

### Who does what

| I own | The factory owns |
| --- | --- |
| Product direction, PRDs and priorities | Turning tasks into tested code on main |
| Architecture and acceptable risk | Running the checks and fixing its own failures |
| Accepting results against the PRD | Stopping and explaining when it is stuck or outside its limits |
| Context and knowledge for agents (later) | Reporting what changed, in plain language |

## Why

- **It runs on Kubernetes I control**, in the cloud or on-premises. Agent work
  runs on infrastructure I own, not only in a vendor's cloud. While I develop the
  platform it runs on my laptop; later it moves to an independent cluster.
- **I want to shape the product, not review code.** My attention goes to ideas,
  PRDs and outcomes. The measure of success is verified value per minute of my
  attention, without added risk.
- **Conversation is the natural way to explore.** One chat per project keeps
  ideas, PRDs and results together.
- **Trust comes from tests and independent checks**, never from an agent saying
  its work is done.

### North star: a dark factory

> An evidence-gated system that turns bounded intent into verified outcomes
> within explicit limits, and escalates what is outside its authority.

"Dark" means unattended. It does not mean opaque, unaccountable or unlimited.

## Principles

1. **Durable control, disposable compute.** Work, context, limits and evidence
   outlive any Pod or model session. A restart is normal, not a reason to redo
   work.
2. **Evidence over claims.** A change lands only when tests and independent
   checks pass on that exact commit. The agent that wrote a change is never its
   only checker.
3. **Feedback, then limits.** Agents learn from failed checks and try again, but
   every task has limits on attempts, time and cost. Nothing an agent reads can
   widen them.
4. **The repository is the truth for the project.** Code, PRDs, tests and how to
   build them live in the project's repository. Deck holds conversations and task
   state; Marina holds execution.
5. **Explicit steering.** My decisions are recorded and acknowledged.
6. **Infrastructure I control.** Plain Kubernetes, in the cloud or on-premises.
   Every external data destination is explicit.
7. **Replaceable parts.** Agents, models and engines can be swapped behind narrow
   contracts.
8. **Small and inspectable.** One process and one SQLite database per service
   until a measured need justifies more.

## Direction

The order of work, with acceptance criteria, is in [backlog.md](backlog.md). In
outline:

1. **One agent engine.** kagent owns agent execution; Marina owns workflow and
   governance ([decision 0017](decisions/0017-kagent-core-engine.md)).
2. **Chat to results.** A request in the chat becomes a task; results and
   questions come back to the same thread (CHAT).
3. **Land on main with feedback.** The two checks, bounded repair from their
   feedback, and the independent review
   ([decisions 0020](decisions/0020-bounded-repair-policy.md),
   [0021](decisions/0021-semantic-review-gate.md) and
   [0022](decisions/0022-autonomous-software-factory.md); AUTO, LAND).
4. **PRDs from the chat.** The chat agent drafts a PRD, I approve it, it is
   committed to the repository, and top-level tasks are derived from it (PRD).
5. **The simple three-column Deck** (UI-01).
6. **Later:** context and knowledge bases for agents, several tasks in parallel,
   and a move to an independent Kubernetes cluster.

**First milestone:** one real repository, one PRD born in the chat, and agents
landing its tasks on main without me touching the code.

Not planned now: deployment, multiple users, team permissions, high availability.
