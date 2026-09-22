# What is a dark software factory?

A dark factory is an always-on production system that completes normal work
without people continuously operating it. For software, that means:

> An evidence-gated system that turns bounded intent or trusted signals into
> verified outcomes within explicit policy, and escalates decisions outside its
> authority.

“Dark” means unattended. It does not mean opaque, unaccountable, or unlimited.

## Two loops

```text
Human governance
intent -> architecture -> policy -> priorities -> exceptional decisions
                              |
                              v
Factory operation
sense -> plan -> execute -> verify -> deliver -> observe -> recover
  ^                                                           |
  +-----------------------------------------------------------+
```

Humans remain responsible for product direction, architecture, acceptable risk,
evidence standards, and decisions that must never be delegated. The factory owns
the repeatable engineering work already authorized by those choices.

Deck owns the preparation flow between these loops: prioritize backlog outcomes,
break them into focused verifiable tasks, select their context, and record bounded
authorization. Marina runs planning assistance and executes eligible work. This
accepted direction is specified in
[decision 0018](docs/decisions/0018-deck-work-preparation.md) and remains planned.
Release work at the rate verification and review can support; small tasks must
still preserve coherent architectural boundaries.

## What Marina contributes

| Factory concept | Platform component |
| --- | --- |
| Control room | Deck |
| Control system | Marina |
| Factory floor | Kubernetes |
| Work cell | Isolated worker Job |
| Machine and tooling | Agent harness and sandbox |
| Work order | Task, acceptance criteria, authority, and budget |
| Materials | Pinned repository and immutable context |
| Inspection | Tests, CI, policy checks, and independent validation |
| Safety interlocks | Identity, leases, fences, limits, and cancellation |
| Finished output | Evidence-backed result for human review |

An individual agent is one replaceable worker inside the factory. A cron job,
terminal agent, generated patch, or automatic merge does not by itself provide
durable authority, recovery, and evidence.

## Scoped autonomy

Autonomy is granted by repository, workload, change class, environment, and risk.
A factory may handle dependency updates, documentation fixes, or known defect
patterns unattended while still escalating architecture changes, destructive
migrations, security-boundary changes, and production incidents.

For example, a bounded vulnerability update can be implemented in an isolated
worker, checked at the exact source revision, returned as a pull request, and
blocked if tests, CI, authority, or budget fail. A migration requiring product or
data judgment returns to the operator. Correct escalation is successful factory
behavior.

The useful measure is verified value per minute of human attention without added
operational risk. Marina's goal is to move the operator from running every task
to designing and governing the system that runs them.
