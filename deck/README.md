# Deck

Deck is the operator surface and authoritative work-control ledger for tasks run
by Marina. It stores human intent, budgets, runs, plans, decisions, commands,
events, evidence metadata, project context, and conversation history in SQLite.
It does not execute workers or own Marina's journal.

The default workspace is a project conversation, with Tasks and Context tabs.
Each new project has one channel and one GitHub repository association request.
The compact task list shows Backlog, Ready, In progress and Done. Successful
execution appears as Awaiting review until the operator accepts its result.
Operators can create and authorize tasks, approve a plan, cancel a run, inspect
its immutable inputs and usage, open its pull request, and download
digest-checked evidence.

Use the [paired workflow guide](../deploy/kubernetes/first-release/README.md) for
repository work and the
[two-workload guide](../deploy/kubernetes/agent-platform/README.md) for the pinned
diagnostic profile. [VISION.md](../VISION.md) describes the current state and
product direction; [ARCHITECTURE.md](../ARCHITECTURE.md) defines the Deck–Marina boundary.

## Current behavior

Deck provides:

- persistent projects, tasks, structured budgets, runs, plan revisions,
  decisions, commands, events, channels, artifacts, and evidence;
- optional operator sessions with CSRF-protected browser mutations;
- scoped service identities and explicit project/capability grants;
- atomic claims, lease expiry, monotonic versions, fence tokens, and idempotent
  replay of successful mutations;
- immutable task, acceptance-criteria, workload-profile, repository, and context
  selections for active runs;
- project-authored context sources and selected conversation excerpts;
- versioned repository-association requests reconciled by Marina;
- live server-sent event updates and paginated channel history;
- deterministic project export/import; and
- an embedded Svelte frontend in the Go binary.

The repository workflow supports cancellation and ends at a verified PR for human
review or a blocker. Deck can record additional command types, but the current
Marina workflow rejects pause/resume, replan, retry, and authority reduction.
Deck also exposes installed workload discovery and idempotent invocation for the
bounded repository and diagnostic profiles.

## Quick start

Requirements: Go 1.26+, Node.js 24+, and npm 11+.

```bash
make setup
make run
```

