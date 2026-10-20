# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

One user: the operator who owns The Platform. They steer projects, explore
ideas with the project agent in chat, approve PRDs and accept results against
them. They do not review code. No other users, teams or permissions are planned
([VISION](../../docs/VISION.md)).

## Product Purpose

Deck is the operator surface of an autonomous software factory. The operator
shapes a product in conversation; agents running on Marina turn the resulting
tasks into tested code on main. Success is verified value per minute of the
operator's attention, without added risk.

## Positioning

Deck is a steering surface, not a code review tool. Work lands on main without
pull requests once tests and an independent review pass on that exact commit,
so the UI shows intent, progress, evidence and outcomes rather than diffs. It is
one client of Marina's platform API and owns no execution.

## Operating Context

- Runs on infrastructure the operator controls: today local Kubernetes on a Mac,
  reached at `https://localhost:8443`.
- One project is one Git repository. PRDs live in that repository.
- Three columns: projects; what is happening (in progress, to do, recently
  landed); the project chat.
- Acceptance happens at the PRD level: does the product now do what the PRD
  describes?

## Capabilities and Constraints

What works today is in [docs/status.md](../../docs/status.md); Deck's current
behavior is in [deck/README.md](../README.md). Durable constraints:

- Show only real state. Agent and provider availability come from data; never
  suggest a saved message invoked an absent worker.
- Never show a control for an action Marina does not offer (Marina offers
  cancel only).
- A task is done only when it lands with evidence; an agent saying so is not
  evidence.
- No unfinished UI: no controls, links or placeholders for unavailable features.
- Plain domain language: projects, tasks, PRDs, decisions, results, evidence.

## Brand Commitments

Product names are The Platform, Deck and Marina. Deck's existing visual system
is Harbor, recorded in [DESIGN.md](DESIGN.md). Maritime character stays in the
brand and never in status or control labels.

## Evidence on Hand

Real projects, tasks and run evidence exist only in the operator's local
installation. There are no customers, testimonials or benchmarks; do not
invent them.

## Product Principles

1. Attention is the scarce resource: surface what needs a decision, keep the
   rest quiet.
2. Evidence over claims: every outcome links to the proof behind it.
3. Conversation first: ideas, PRDs and results stay together in one thread per
   project.
4. Honest state: show what is true now, including blocked, failed and stale.
5. Small and inspectable: prefer fewer screens and controls over more.
