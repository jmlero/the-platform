# Decision 0002: Add a standalone operator CLI

- Status: accepted
- Date: 2026-07-19
- Scope: terminal-based operator access

## Context

The web interface is not always the most convenient way to create work, inspect runs, or automate read-only operator workflows. Putting terminal commands inside the AppDeck binary would couple client release cadence and presentation concerns to the authoritative work-control service. Calling Marina directly would also create a second human-control boundary and risk bypassing AppDeck's ledger.

A later terminal UI may need richer interaction, but its workflow and reusable client boundary are not yet stable.

## Decision

Add a peer `cli/` project that builds the `platformctl` binary:

- The CLI is a separate Go module, binary, test target, and eventual release artifact.
- It communicates with AppDeck only through supported HTTP/JSON operator endpoints.
- It never imports AppDeck or Marina internal packages and never reads either database.
- It owns configuration and presentation only; AppDeck remains authoritative for every operator-visible fact and mutation.
- It supports endpoint and bearer-credential injection without persisting credentials in repository files.
- The first slice stays intentionally small: connectivity, task listing, task inspection, and task creation.
- Operator commands are added only after AppDeck exposes the durable command resource.
- A TUI remains a separate later project. It may consume the public API directly or a deliberately extracted public client library after the CLI boundary stabilizes.

## Consequences

Benefits:

- Terminal interaction can evolve and ship without changing AppDeck's service binary.
- Scripts receive stable JSON output while humans receive compact default output.
- All human steering still converges through AppDeck's authoritative ledger.
- The CLI provides an early consumer of the operator API without creating implementation coupling.

Costs and controls:

- Wire types currently needed by the CLI are mirrored locally and require compatibility tests against AppDeck behavior.
- Root tooling includes a third Go module and must keep it independently buildable.
- The browser uses the human session mechanism recorded in [decision 0005](0005-operator-authentication.md). The CLI's supported operator-authentication flow remains separate work, so bearer injection alone must not be mistaken for permission to reuse Marina's service credential.
- No shared client library is extracted until a second real consumer justifies that boundary.

## Reversal criteria

Fold the CLI into another project only if separate packaging creates sustained release or maintenance cost without preserving a meaningful boundary. Any change must retain API-only integration and must not give terminal clients direct access to AppDeck or Marina storage.
