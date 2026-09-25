# The Platform

The Platform is an operator-controlled system for running bounded agent work on
Kubernetes. It has three independently built components:

- [Deck](deck/README.md) is the human-facing task, decision, command, and evidence
  ledger.
- [Marina](marina/README.md) executes authorized work, recovers it across
  restarts, and publishes results back to Deck.
- [platformctl](cli/README.md) is a stateless client for Deck's public HTTP/JSON
  API.

Deck and Marina use separate binaries, stores, identities, and deployments.
They integrate only through the versioned contract described in
[contracts/README.md](contracts/README.md).

## Current state and vision

[VISION.md](VISION.md) is the reference for what the platform does today and
where it is going.

## Start here

- [VISION.md](VISION.md): current state, vision and goal (the reference)
- [ARCHITECTURE.md](ARCHITECTURE.md): ownership, invariants, lifecycle, and
  failure behavior
- [TODO.md](TODO.md): active backlog and completed milestone index
- [Local Podman/Kind](deploy/kubernetes/local-podman/README.md): development
  installation
- [Repository workflow](deploy/kubernetes/first-release/README.md): configuration
  and acceptance
- [Two-workload installation](deploy/kubernetes/agent-platform/README.md):
  diagnostic profile and upgrade path

Consequential choices are recorded under [docs/decisions/](docs/decisions/).
Those records explain why a choice was made; the documents above describe the
current system.

## Development

Requirements: Go 1.27+, Node.js 24+, npm 11+, Python 3, and OpenSSL.

```bash
make check
make conformance
```

`make check` runs the deterministic product and installation checks.
`make conformance` builds a real Deck process and exercises it through the CLI
and Marina consumer contracts. It does not replace live model, Git, Kubernetes,
or browser acceptance.

Other useful commands are `make build`, `make test`, `make vet`, and `make fmt`.
The root `go.work` joins three independent Go modules; it does not create a root
module.

## Repository layout

```text
cli/                          platformctl
deck/                         work-control service and web UI
deck/api/                     canonical OpenAPI and wire fixtures
marina/                       execution service and worker harnesses
marina/internal/workcontrol/  provider-neutral consumer contract
deploy/kubernetes/            installation profiles and runbooks
docs/decisions/               architecture and product decisions
docs/validation/              redacted acceptance evidence
```
