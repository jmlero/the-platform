# Platform CLI

`platformctl` is the small, standalone command-line client for The Platform. It talks to Marina's platform API (`marina.dev/v1`) and to Deck's operator API over HTTP/JSON. It owns no platform state and does not import Deck or Marina internals.

With Marina, the CLI can show the installation's capabilities, create and inspect Runs and their evidence, follow a Run's events, cancel a Run, ask one agent Turn, and list and answer Decisions. See [Marina platform API](#marina-platform-api).

With Deck, the CLI can:

- verify that Deck is reachable;
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

## Marina platform API

These commands use Marina's API as described in [marina/api](../marina/api/README.md). Put the options before the command:

| Flag | Environment | Default | Purpose |
| --- | --- | --- | --- |
| `--marina-url` | `MARINA_URL` | `http://localhost:8090` | Marina base URL |
| `--marina-token-file` | `MARINA_TOKEN_FILE` | none | Protected file containing a Kubernetes token |
| `--marina-ca-file` | `MARINA_CA_FILE` | none | Extra CA bundle that signs Marina's certificate |
| `-n`, `--namespace` | `MARINA_NAMESPACE` | `default` | Namespace of the project |
| `--timeout` | none | `10s` | Request deadline |
| `--json` | none | off | Print the full API objects |

The CLI sends the token as `Authorization: Bearer`; Marina checks it with TokenReview and RBAC. Use a service account or user token, for example from `kubectl create token` for a service account bound to the `marina-factory` Role. The token file follows the same rules as Deck's: a regular file readable only by its owner (mode `0600` on Unix). `MARINA_TOKEN` is a fallback when no file is set. Marina requires HTTPS outside loopback, and the CLI never follows redirects.

```bash
export MARINA_URL=https://marina.example:8090
export MARINA_CA_FILE=/private/path/marina-ca.pem
export MARINA_NAMESPACE=factory-default
(umask 077 && kubectl -n factory-default create token factory-cli > /private/path/marina-token)
export MARINA_TOKEN_FILE=/private/path/marina-token
./bin/platformctl capabilities
```

Create a Run. The name is the idempotency key: running the same command again returns the existing Run, and a different spec under the same name fails with Marina's `AlreadyExists` message. Each `--context NAME=PATH` sends a local file as a data document with its SHA-256 digest.

```bash
./bin/platformctl runs create task-0042 \
  --project acceptance --workload land-on-main \
  --title "Reject an empty range" \
  --criteria "A unit test covers min > max and passes." \
  --minutes 60 --tokens 200000 --attempts 7 \
  --context "Project goals=docs/goals.md"
```

Inspect and steer it:

```bash
./bin/platformctl runs list
./bin/platformctl runs get task-0042
./bin/platformctl runs events task-0042 --follow
./bin/platformctl runs cancel task-0042
```

`runs events --follow` streams events and stops on Ctrl-C or when the Run is `Succeeded`, `Blocked`, `Failed` or `Cancelled`. `runs cancel NAME` without `--reason` goes to Marina; with `--reason` it remains Deck's operator command.

Ask one agent turn and read the reply. The thread and message IDs come from the Turn name, so a retry sends the same spec. The budget defaults to 15 minutes and 40000 tokens.

```bash
./bin/platformctl turns create ask-1 --project acceptance --workload chat \
  --message "Which file defines isStrictlyWithinRange?"
./bin/platformctl turns get ask-1
```

Answer the decisions Marina needs:

```bash
./bin/platformctl decisions list
./bin/platformctl decisions answer task-0042-plan-1 approve
```

API errors show Marina's Status message and reason and exit with code 1. Usage errors exit with code 2.

## Configuration

For Deck, the CLI reads these options before the command:

| Flag | Environment | Default | Purpose |
| --- | --- | --- | --- |
| `--deck-url` | `DECK_URL` | `http://localhost:8080` | Deck base URL |
| `--token-file` | `DECK_TOKEN_FILE` | none | Protected file containing an operator login token |
| `--timeout` | none | `10s` | Request deadline |
| `--json` | none | off | Machine-readable output |

Use the raw operator token configured by `DECK_OPERATOR_AUTH_FILE`, not Marina's service token. The CLI exchanges it at `POST /api/v1/operator/session`, keeps session cookies only in memory, and echoes the CSRF cookie in `X-CSRF-Token` on mutations. It attempts logout when the command ends; interrupted or unavailable logout leaves a session valid until its server expiry. No credentials or cookies are written to CLI configuration.

Prefer `--token-file /path/to/operator-token` (or `DECK_TOKEN_FILE`). On Unix the file must be regular and accessible only to its owner, for example mode `0600`; on Windows protect it with the account's file ACL. `DECK_TOKEN` remains a fallback when no file is configured. Avoid entering tokens as command arguments or into shell history. HTTPS is required outside loopback, and authenticated requests never follow redirects. An expired session fails the command without replaying a mutation; rerun intentionally with a valid token.

Without a token, the CLI supports Deck's unauthenticated local development mode. `health` calls the public health endpoint without exchanging or sending a token. For authenticated use:

```bash
export DECK_URL=https://deck.example
export DECK_TOKEN_FILE=/private/path/operator-token
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
  --acceptance "The task can be inspected through Deck" \
  --status ready \
  --priority high
```

Request a durable run command with an explicit reason:

```bash
./bin/platformctl runs cancel \
  --reason "The requested outcome is no longer needed" \
  run_example
```

The configured repository workflow supports cancellation, which waits for worker termination. Deck can also record `pause`, `resume`, `replan`, and `retry` commands, and the CLI retains those protocol operations for other consumers. Marina's current repository workflow rejects them; they do not pause/resume or revise the coding Job.

The successful response confirms that Deck durably recorded a pending command. It does not claim that Marina has already acknowledged or completed it. Use `--json` when a script needs the command ID, sequence, fence, or status.

Put global options before the command. For example:

```bash
./bin/platformctl --json --deck-url https://deck.example tasks list
```

Run `platformctl help`, `platformctl tasks help`, `platformctl runs help`, `platformctl turns help`, or `platformctl decisions help` for the concise command reference.

## Boundary

`platformctl` is a separate Go module and release artifact. Its private client mirrors only the public wire shapes it consumes and tolerates additive response fields. Deck remains authoritative for tasks, runs, decisions, commands, and evidence; Marina remains authoritative for durable factory execution. A future TUI should use the same public API and must not read either product's database or internal packages.
