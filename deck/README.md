# Deck

Deck is the operator surface for work that Marina executes: projects, chat
threads, PRDs, backlog, dependencies and acceptance. Deck is a client of
Marina's platform API ([decision 0033](../docs/decisions/0033-marina-platform-api.md),
contract in [marina/api](../marina/api/README.md)). It submits Ready tasks as
Marina Runs and chat requests as Marina Turns, forwards the operator's answers
and cancellations, and mirrors Marina's status, decisions, events and evidence
into its SQLite ledger so the browser can show them. Marina never calls Deck.

The workspace has projects, a work overview and a project conversation, with
task details and project context available alongside them. An evidenced
landing completes its task automatically and shows its exact commit. Other
completed results retain their operator acceptance flow. Operators can create
and authorize tasks, approve a plan, cancel a run, inspect immutable inputs and
download digest-checked evidence.

[VISION.md](../docs/VISION.md) describes the direction,
[status.md](../docs/status.md) the current state, and
[ARCHITECTURE.md](../docs/ARCHITECTURE.md) the Deck and Marina boundary.

## Current behavior

Deck provides:

- persistent projects, tasks, structured budgets, dependencies, channels,
  threads, PRDs, project context and acceptance records;
- optional operator sessions with CSRF-protected browser mutations;
- immutable task, acceptance-criteria and context selections frozen when a task
  becomes Ready and sent to Marina as the Run spec;
- a mirror of each Marina Run: plan revisions, decisions, progress events,
  evidence, landing, blocked, failed and cancelled outcomes;
- chat turns answered by Marina, including clarification questions and task
  proposals, published into the originating thread;
- live server-sent event updates and paginated channel history;
- deterministic project export/import; and
- an embedded Svelte frontend in the Go binary.

Marina offers cancel only; Deck rejects pause, resume, replan and retry with
409 and shows no control for them.

## Quick start

Requirements: Go 1.26+, Node.js 24+, and npm 11+.

```bash
make setup
make run
```

Open [http://localhost:8080](http://localhost:8080). Runtime data defaults to
`./data/deck.db`. Without `DECK_MARINA_URL` Deck has no executor: work stays
Ready and the provider status shows Marina as unavailable.

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

`make conformance` builds Marina's API sandbox (`MARINA_REPO`, default
`../marina`) and a real Deck, and drives Deck's operator API through plan
approval and landing, a blocked run, cancellation, a restart without duplicate
Runs, chat replies, a clarification question and a task proposal.

Run the browser suite with `make browser-test`. It builds the embedded frontend,
Deck and the Marina API sandbox, and starts both as disposable processes. On
macOS it uses installed Google Chrome. Linux setup uses
`web/node_modules/.bin/playwright install --with-deps chromium`; CI performs this
before testing, and verification images package the same browser during build.
Tests never install browsers. `PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH` can select an
existing executable; `PLAYWRIGHT_BROWSERS_PATH` selects the frozen browser cache.
The sandbox simulates no PRD drafts, so store and HTTP tests cover the PRD flows.

The frontend design tokens and interaction rules are in
[web/DESIGN.md](web/DESIGN.md).

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `DECK_ADDR` | `:8080` | HTTP listen address |
| `DECK_DATA_DIR` | `./data` | Directory containing `deck.db` |
| `DECK_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `DECK_OPERATOR_AUTH_FILE` | unset | Operator IDs and token fingerprints |
| `DECK_OPERATOR_COOKIE_SECURE` | `true` | Require HTTPS for session cookies |
| `DECK_MARINA_URL` | unset | Marina's platform API; https except on loopback. Unset means no executor |
| `DECK_MARINA_TOKEN_FILE` | `/var/run/secrets/kubernetes.io/serviceaccount/token` | Kubernetes token Deck presents; reread for every request |
| `DECK_MARINA_CA_FILE` | unset | PEM bundle trusted in addition to the system roots |
| `DECK_MARINA_PROJECTS_FILE` | unset | JSON mapping of Deck projects to Marina; reread every cycle |

The projects file maps each Deck project to a Marina namespace, Project and
Workloads:

```json
[{"deck_project": "prj_default", "namespace": "factory", "project": "acceptance",
  "workloads": {"repository/v2": "land-on-main"}, "conversation_workload": "chat"}]
