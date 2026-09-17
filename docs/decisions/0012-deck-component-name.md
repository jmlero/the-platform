# 0012: Deck and Marina component names

- Status: Accepted
- Date: 2026-09-17

## Decision

The Platform has two main components: **Deck** and **Marina**. Deck owns human
steering and the work-control ledger; Marina owns execution and reconciliation.
`platformctl` remains a supporting operator client, not a third main component.

Use `Deck` in prose and UI, `deck` in paths, packages, commands, provider identity,
images and Kubernetes resources, and `DECK_` for configuration. Marina uses
`MARINA_DECK_` for its provider configuration. The CLI and Marina accept
`--deck-url`; Marina also accepts `--deck-token-file`.

This is a coordinated naming change to both services and their clients. The
HTTP routes, capabilities and lifecycle semantics remain version 1. Provider
discovery now returns `deck`; new exports identify as `deck-export/v1`.
Deploy the corresponding component versions together. Older clients that require
the previous provider identity are not compatible with the renamed provider.

The previous product spelling is retained only in upgrade handling, its tests,
and this migration record. It is not an alternate product name. Immutable
operator data, audit records and historical artifact URIs are not rewritten.

## Existing installations

Repository edits do not migrate a running installation. Before updating one:

1. Stop Marina and Deck and retain a consistent copy of both databases, their
   SQLite WAL files, artifact storage, and private configuration. Keep the same
   service/operator credentials and installation identities.
2. Replace `APPDECK_` environment keys with `DECK_`, and `MARINA_APPDECK_` with
   `MARINA_DECK_`. Replace old CLI flags and binary paths with the names above.
   Old environment names fail with an actionable error instead of silently
   dropping authentication or selecting a different server.
3. With all SQLite connections closed, move `appdeck.db` to `deck.db` within
   the existing Deck data volume. Move any `appdeck.db-wal` and `appdeck.db-shm`
   sidecars to the corresponding `deck.db-wal` and `deck.db-shm` paths together.
   Do not overwrite an existing destination or discard a WAL. Resolve any
   competing database copies explicitly before starting either service. Deck
   refuses to start if any previous database filename remains in its data
   directory, preventing an accidental empty ledger.
4. Update private deployment overlays, service DNS, TLS certificates and SANs,
   Secret references, labels/selectors, ingress and image names together. Fresh
   manifests use `deck`, `deck-config`, `deck-auth`, `deck-tls` and `deck-data`.
   **Reuse the existing persistent volume:** Kubernetes PVC names cannot be
   renamed in place. Patch the new Deployment's claim reference to the existing
   claim, or arrange an explicit offline volume migration. Likewise, preserve
   the existing Compose data volume when its service/volume keys change. Do not
   apply fresh-install defaults over an existing installation and accept an
   empty replacement volume.
5. Start the matching versions. Deck recognizes the renamed ledger migration
   without rerunning its DDL and renames only the unchanged built-in project.
   Marina migrates provider identifiers in bindings and executions without
   changing their IDs, versions, leases or checkpoints. Mixed deployments that
   already contain duplicate old/new provider bindings require manual inspection;
   uniqueness conflicts fail transactionally rather than merging work.
6. Sign in again: browser and CLI cookies now use `deck_operator_session` and
   `deck_operator_csrf`. Check readiness, retained task/run IDs, artifact access
   and Marina recovery before re-enabling intake.

Existing `appdeck-export/v1` bundles remain importable through the normal
empty-project import boundary. New exports always use the Deck schema name.
This is not a raw string replacement of stored task content or evidence.

Documentation examples use the new names, including in earlier decision and
validation records. Those edits do not constitute fresh deployment evidence.

## Verification

Regression tests cover the provider's canonical name, generated installation
names/certificates, prior migration metadata, export compatibility, provider
binding/execution identity, and refusal to ignore old configuration or data.
On 2026-09-17, `make check`, `make build` and `make conformance` passed, including
the real-process CLI session and Marina repository-workflow suites. The build
refreshed the embedded frontend. The base and both paired Kubernetes profiles
rendered successfully; complete temporary installation overlays also rendered,
and their certificates matched `deck.marina-system.svc`. No running installation
was changed by these checks.
