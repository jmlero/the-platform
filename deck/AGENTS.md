# Working on AppDeck

## Scope

This file applies to `deck/`. Root [AGENTS.md](../AGENTS.md) also applies.

## Read first

Before changing AppDeck, read:

1. [README.md](README.md)
2. [../VISION_DECK.md](../VISION_DECK.md)
3. [../ARCHITECTURE.md](../ARCHITECTURE.md)
4. [api/openapi.yaml](api/openapi.yaml) for agent-facing protocol work

## Boundary

AppDeck owns human steering and the authoritative work-control ledger. It does not schedule runtimes, construct Kubernetes Jobs, check out repositories, prompt models, or duplicate Marina's execution journal.

Keep the OpenAPI document, advertised capabilities, canonical fixtures, handlers, store behavior, and Marina conformance expectations synchronized. Future fixture schemas do not make a behavior available; advertise a capability only after its persistence, authorization, failure semantics, and tests exist.

Do not import Marina packages. The HTTP/JSON contract is the only integration boundary.

## Verification

Run focused Go or frontend tests while iterating, then:

```bash
make test check
```

For a joint protocol change, return to the repository root and run:

```bash
make conformance
```
