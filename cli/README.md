# Platform CLI

`platformctl` is the small, standalone command-line client for operating The Platform through AppDeck's HTTP/JSON API. It owns no platform state and does not import AppDeck or Marina internals.

The first slice can:

- verify that AppDeck is reachable;
- list tasks from the operator workspace, optionally filtered by status or project;
- inspect one task and its run state;
- create backlog or ready work; and
- emit human-readable tables or JSON for scripts.

## Build and test

Requirements:

- Go 1.26+

```bash
make check
make build
```

The binary is written to `bin/platformctl`.

## Configuration

The CLI reads these options before the command:

| Flag | Environment | Default | Purpose |
| --- | --- | --- | --- |
| `--appdeck-url` | `APPDECK_URL` | `http://localhost:8080` | AppDeck base URL |
| `--token-file` | `APPDECK_TOKEN_FILE` | none | File containing a bearer token |
| `--timeout` | — | `10s` | Request deadline |
| `--json` | — | off | Machine-readable output |

`APPDECK_TOKEN` is also accepted when no token file is configured. Prefer a protected token file so the credential is not persisted in shell history or CLI configuration. Bearer credentials must only be sent over a protected transport outside local development.

AppDeck's current prototype still lacks human authentication. Until that boundary exists, keep both AppDeck and this operator client on a trusted local network.

## Usage

Check connectivity:

```bash
./bin/platformctl health
```

List ready work:

```bash
./bin/platformctl tasks list --status ready
```

Inspect a task and its runs:

```bash
./bin/platformctl tasks get tsk_example
```

Create ready work:

```bash
./bin/platformctl tasks create \
  --title "Add the operator CLI" \
  --description "Provide a small terminal workflow" \
  --acceptance "The task can be inspected through AppDeck" \
  --status ready \
  --priority high
```

Put global options before the command. For example:

```bash
./bin/platformctl --json --appdeck-url https://appdeck.example tasks list
```

Run `platformctl help` or `platformctl tasks help` for the concise command reference.

## Boundary

`platformctl` is a separate Go module and release artifact. Its private client mirrors only the public wire shapes it consumes and tolerates additive response fields. AppDeck remains authoritative for tasks, runs, decisions, commands, and evidence; Marina remains authoritative for durable factory execution. A future TUI should use the same public API and must not read either product's database or internal packages.
