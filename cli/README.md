# Platform CLI

`platformctl` is the small, standalone command-line client for operating The Platform through AppDeck's HTTP/JSON API. It owns no platform state and does not import AppDeck or Marina internals.

The CLI can:

- verify that AppDeck is reachable;
- list tasks from the operator workspace, optionally filtered by status or project;
- inspect one task and its run state;
- create backlog or ready work;
- request durable pause, resume, cancel, replan, and retry commands for a run; and
- emit human-readable tables or JSON for scripts.

## Build and test

Requirements:

- Go 1.26+

```bash
make check
make build
```

The binary is written to `bin/platformctl`.

Create a versioned archive for the current operating system and architecture:

```bash
make package VERSION=v0.1.0
```

The archive is written to `dist/`. Tags matching `platformctl/v*` run the independent platformctl release workflow, which verifies the CLI, packages Linux, macOS, and Windows binaries for AMD64 and ARM64, publishes SHA-256 checksums, and creates a GitHub release. A manual workflow run builds the same artifacts without publishing a release.

## Configuration

The CLI reads these options before the command:

| Flag | Environment | Default | Purpose |
| --- | --- | --- | --- |
| `--appdeck-url` | `APPDECK_URL` | `http://localhost:8080` | AppDeck base URL |
| `--token-file` | `APPDECK_TOKEN_FILE` | none | Protected file containing an operator login token |
| `--timeout` | — | `10s` | Request deadline |
| `--json` | — | off | Machine-readable output |

Use the raw operator token configured by `APPDECK_OPERATOR_AUTH_FILE`, not Marina's service token. The CLI exchanges it at `POST /api/v1/operator/session`, keeps session cookies only in memory, and echoes the CSRF cookie in `X-CSRF-Token` on mutations. It attempts logout when the command ends; interrupted or unavailable logout leaves a session valid until its server expiry. No credentials or cookies are written to CLI configuration.

Prefer `--token-file /path/to/operator-token` (or `APPDECK_TOKEN_FILE`). On Unix the file must be regular and accessible only to its owner, for example mode `0600`; on Windows protect it with the account's file ACL. `APPDECK_TOKEN` remains a fallback when no file is configured. Avoid entering tokens as command arguments or into shell history. HTTPS is required outside loopback, and authenticated requests never follow redirects. An expired session fails the command without replaying a mutation; rerun intentionally with a valid token.

Without a token, the CLI supports AppDeck's unauthenticated local development mode. `health` calls the public health endpoint without exchanging or sending a token. For authenticated use:

```bash
export APPDECK_URL=https://appdeck.example
export APPDECK_TOKEN_FILE=/private/path/operator-token
./bin/platformctl tasks list
```

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

Create generic ready work (repository PR budgets and permissions are configured in the web task form):

```bash
./bin/platformctl tasks create \
  --title "Add the operator CLI" \
  --description "Provide a small terminal workflow" \
  --acceptance "The task can be inspected through AppDeck" \
  --status ready \
  --priority high
```

Request a durable run command with an explicit reason:

```bash
./bin/platformctl runs cancel \
  --reason "The requested outcome is no longer needed" \
  run_example
```

The configured repository workflow supports cancellation, which waits for worker termination. AppDeck can also record `pause`, `resume`, `replan`, and `retry` commands, and the CLI retains those protocol operations for other consumers. Marina's current repository workflow rejects them; they do not pause/resume or revise the coding Job.

The successful response confirms that AppDeck durably recorded a pending command. It does not claim that Marina has already acknowledged or completed it. Use `--json` when a script needs the command ID, sequence, fence, or status.

Put global options before the command. For example:

```bash
./bin/platformctl --json --appdeck-url https://appdeck.example tasks list
```

Run `platformctl help`, `platformctl tasks help`, or `platformctl runs help` for the concise command reference.

## Boundary

`platformctl` is a separate Go module and release artifact. Its private client mirrors only the public wire shapes it consumes and tolerates additive response fields. AppDeck remains authoritative for tasks, runs, decisions, commands, and evidence; Marina remains authoritative for durable factory execution. A future TUI should use the same public API and must not read either product's database or internal packages.
