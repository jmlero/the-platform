# Working in The Platform

## Scope

This file applies to the complete monorepo. More specific instructions in a product directory add to these rules.

## Read first

Before planning or changing cross-product behavior, read:

1. [README.md](README.md)
2. [ARCHITECTURE.md](ARCHITECTURE.md)
3. [TODO.md](TODO.md)
4. The affected product's README and vision
5. [marina/AGENTS.md](marina/AGENTS.md) for any Marina change

## Product boundaries

- `deck/` owns AppDeck: human steering and the authoritative work-control ledger.
- `marina/` owns Marina: durable autonomous execution and reconciliation.
- Keep separate Go modules, binaries, databases, images, identities, deployments, migrations, and release versions.
- Neither product may import the other's internal packages. Integration goes through the versioned HTTP/JSON contract.
- AppDeck owns its provider-specific OpenAPI and canonical wire fixtures under `deck/api/`.
- Marina owns provider-neutral ports and the reusable consumer conformance suite under `marina/internal/workcontrol/`.
- A joint contract item is complete only when both product checks and the real-process conformance test pass.

## Workflow

- Preserve unrelated user changes and inspect the complete root Git state before editing.
- Make cross-product contract changes atomically in one commit when they must remain compatible.
- Keep future schemas visibly marked as planned; do not advertise a provider capability before its behavior exists.
- Run the narrowest relevant checks first, then `make check` and `make conformance` for joint protocol changes.
- Update [TODO.md](TODO.md) only when acceptance criteria are genuinely satisfied.
- Record consequential cross-product decisions under `docs/decisions/`.

## External effects

Do not push, create a remote, deploy, merge, modify production, or send external messages unless the user explicitly requests it. Never commit credentials, local databases, generated dependency trees, runtime data, or `.codex/` artifacts.