Open [http://localhost:8080](http://localhost:8080). A new development database
seeds a demo workspace and the Atlas demo agent. Runtime data defaults to
`./data/deck.db`.

For separate backend and frontend processes:

```bash
make dev-api
make dev-web
```

The Vite frontend runs at [http://localhost:5173](http://localhost:5173) and
proxies the API on port 8080.

Useful commands:

```bash
make check
make test
make build
make image
make manifests
```

The frontend design tokens and interaction rules are in
[web/DESIGN.md](web/DESIGN.md).

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DECK_ADDR` | `:8080` | HTTP listen address |
| `DECK_DATA_DIR` | `./data` | Directory containing `deck.db` |
| `DECK_DEMO_AGENT` | `true` | Seed demo tasks and run Atlas |
| `DECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `DECK_SERVICE_AUTH_FILE` | unset | Service identities and capability grants |
| `DECK_OPERATOR_AUTH_FILE` | unset | Operator IDs and token fingerprints |
| `DECK_OPERATOR_COOKIE_SECURE` | `true` | Require HTTPS for session cookies |

When operator authentication is enabled, the policy contains a stable operator
ID and one or more SHA-256 fingerprints of tokens with at least 256 bits of
entropy. Login exchanges the raw token for a 12-hour server-side session. Deck
persists only digests of session and CSRF secrets, reloads the policy for each
login/readiness check, and supports overlapping fingerprints during rotation.
Keep the raw value in a secret file or manager. Use unauthenticated mode only for
trusted loopback development. See
[decision 0005](../docs/decisions/0005-operator-authentication.md).
The local Podman/Kind profile uses this mode at the operator's request and opens
directly without sign-in. Its browser header has no import/export or session
controls.

Service authentication uses the same fingerprint pattern plus explicit project
and capability grants. Deck reloads the policy on every request, rejects
wildcards, and requires TLS outside loopback. Marina's credential cannot create
an operator session. See
[decision 0003](../docs/decisions/0003-service-authentication.md).

## Public API

The canonical machine contract is [api/openapi.yaml](api/openapi.yaml).
`GET /api/v1/provider` publishes the protocol version, server time, and
implemented capabilities.

Every claim and run mutation requires `Idempotency-Key`. Deck stores the key,
normalized request digest, and successful result in the same transaction as the
state change. An exact retry returns the original result; a changed payload
returns `409 conflict`. Active-run mutations also require the stable logical
agent ID, exact run version, and current fence token.

The normal service lifecycle is:

1. register a logical agent and inspect provider capabilities;
2. discover and atomically claim ready work;
3. renew the lease and submit an immutable plan revision;
4. wait for any revision-bound decision and start the approved run;
5. publish progress, usage, operational state, artifacts, and evidence; and
6. complete or block using the current evidence IDs.

Commands are durable records. Marina consumes them after a persisted cursor and
acknowledges each result idempotently. Projected events are deduplicated by
producer ID and execution sequence.

Repository tasks require acceptance criteria, at least two attempts, and explicit
`source.read`, `source.write`, and `pull_request.create` authority before
entering Ready. Drafts can remain in Backlog. This admission check does not
provide credentials or guarantee cluster/model readiness.

Channel messages can become Backlog drafts before criteria are complete.
Readiness remains the authorization point, with acceptance criteria, budgets and
permissions checked before eligible repository execution. Dependencies must be
accepted; code prerequisites also require explicit confirmation of base-branch
availability. The composer can record explicit agent requests for the optional
conversation profile. Automatic summary maintenance remains planned. Workload invocation
similarly selects an installed immutable profile and creates one task through an
idempotent operator request.

## Conversation threads and invocation

Deck stores durable threads within existing channels. A thread references one
canonical root message and its replies; the channel still contains the same
messages. One thread can originate multiple tasks, and each linked task retains
one originating thread. Linking does not authorize work or change the task's
selected context. Promoting a message creates or reuses its thread and links the
new task in the same transaction.

Operator-session endpoints provide the persistence boundary for the planned UI:

- `POST /api/v1/operator/channels/{id}/threads` with `root_message_id` creates or
  recovers the message's thread. A reply resolves to its existing thread.
- `GET /api/v1/operator/channels/{id}/threads` lists threads.
- `GET /api/v1/operator/threads/{id}` returns its canonical task links.
- `GET /api/v1/operator/threads/{id}/messages` pages the root and replies.
- `POST /api/v1/operator/threads/{id}/tasks` with `task_id` links existing work;
  repeating the same pair succeeds, and assigning a different origin conflicts.
- `GET /api/v1/operator/tasks/{id}/thread` recovers a task's origin.

Append a reply through the existing channel message endpoint with `thread_id`.
It must belong to that channel. Threads, message identities, pagination cursors,
and task origins persist across operator sessions, restart, archive, and project
export/import. Upgrade and legacy import recover existing explicit promotion
links; unrelated channel messages remain unthreaded. Linked drafts must be
archived rather than deleted. Thread linkage does not automatically select
additional messages for an execution snapshot.

CHAT-03 adds a focused browser thread view and `POST
/api/v1/operator/channels/{id}/invocations`. A service registers a logical agent
with an installation ID and `conversation.invoke.{project_id}` capability before Deck offers
it in the composer. The first invocation fixes that agent for the thread;
subsequent Send actions address it without another mention. A leading `@name`
can select a configured agent. The API records the human message, thread, agent
selection, and request ID atomically. Retrying the same request ID recovers the
same message; changing the request conflicts. An existing human message can also
be explicitly invoked. Thread URLs restore the view after reload, and selected
agents and invocation records survive project export/import.

Marina reads explicit requests through `GET /api/v1/conversation/invocations`
with `project_id` and an exclusive `after_id` event cursor. The service identity
needs `conversation.invocation.list` and that project's grant. Event order also
covers invoking a message posted earlier in the channel; each item includes the
canonical message and invocation time.

Invocation is an operator intent record, not a task or model response. An
optional Marina profile can consume it through a bounded read-only kagent turn
and retain the result, but that profile is not installed in the regular local
environment. When its engine asks a typed question, Marina posts it through
`POST /api/v1/conversation/questions` with the invoked message and engine
question IDs. Deck records one agent question in the originating thread.
`GET /api/v1/operator/threads/{id}/questions` restores pending questions;
`POST /api/v1/operator/conversation/questions/{id}/answer` appends one human
reply and rejects a changed or second answer. Marina recovers that exact answer
through its replay-safe question request and sends it to the waiting attempt.
Answering does not authorize a task. The service needs
`conversation.question.write` and the project grant. Marina publishes one
terminal reply and optional task through `POST /api/v1/conversation/outcomes`.
Ready work uses `POST /api/v1/conversation/delegations` with the separate
`conversation.task.delegate` capability, installed repository profile and
explicit budget. Deck links the task to the originating thread and mirrors
selected run events there with canonical evidence references. A linked follow-up
creates new work from feedback and leaves the completed task intact. Local
engine and browser acceptance remains pending; see
[CHAT-06–08 evidence](../docs/validation/chat-06-08-2026-09-25.md).

Run `make -C deck conformance` from the repository root for the temporary
real-process, two-session persistence check; root `make conformance` includes it.
See [CHAT-02 evidence](../docs/validation/chat-02-2026-09-23.md).

## Delete and archive tasks

Task details offer **Delete draft** for unexecuted Backlog tasks, with a permanent
deletion confirmation. Ready tasks must first return to Backlog. Tasks with runs
cannot be deleted. Drafts linked to channel promotion or workload invocation must
be archived instead, preserving their replay identity.

**Archive task** hides inactive work from the overview, board and backlog while
retaining its status, runs, PR links, evidence and history. Active runs must stop
before archiving; Ready work must first return to Backlog. The **Archived** view
supports search, inspection and **Restore task**. Restore preserves the original
status and does not authorize execution. Both actions check the current task
version; archived tasks cannot be edited, requeued or claimed until restored.
Archive state survives restart and project export/import.

## Project context

The **Project context** view stores versioned instructions, goals, authored
documents, exact-commit Git file references, and approved Deck decision
references. Only explicit operator approval marks a source as guidance.
Conversation excerpts remain task data.

When a task becomes Ready, Deck freezes selected source and message revisions.
The task's **Sources** view shows the interpreted outcome, repository binding,
immutable worker snapshot, provenance, trust, and omissions. Revision,
deactivation, and revocation have distinct meanings; active work keeps its
recorded revisions, while revocation denies further use.

Marina owns repository configuration and validates association requests. Changing
the repository never grants source-write or PR authority. See
[decision 0014](../docs/decisions/0014-project-repository-and-knowledge.md).

## Deployment and boundary

The container runs unprivileged and stores SQLite at `/data`. The Kubernetes
base under [deploy/kubernetes/](deploy/kubernetes/) uses one replica,
`ReadWriteOnce` storage, and `Recreate` updates. Choose immutable images,
storage, TLS, ingress, authentication, and backup in a private overlay. Do not
share the SQLite file between replicas.

Deck stores artifact metadata and proxies authorized downloads; Marina owns
workflow artifact bytes. Deck does not schedule runtimes, mutate source, call
models, or read Marina's database. Multi-user permissions, federated identity,
HA, automatic task decomposition, and merge/deploy authority are outside the
current release.

## Layout

```text
cmd/deck/          application entry point
internal/domain/   ledger model
internal/store/    SQLite repositories and migrations
internal/httpapi/  operator and service HTTP/SSE APIs
internal/demo/     Atlas development agent
internal/webui/    embedded frontend
web/               SvelteKit application
api/               OpenAPI and canonical fixtures
```
