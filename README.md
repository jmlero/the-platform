# The Platform

The Platform is the monorepo for AppDeck and Marina, two independently deployable products that form one governed autonomous-software system.

- [AppDeck](deck/README.md) is the human-facing work-control surface and authoritative task, decision, command, event, budget, and evidence ledger.
- [Marina](marina/README.md) is the always-on factory engine that reconciles bounded work, durable execution, policy, runtimes, verification, and delivery.

They evolve together through a versioned HTTP/JSON contract while retaining separate binaries, Go modules, container images, databases, service identities, deployment lifecycles, and product responsibilities. Neither product imports the other's internal packages.

Read [ARCHITECTURE.md](ARCHITECTURE.md) for the joint boundaries and failure semantics, [TODO.md](TODO.md) for the ordered implementation backlog, and [the monorepo decision](docs/decisions/0001-product-boundary-monorepo.md) for why the source trees were combined.

An illustrative pre-monorepo architecture export is preserved under [docs/architecture/](docs/architecture/); [ARCHITECTURE.md](ARCHITECTURE.md) is authoritative for current packaging and boundaries.

## Development

Requirements:

- Go 1.26+
- Node.js 24+
- npm 11+

Run the complete deterministic checks from the repository root:

```bash
make check
```

Exercise Marina's consumer contract against a freshly built real AppDeck process and an isolated database:

```bash
make conformance
```

Other useful commands:

```bash
make build
make test
make vet
make fmt
```

Product-local Makefiles remain usable from `deck/` and `marina/`. The root [`go.work`](go.work) provides editor and local workspace integration without turning the products into one Go module.

## Layout

```text
deck/                         AppDeck application, frontend, API, and database
marina/                       Marina control service, journal, and controllers
deck/api/                     Canonical AppDeck work-control OpenAPI and v1 fixtures
marina/internal/workcontrol/  Provider-neutral consumer ports and conformance suite
docs/                         Cross-product decisions and architecture artifacts
ARCHITECTURE.md               Joint source-of-truth boundaries and failure semantics
TODO.md                       Prioritized cross-product delivery backlog
go.work                       Local workspace over two independent Go modules
```

The first proof remains conservative: one bounded task should produce one truthful merge-ready pull request or focused escalation while surviving process and workload restarts. Runtime and source-control mutation remain disabled until the Milestone 1 control-system safety gate is complete.
