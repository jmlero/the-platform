# Working on Deck

## Scope

This file applies to `deck/`. Root [AGENTS.md](../AGENTS.md) also applies.

## Read first

Before changing Deck, read:

1. [README.md](README.md)
2. [../docs/INDEX.md](../docs/INDEX.md): the documentation map, then the VISION, ARCHITECTURE and status documents it lists
3. [api/openapi.yaml](api/openapi.yaml) for operator API work and [../marina/api/README.md](../marina/api/README.md) for the Marina contract Deck consumes

## Boundary

Deck owns human steering: projects, threads, PRDs, backlog, dependencies and acceptance. Marina owns execution and its API ([decision 0033](../docs/decisions/0033-marina-platform-api.md)). Deck is a client: it creates Runs and Turns, forwards answers and cancellations, and mirrors Marina's status into its ledger through `internal/marina`. Deck serves no route for an executor, and Marina never calls Deck.

Deck does not schedule runtimes, construct Kubernetes Jobs, check out repositories, prompt models, or read Marina's journal. Do not import Marina packages; Marina's public HTTP/JSON API (`marina/api/`) is the only integration boundary.

Keep the operator OpenAPI document, fixtures, handlers and store behavior synchronized. Keep every mirror step idempotent: a replayed cycle or restart must change nothing and must never create a second Run or Turn. Record an operator action locally only after Marina accepted it.

Marina supports cancel only; Deck rejects other run commands. What is supported today is in [../docs/status.md](../docs/status.md).

## Verification

Run focused Go or frontend tests while iterating, then:

```bash
make test check
make conformance   # real Deck against Marina's API sandbox (MARINA_REPO, default ../marina)
make browser-test  # browser suite against the same sandbox
```

For a joint protocol change, return to the repository root and run:

```bash
make conformance
```
