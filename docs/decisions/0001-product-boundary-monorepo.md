# Decision 0001: Adopt a product-boundary monorepo

- Status: accepted
- Date: 2026-07-19
- Scope: Deck and Marina source control

## Context

Deck and Marina are distinct products and deployable services, but the first proof requires them to evolve one protocol together. Separate private repositories made atomic contract changes, canonical fixture consumption, real-binary CI, and coordinated failure testing depend on cross-repository credentials and synchronized pull requests. One operator currently owns both products and their release direction.

## Decision

Keep both products in one neutral repository with peer directories:

```text
deck/
marina/
```

The monorepo is a source-control and verification boundary, not a runtime merge:

- Each product retains its own Go module, binary, frontend or service assets, container image, database, migrations, identity, deployment, and release version.
- Neither product imports the other's internal packages.
- Deck owns the provider-specific OpenAPI contract and canonical wire fixtures.
- Marina owns provider-neutral work-control ports and the consumer conformance suite.
- Root tooling runs independent product checks and a paired real-process protocol test.
- Cross-product contract changes that must remain compatible land atomically.

The repository is expected to be published as `github.com/jmlero/the-platform`; module paths use `github.com/jmlero/the-platform/deck` and `github.com/jmlero/the-platform/marina`.

## Consequences

Benefits:

- One change can update producer, consumer, fixtures, and evidence together.
- CI can build and test both private products without a cross-repository token.
- Shared crash and compatibility tests have one revision identity.
- Architecture and roadmap documents have one joint home.

Costs and controls:

- A larger checkout and shared issue/review surface are accepted for the single-operator stage.
- Accidental implementation coupling is prevented through separate modules, no cross-internal imports, path-aware ownership, and protocol-level tests.
- Deployments remain independently releasable; one product does not require the other's database or process lifecycle.

## Reversal criteria

Split a product into its own repository if it requires materially different access control, public visibility, ownership, compliance boundaries, or an independent release organization. Any split must preserve the HTTP contract and conformance evidence rather than introduce shared internal packages.
