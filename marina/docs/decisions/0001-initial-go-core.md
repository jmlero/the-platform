# 0001: Use Go for the Initial Marina Core

- Status: Accepted as a reversible starting decision
- Date: 2026-07-17

## Context

Marina needs a small always-on control service, clear adapter boundaries, reliable concurrency, and a natural path to Kubernetes. No implementation language was inherited from prior work, and choosing one must not predetermine the agent harness or provider ecosystem.

## Decision

Build the initial Marina control service in Go 1.26, using the standard library first and adding dependencies only when a proven vertical-slice requirement justifies them.

This decision applies to the initial control service, not to every future runtime adapter or agent harness.

## Why this is a reasonable first move

- Go produces one small deployable binary with straightforward process and concurrency behavior.
- Kubernetes tooling and conventions are well supported.
- AppDeck is currently implemented in Go, reducing incidental friction while the integration protocol is established.
- The standard library is sufficient for the first HTTP service, AppDeck adapter, configuration, and tests.
- The choice can be evaluated through working code before the system accumulates a large dependency surface.

## Consequences

- The first build and test workflow requires Go 1.26.
- Core contracts must remain protocol-oriented so runtime adapters can use different languages or deployment systems.
- Sharing a language with AppDeck must not become code-level coupling or shared persistence.
- Provider SDKs and Kubernetes client libraries are deferred until a concrete adapter requires them.

## Revisit when

Reconsider this decision before the codebase becomes large if Go makes durable workflow modeling, runtime integration, agent-harness interoperability, or iteration materially harder than a credible alternative. Use measured implementation evidence rather than preference alone.