```

`repository/v2` lands on main and must map to a Workload with
`delivery.mode: land-on-main`; `repository/v1` opens a pull request and must map
to a `pull-request` Workload. Deck submits no work to a Workload whose delivery
does not match. An invalid file fails closed. Deck's identity needs the
`marina-factory` Role in each mapped namespace.

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

## How Deck uses Marina

A loop in the Deck process runs every second:

1. **Project sync** (every 30 seconds per project): reads the Marina Project and
   records its bound repository as the project's read-only repository binding,
   and publishes the mapped Workloads as the project's workload profiles.
2. **Dispatch**: each Ready task whose dependencies are satisfied and whose
   profile is mapped is claimed and recorded with its exact Run spec in one
   transaction, then created in Marina. The Run is named from the task
   (`deck-<task>-<attempt>`), so a lost response is replayed (HTTP 200) instead
   of creating a second Run, also after a restart. Marina's refusal blocks the
   task with Marina's message.
3. **Mirror**: for each active Run, Deck reads the Run, its Decisions and its
   events after the last mirrored sequence, and applies the matching Deck
   transitions: plan revision and approval decision, start, verification,
   evidence, completion with landing, block, failure or cancellation. Every step
   is idempotent, so replays change nothing. A landing completes the task only
   when Deck's own landing checks pass; otherwise the result waits for operator
   acceptance.
4. **Turns**: each chat request addressed to the Marina agent becomes one Turn
   with the thread and project context frozen at invocation. A clarification
   Decision appears as a question in the thread; a reply, a task proposal
   (created in Backlog), a PRD draft or ordered PRD tasks are published through
   Deck's own outcome rules. A failed Turn posts a visible reply.

Operator answers to plan approvals and chat questions, and cancellations, go to
Marina first with a stable `Idempotency-Key`; Deck records them only after Marina
accepts. Evidence downloads are proxied from Marina's artifact endpoint and
checked against their digest. The provider status shows whether Marina was
reachable on the last cycle and the last refusal reason; details are in Deck's
log.

## Operator API

The contract is [api/openapi.yaml](api/openapi.yaml). Every route requires an
operator session when operator authentication is enabled. Deck serves no route
for an executor.

Repository tasks require acceptance criteria, at least two attempts, and explicit
`source.read`, `source.write`, and `pull_request.create` or
`source.fast_forward` authority before entering Ready. Drafts can remain in
Backlog. Channel messages can become Backlog drafts before criteria are
complete. Evidenced landed prerequisites release dependent tasks without
operator acceptance. Other results retain human acceptance and
code-availability confirmation.

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

`POST /api/v1/operator/channels/{id}/invocations` addresses a configured agent.
Deck offers an agent in the composer when it has an installation ID and the
`conversation.invoke.{project_id}` capability; Deck registers the Marina agent
(`marina`) with that capability for every mapped project. The first invocation
fixes that agent for the thread; subsequent Send actions address it without
another mention. A leading `@name` can select a configured agent. The API records
the human message, thread, agent selection, and request ID atomically. Retrying
the same request ID recovers the same message; changing the request conflicts.
An existing human message can also be explicitly invoked. Thread URLs restore
the view after reload, and selected agents and invocation records survive
project export/import.

Each new invocation also freezes the selected active project-source revisions
and safe repository binding in the same transaction. Deck sends that snapshot,
with the thread up to the invoked message, as the Turn spec. Edits affect new
invocations; explicit revocation is respected.

Invocation is an operator intent record, not a task or model response. When
Marina asks a clarification question, Deck records it in the originating thread.
`GET /api/v1/operator/threads/{id}/questions` restores pending questions;
`POST /api/v1/operator/conversation/questions/{id}/answer` sends the answer to
Marina's Decision and then appends one human reply, rejecting a changed or second
answer. Answering does not authorize a task. Marina's reply and optional
proposal are published once in the thread; a proposed task is created in Backlog
and grants no execution authority. Deck links tasks to their originating thread
and mirrors selected run events there with canonical evidence references.

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
version; archived tasks cannot be edited, requeued or submitted to Marina until restored.
Archive state survives restart and project export/import.

## Project context

The **Project context** view stores versioned instructions, goals, authored
documents, exact-commit Git file references, and approved Deck decision
references. Only explicit operator approval marks a source as guidance.
Conversation excerpts remain task data.

When a task becomes Ready, Deck freezes selected source and message revisions.
The task's **Sources** view shows the interpreted outcome, repository binding,
provenance and trust. Deck sends exactly this selection, with content digests,
in the Run spec. Revision,
deactivation, and revocation have distinct meanings; active work keeps its
recorded revisions, while revocation denies further use.

Marina's Project owns the repository binding; Deck shows it read-only and
freezes it into tasks that become Ready. Changing the repository never grants
source-write or PR authority. See
[decision 0014](../docs/decisions/0014-project-repository-and-knowledge.md).

## Deployment and boundary

The container runs unprivileged and stores SQLite at `/data`. The Kubernetes
base under [deploy/kubernetes/](deploy/kubernetes/) uses one replica,
`ReadWriteOnce` storage, and `Recreate` updates. Choose immutable images,
storage, TLS, ingress, authentication, and backup in a private overlay. Do not
share the SQLite file between replicas.

Deck stores evidence metadata and proxies authorized downloads from Marina's
artifact endpoint; Marina owns the bytes. Deck does not schedule runtimes, mutate
source, call models, or read Marina's database; it uses only Marina's public API. Multi-user permissions, federated identity,
HA, agent-maintained knowledge, and deployment authority are outside the
current release.

## Layout

```text
cmd/deck/          application entry point
internal/domain/   ledger model
internal/store/    SQLite repositories and migrations
internal/httpapi/  operator HTTP/SSE API and Marina forwarding
internal/marina/   Marina API client, dispatch and mirror loop
internal/webui/    embedded frontend
web/               SvelteKit application
api/               OpenAPI and canonical fixtures
```

## PRD drafts and approval

A Marina Turn may propose a PRD draft (`title`, `path`, `content`) instead of a
task. Deck binds the immutable draft to the invoked repository and displays its
complete text in the originating thread. Review the draft, choose the time/token
limits and use **Approve PRD and publish**. Approval authorizes publishing that
exact document, not implementing its requirements.

`POST /api/v1/operator/messages/{id}/prd/approve` takes `digest`,
`profile_digest` and `budget`, with an `Idempotency-Key`. It requires an operator
session (or local operator mode), rejects bearer credentials and creates
one thread-linked `repository/v2` task, which Marina runs with the document as
the Run's `publish` input. Exact replay, including a new request key,
returns the original task. Changed content, repository bindings or approval
budgets conflict. Task details and the originating thread retain the landed
commit through the normal workflow. See [decision 0029](../docs/decisions/0029-approved-prd-publication.md)
and [acceptance status](../docs/status.md).

## PRD tasks and product acceptance

After publication lands, **Propose implementation tasks** records a chat request
in the same thread. The agent returns an ordered proposal with observable criteria.
Review the whole list and choose explicit limits per task; **Approve ordered tasks**
creates all tasks atomically. Each task retains the PRD path and commit, and waits
for its predecessor to land. The browser shows total limits before approval.

The publication card shows the approved tasks and their landed commits. **Accept
PRD** binds the current result set once every approved task has current evidence.
Requested follow-ups invoke the same thread's agent to propose new work; those
proposals need approval, and their landed results require fresh PRD acceptance.
Earlier tasks and acceptance history remain readable. See
[decision 0030](../docs/decisions/0030-ordered-prd-tasks-and-acceptance.md) and
[current acceptance](../docs/status.md).

The operator APIs are `POST /api/v1/operator/messages/{id}/prd/tasks/approve`,
`GET /api/v1/operator/tasks/{publication}/prd`, and
`POST /api/v1/operator/tasks/{publication}/prd/accept`. Mutations require an
idempotency key and reject bearer credentials. Proposals arrive as Marina Turn
outcomes and grant no execution authority.
