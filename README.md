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

## Supported workflows

The repository workflow turns one authorized task into either a verified pull
request for human review or a truthful blocker:

```text
task and budget -> approved plan -> coding Job -> verification Job
                -> pull request and CI -> evidence in Deck
```

It supports one repository and one active task, with one implementation attempt
and one separate deterministic verification attempt. Cancellation waits for
worker termination. Failed checks, changed pull-request heads, and requested
changes block. Pause/resume, automatic revision loops, semantic review, merge,
and deployment authority are outside the supported workflow.

The same installation also supports one pinned diagnostic-analysis profile. It
runs a LangChain worker against a fixed immutable fixture, permits one governed
read-only context tool, and independently validates the report. This is a bounded
second workload, not general agent hosting.

Both workflows passed local Podman/Kind acceptance. See the
[repository-workflow report](docs/validation/poc-mvp-2026-09-21.md), the
[two-workload MVP report](docs/validation/agent-11-13-2026-09-22.md), and the
[validation index](docs/README.md).

## Start here

- [VISION.md](VISION.md): product direction and scope
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
