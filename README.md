# The Platform

The Platform is the monorepo for AppDeck and Marina, two independently deployable products that form one governed autonomous-software system, plus a standalone operator client.

- [AppDeck](deck/README.md) is the human-facing work-control surface and authoritative task, decision, command, event, budget, and evidence ledger.
- [Marina](marina/README.md) is the installable, Kubernetes-native, and extensible factory layer that reconciles bounded work, durable execution, policy, runtimes, verification, and delivery.
- [Platform CLI](cli/README.md) is the stateless `platformctl` operator client for AppDeck's public HTTP/JSON API.

They evolve together through a versioned HTTP/JSON contract while retaining separate binaries, Go modules, container images, databases, service identities, deployment lifecycles, and product responsibilities. Neither product imports the other's internal packages.

Read the [Marina vision](VISION.md), [AppDeck vision](VISION_DECK.md), and [dark-factory definition](DARK_FACTORY.md) for product direction; [ARCHITECTURE.md](ARCHITECTURE.md) for joint boundaries and failure semantics; and [TODO.md](TODO.md) for the ordered implementation backlog. Consequential cross-product choices are recorded in the [monorepo decision](docs/decisions/0001-product-boundary-monorepo.md), [operator CLI decision](docs/decisions/0002-standalone-operator-cli.md), [service-authentication decision](docs/decisions/0003-service-authentication.md), and [Milestone 1 safety decision](docs/decisions/0004-milestone-one-work-control-safety.md).

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

Exercise Marina's consumer contract against a freshly built real AppDeck process, isolated database, and ephemeral scoped service credential:

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

Project-local Makefiles remain usable from `cli/`, `deck/`, and `marina/`. The root [`go.work`](go.work) provides editor and local workspace integration without turning the projects into one Go module.

## Layout

```text
cli/                          Stateless platformctl operator client
deck/                         AppDeck application, frontend, API, and database
marina/                       Marina control service, journal, and controllers
deck/api/                     Canonical AppDeck work-control OpenAPI and v1 fixtures
marina/internal/workcontrol/  Provider-neutral consumer ports and conformance suite
docs/                         Cross-product decisions and architecture artifacts
VISION.md                     Marina product vision
VISION_DECK.md                AppDeck product vision
DARK_FACTORY.md               Operational definition of the north star
ARCHITECTURE.md               Joint source-of-truth boundaries and failure semantics
TODO.md                       Prioritized cross-product delivery backlog
go.work                       Local workspace over three independent Go modules
```

Milestone 1's control-system safety gate is complete: Marina can durably acquire one bounded AppDeck task, recover ambiguous handoffs, enforce fenced authority, consume operator commands, and project ordered state without duplicates. Runtime and source-control mutation remain disabled while Milestone 2 introduces isolated worker attempts.
