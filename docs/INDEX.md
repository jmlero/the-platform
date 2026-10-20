# Documentation index

Start here. The documentation has two kinds of files:

- **CAPITAL names are for humans.** Read these to understand the project.
- **lowercase names are working documents.** Agents keep them current; open them
  when you need detail.

## For humans

| Document | Answers |
| --- | --- |
| [VISION.md](VISION.md) | What are we building, and why? The agent and application platform, the autonomous software factory on it, the inner and outer loop, landing on main, the three-column experience, principles and direction. |
| [ARCHITECTURE.md](ARCHITECTURE.md) | How is it built? Components, how the loops map onto them, tech stack, key rules and deployment. |
| [MARINA.md](MARINA.md) and [marina-visual.html](marina-visual.html) | What is Marina, what is inside, how to install and use it, how to deploy your own agent. The HTML page is a visual tour. |
| [INDEX.md](INDEX.md) | Where is everything? This file. |

## Working documents

| Document | Answers |
| --- | --- |
| [status.md](status.md) | What works today, what still needs acceptance proof, and what is not supported. |
| [backlog.md](backlog.md) | What comes next, in order, with stable IDs and acceptance status. |
| [decisions/](decisions/) | Why a choice was made. Numbered records; add new ones instead of rewriting old ones. |
| [plans/](plans/) | Designs for work that is not built yet: the [platform components plan](plans/platform-components.md) and the [Marina platform API plan](plans/marina-platform-api.md). The shipped kagent work is recorded in [final validation](validation/kint-12-17-2026-10-01.md). Remove a plan once its work ships. |
| [validation/](validation/) | Dated, redacted evidence from checks on a named installation. Never edited after the fact. |

## Next to the code

| Document | Answers |
| --- | --- |
| [AGENTS.md](../AGENTS.md), [deck/AGENTS.md](../deck/AGENTS.md), [marina/AGENTS.md](../marina/AGENTS.md) | Rules for coding agents working in the repository. |
| [deck/README.md](../deck/README.md), [marina/README.md](../marina/README.md), [cli/README.md](../cli/README.md) | How each component works and how to run it. |
| [deck/api/openapi.yaml](../deck/api/openapi.yaml) | The Deck–Marina HTTP/JSON contract. |
| [deck/web/DESIGN.md](../deck/web/DESIGN.md) | Deck's visual design system. |
| [marina/docs/workloads/](../marina/docs/workloads/) | Workload profile and API contracts. |
| [marina/docs/decisions/](../marina/docs/decisions/) | Decisions internal to Marina. |
| [Marina on a local Kind cluster](../deploy/kubernetes/marina-local/README.md) (Marina only, one script), [Local Podman/Kind](../deploy/kubernetes/local-podman/README.md), [repository workflow](../deploy/kubernetes/first-release/README.md), [native agent and verifier](../deploy/kubernetes/agent-platform/README.md), [kagent trial](../deploy/kubernetes/kagent-trial/README.md) | Installation guides and runbooks. |
| [Context Lens pilot](../deploy/pilots/context-lens/README.md) | The operator's own first real use of the platform. |

## Where does a fact go?

One fact lives in one place. Other documents link to it instead of copying it.

| When this changes | Update |
| --- | --- |
| Goals, product experience or direction | [VISION.md](VISION.md), plus a decision record if it is consequential |
| Components, tech stack, ownership or safety rules | [ARCHITECTURE.md](ARCHITECTURE.md) |
| What is supported | [status.md](status.md), in the same change that ships or removes it |
| The order or state of work | [backlog.md](backlog.md) |
| A consequential choice | A new file in [decisions/](decisions/) |
| Test or acceptance proof | A new dated file in [validation/](validation/) |

## Decisions that shape the current system

| Decision | Summary |
| --- | --- |
| [0001](decisions/0001-product-boundary-monorepo.md) | Separate products in one monorepo. |
| [0004](decisions/0004-milestone-one-work-control-safety.md) | Idempotency, fencing, commands and outbox safety. |
| [0007](decisions/0007-sovereign-first-usable-release.md) | Own the execution system (its stop-at-PR boundary is superseded by 0022). |
| [0010](decisions/0010-initial-installation-services.md) | Pilot services, storage, environments and budget. |
| [0014](decisions/0014-project-repository-and-knowledge.md) | Project context and repository ownership. |
| [0017](decisions/0017-kagent-core-engine.md) | kagent becomes the agent engine. |
| [0018](decisions/0018-deck-work-preparation.md) | Deck owns preparing tasks (its PREP breakdown is replaced by PRDs in 0022). |
| [0019](decisions/0019-channel-first-project-work.md) | Channel-first project work. |
| [0020](decisions/0020-bounded-repair-policy.md) | Bounded repair after failures. |
| [0021](decisions/0021-semantic-review-gate.md) | Independent review before every commit. |
| [0022](decisions/0022-autonomous-software-factory.md) | Autonomous software factory: PRDs from the chat in the repo, agents land on main after two checks. |
| [0023](decisions/0023-focus-kagent-cutover.md) | kagent cutover gated on core failure cases; the LangChain diagnostic workload is retired. |
| [0024](decisions/0024-per-attempt-engine-templates.md) | Marina creates and removes each engine attempt's own template and gateway Secret. |
| [0025](decisions/0025-repair-attempts-and-budget.md) | Repair diagnosis, runtime-start budgets, durable failure history and fresh evidence for each commit. |
| [0026](decisions/0026-read-only-review-and-exact-patch.md) | Independent text-only review, exact-patch commit gating and budgeted response replay. |
| [0027](decisions/0027-read-only-repository-context-for-chat.md) | Frozen repository and selected project context for bounded read-only chat. |
| [0028](decisions/0028-evidenced-landing-and-rebase.md) | Automatic evidenced landing, source-observed prerequisites, bounded rebase and offline checks. |
| [0029](decisions/0029-approved-prd-publication.md) | Exact PRD drafts, operator approval and publication through the existing landing gates. |
| [0030](decisions/0030-ordered-prd-tasks-and-acceptance.md) | Ordered PRD task approval, source links and evidenced PRD acceptance. |
| [0031](decisions/0031-explicit-per-project-profiles.md) | Explicit per-project execution profiles, one active task across Marina. |
| [0032](decisions/0032-agent-and-application-platform.md) | Marina becomes a pluggable agent and application platform; the factory is one workload; the platform deploys apps. |
| [0033](decisions/0033-marina-platform-api.md) | Marina owns the platform API (HTTP/JSON and Kubernetes resources); Deck becomes a client. |
