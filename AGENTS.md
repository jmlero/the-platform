# Working in The Platform

## Scope

This file applies to the complete monorepo. More specific instructions in a product directory add to these rules.

## Read first

Before planning or changing cross-product behavior, read:

1. [README.md](README.md)
2. [ARCHITECTURE.md](ARCHITECTURE.md)
3. [TODO.md](TODO.md)
4. The affected product's README and root vision: [VISION_DECK.md](VISION_DECK.md) for Deck or [VISION.md](VISION.md) for Marina
5. [marina/AGENTS.md](marina/AGENTS.md) for any Marina change

## Product boundaries

- `cli/` owns `platformctl`: a stateless operator client that uses Deck's public HTTP/JSON API and owns no authoritative platform state.
- `deck/` owns Deck: human steering and the authoritative work-control ledger.
- `marina/` owns Marina: durable autonomous execution and reconciliation.
- Keep separate Go modules, binaries, databases where applicable, images, identities, deployments, migrations, and release versions.
- Neither product may import the other's internal packages. Integration goes through the versioned HTTP/JSON contract.
- Terminal clients must not import product internals or read product databases; they interact through supported public APIs.
- Deck owns its provider-specific OpenAPI and canonical wire fixtures under `deck/api/`.
- Marina owns provider-neutral ports and the reusable consumer conformance suite under `marina/internal/workcontrol/`.
- A joint contract item is complete only when both product checks and the real-process conformance test pass.

## Current supported behavior

- The supported Deck–Marina pair uses HTTP/JSON and separate stores. Headless and in-process packaging remain future work.
- The repository workflow ends at a PR ready for human review or a blocker, with one implementation and one separate deterministic verification attempt. It supports cancellation; pause/resume, automatic revision loops, independent semantic review, merge and deploy authority remain future work.
- Distinguish Deck's command records and Marina's reusable foundations from behavior supported by the configured workflow.

## Workflow

- Preserve unrelated user changes and inspect the complete root Git state before editing.
- Make cross-product contract changes atomically in one commit when they must remain compatible.
- Keep future schemas visibly marked as planned; do not advertise a provider capability before its behavior exists.
- Run the narrowest relevant checks first, then `make check` and `make conformance` for joint protocol changes.
- Update [TODO.md](TODO.md) only when acceptance criteria are genuinely satisfied.
- Record consequential cross-product decisions under `docs/decisions/`.

## Local Kubernetes testing

- The user authorizes using their local Podman installation and Kubernetes for development testing. Use the existing [local Podman/Kind profile](deploy/kubernetes/local-podman/README.md) for relevant integration and runtime checks; an always-on host is not a prerequisite for local validation.
- Inspect and reuse the dedicated `kind-marina-local` context on `podman-machine-default-root`. Preserve unrelated containers, contexts, credentials, and persistent data.
- Record local test evidence separately from always-on acceptance. Local Kubernetes can validate Jobs, persistence, recovery, cancellation, and network isolation; the laptop-off proof requires a host independent of the Mac.

## External effects

Do not push, create a remote, deploy, merge, modify production, or send external messages unless the user explicitly requests it. Never commit credentials, local databases, generated dependency trees, runtime data, or `.codex/` artifacts.

<!-- harness-workshop:start block/ponytail -->
<!-- harness-workshop:source block/ponytail@1.0.0 integrity sha256-8ozvvuvXP6Df83cMrc2DMjIzolw40npIkxiog5OXA9w= -->
## Minimal implementation

- Choose the smallest safe change that fully meets current requirements.
- Reuse project code, standard libraries, native features, and installed
  dependencies before adding code or dependencies.
- Avoid speculative abstractions, configuration, extension points, and wrappers.
- Fix shared root causes; preserve validation, error handling, security,
  accessibility, and data safety.
- Verify non-trivial behavior with the smallest relevant check.
<!-- harness-workshop:end block/ponytail -->

<!-- harness-workshop:start block/tdd -->
<!-- harness-workshop:source block/tdd@1.0.0 integrity sha256-54BMZaEMWeVKGxmv91DjPLn5qEtdyCv6L6WtnNQTPHY= -->
## Test-driven development

For features and bug fixes:

1. Write a failing test that describes one expected behavior.
2. Make the smallest change that passes it.
3. Refactor while keeping the tests green.

Keep tests independent, name them by behavior, mock external boundaries when
appropriate, and run the relevant checks after each change.
<!-- harness-workshop:end block/tdd -->
