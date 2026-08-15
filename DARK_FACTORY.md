# What Is a Dark Factory?

A dark factory is an always-on production system that can complete its normal work without humans being present or continuously operating it.

In manufacturing, “lights-out” means automation can keep machines running overnight and workers return to completed parts. The factory still has designers, engineers, owners, maintenance, and safety rules—they simply are not required on the production floor for every unit produced. [FANUC describes this literally as leaving in the evening and returning to finished production.](https://crx.fanucamerica.com/articles/athena-achieves-lights-out-production)

## Applied to software

For Marina, a dark software factory is:

> An always-on, policy-constrained and evidence-gated software delivery system that can turn bounded intent or operational signals into verified software outcomes without synchronous human operation, escalating only decisions outside its authority.

The key words matter:

- **Always-on:** it runs independently of your laptop and terminal.
- **Bounded intent:** it receives a defined outcome, not unlimited permission to “improve everything.”
- **End-to-end:** it goes beyond writing code into testing, review, PR revision, delivery, observation, and remediation.
- **Policy-constrained:** permissions, cost, risk, retries, and production authority are explicit.
- **Evidence-gated:** “done” requires tests, checks, review evidence, and observed outcomes—not merely an agent saying it is done.
- **Unattended:** the normal path continues while you sleep.
- **Escalating:** ambiguity and high-risk decisions return to you.

This matches emerging software-factory implementations that autonomously implement, review, revise, and merge work while humans retain architecture and specification responsibility. [Dark Factory](https://godarkfactory.com/) uses the formulation “humans design, agents build,” while [OpenAI’s harness-engineering experience](https://openai.com/index/harness-engineering/) describes agents working for hours while humans sleep and handling implementation, review feedback, CI failures, and merging.

## The human is outside the hot loop

A dark factory does not mean “no humans.”

It means two different loops:

```text
Human governance loop
Intent → architecture → policy → priorities → exceptional decisions
                         ↓
Autonomous factory loop
Sense → plan → execute → verify → deliver → observe → remediate
                         ↺
```

You remain responsible for:

- What should be built.
- Architectural direction and technical taste.
- Which actions agents may take.
- What counts as acceptable evidence.
- Which decisions always require you.
- How the factory should improve after failures.

The factory becomes responsible for repeatedly carrying out the bounded engineering process.

## What the factory contains

| Factory concept | Marina equivalent |
| --- | --- |
| Factory control system | Marina |
| Operator control room | AppDeck plugin |
| Factory floor | Kubernetes |
| Autonomous work cell | Isolated agent runtime |
| Machine and tooling | Agent harness, tools, and sandbox |
| Work order | Task, outcome, constraints, and budget |
| Materials and instructions | Repository plus assembled context |
| Conveyor and routing | Durable workflow and agent loops |
| Inspection stations | Tests, scans, evaluators, and independent review |
| Safety interlocks | Identity, policy, approvals, limits, and kill switches |
| Sensors | CI, deployments, logs, metrics, traces, and alerts |
| Finished product | A verified software outcome in operation |

An individual agent is therefore not the dark factory. It is one replaceable worker or machine inside it.

## What it is not

A dark factory is not:

- A coding agent running in your terminal.
- A cron job that launches Codex.
- A collection of subagents with job titles.
- A CI pipeline that generates code.
- Automatic merging without risk controls.
- A black box whose decisions cannot be inspected.
- An AI system independently deciding company strategy.
- An agent with permanent production credentials.
- An infinite “keep trying until it works” loop.

All of those could exist without producing a trustworthy autonomous factory.

## A concrete example

Imagine a dependency vulnerability is detected at 02:00:

1. Marina receives the security signal.
2. It validates and classifies the signal rather than treating its text as instructions.
3. It creates or claims bounded work.
4. It assembles repository, architecture, security, and dependency context.
5. An isolated agent implements the upgrade.
6. Tests and scanners run.
7. An independent reviewer checks the change.
8. The agent fixes resulting CI or review failures.
9. It creates a merge-ready PR—or merges if policy permits.
10. If deployed, Marina observes the canary and either confirms the outcome or rolls back.
11. AppDeck contains the complete evidence when you wake up.

If the upgrade requires a breaking database migration, Marina pauses and asks you. That does not mean the factory failed. Correct escalation is part of autonomous operation.

## Darkness is scoped, not absolute

A repository might be “dark” for:

- Dependency updates.
- Documentation repairs.
- Test improvements.
- Known defect patterns.
- Low-risk application changes.

But still require human decisions for:

- New product direction.
- Architecture changes.
- Destructive migrations.
- Security-boundary changes.
- Broad production incidents.

So autonomy is not one global switch. It is policy per repository, change class, environment, and risk level.

The important metric is also not “percentage of work without humans.” It is:

> How much verified value can the factory deliver per minute of human attention, without increasing operational risk?

That is the dark-factory idea behind Marina: not eliminating you, but moving you from operating every task to designing and governing the system that continuously performs them.
