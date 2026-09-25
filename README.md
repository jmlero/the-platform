# The Platform

An autonomous software factory on Kubernetes you control. You shape the product
in a chat per project, and PRDs come out of those conversations. Agents turn the
resulting tasks into tested code on main with minimal supervision, learning from
failed checks.

It has three independently built components:

- [Deck](deck/README.md): the web UI (projects, what is happening, chat) and the
  work ledger.
- [Marina](marina/README.md): runs authorized work, recovers it across restarts
  and publishes results back to Deck.
- [platformctl](cli/README.md): a stateless command-line client for Deck's API.

**Documentation starts at [docs/INDEX.md](docs/INDEX.md).** Read
[VISION](docs/VISION.md) for the what and why, and
[ARCHITECTURE](docs/ARCHITECTURE.md) for the how.

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
