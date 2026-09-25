# Decision 0022: Autonomous software factory: PRDs from the chat, agents land on main

- Date: 2026-09-25
- Status: accepted product direction; not implemented (LAND, PRD and UI-01 in the backlog)
- Scope: product direction for Deck and Marina. Supersedes the stop-at-PR delivery
  boundary of [decision 0007](0007-sovereign-first-usable-release.md), moves human
  acceptance from [decision 0019](0019-channel-first-project-work.md) to the PRD,
  and changes the delivery step after [decision 0021](0021-semantic-review-gate.md).

## Context

The platform delivered one task as a pull request for human review. The operator
no longer reviews code and already commits directly to main in their own work.
The review step at the end of every task added waiting time without adding
checks. The operator restated the goal: an autonomous platform where agents run
on Kubernetes with minimal supervision, steered from a chat.

## Decision

The operator decided:

- **Two loops.** The outer loop belongs to the operator: exploring in the chat,
  and deriving PRDs and top-level tasks from those conversations. Context and
  knowledge bases for agents join the outer loop later. The inner loop belongs to
  the agents: plan, code, test, take feedback and fix, until the change lands.
- **PRDs live in the project's repository.** Build and test definitions do too.
  This follows [decision 0014](0014-project-repository-and-knowledge.md): Git owns
  repository documents.
- **No pull requests.** A change lands directly on main after two checks:
  1. The platform check: unit and end-to-end tests in a clean verification Job,
     and the independent review of decision 0021.
  2. The GitHub check: the project's required CI passes on that exact commit
     before main moves.
- **Feedback.** A failure in either check goes back to the agent as untrusted
  feedback for another attempt, within the limits of
  [decision 0020](0020-bounded-repair-policy.md). When the limits are reached,
  the task blocks with an explanation.
- **Tests are required.** Changes come with unit and end-to-end tests, and the
  reviewer rejects changes that weaken them.
- **CI and deployment stay on GitHub.** The platform does not deploy.
- **Deck has three columns:** projects, a very simple middle column (in progress,
  to do, recently landed), and the chat for exploration and PRDs.
- **Value:** it runs on Kubernetes the operator controls, including on-premises.
  Development stays on the laptop; an independent cluster comes later.

The operator did not choose an acceptance model. The default adopted here: a task
completes on its own when it lands on main with evidence, and the operator
accepts at the PRD level. An agent still never accepts its own work, because an
independent verifier and reviewer pass every task.

## Landing sequence

```text
agent attempt -> independent review -> commit -> clean verification (unit + e2e)
  -> push to a task branch -> required GitHub CI on that commit
  -> fast-forward main to that exact commit
```

- Only Marina pushes. Workers never receive Git write credentials.
- Main is only fast-forwarded, never force-pushed. The operator also commits to
  main, so if main moved, Marina rebases, and the change passes both checks again
  before it lands. A conflict is feedback for the agent or a blocker.
- Marina's Git identity needs permission to update main. Branch protection
  should keep requiring the CI checks, so GitHub also enforces the second check.

## Consequences

- The existing PR workflow stays the supported behavior until LAND-01 ships.
  Evidence from earlier PR runs remains valid history.
- The verification image must include what the project's end-to-end tests need,
  prepackaged, without runtime downloads.
- "Code prerequisite is on the base branch" becomes a fact Marina observes, not
  an operator confirmation.
- LATER-05 (authority beyond PR review) is retired by this decision.
- Deployment, production signals, parallel tasks, and knowledge bases remain later
  work.
