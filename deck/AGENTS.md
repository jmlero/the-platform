# Working on Deck

## Scope

This file applies to `deck/`. Root [AGENTS.md](../AGENTS.md) also applies.

## Read first

Before changing Deck, read:

1. [README.md](README.md)
2. [../docs/INDEX.md](../docs/INDEX.md): the documentation map, then the VISION, ARCHITECTURE and status documents it lists
3. [api/openapi.yaml](api/openapi.yaml) for agent-facing protocol work

## Boundary

Deck owns human steering and the authoritative work-control ledger. It does not schedule runtimes, construct Kubernetes Jobs, check out repositories, prompt models, or duplicate Marina's execution journal.

Keep the OpenAPI document, advertised capabilities, canonical fixtures, handlers, store behavior, and Marina conformance expectations synchronized. Future fixture schemas do not make a behavior available; advertise a capability only after its persistence, authorization, failure semantics, and tests exist.

Do not import Marina packages. The HTTP/JSON contract is the only integration boundary.

Deck can record more command types than Marina executes. What is supported today is in [../docs/status.md](../docs/status.md).

## Verification

Run focused Go or frontend tests while iterating, then:

```bash
make test check
```

For a joint protocol change, return to the repository root and run:

```bash
make conformance
```
