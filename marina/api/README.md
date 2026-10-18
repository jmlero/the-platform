# Marina platform API v1

The contract every client uses: Deck, `platformctl`, `kubectl`, other
factories. Direction in [decision 0033](../../docs/decisions/0033-marina-platform-api.md);
tasks in the [plan](../../docs/plans/marina-platform-api.md). Nothing here is
served yet; [status](../../docs/status.md) says what works.

| File | Contents |
| --- | --- |
| [openapi.yaml](openapi.yaml) | HTTP/JSON paths and the object schemas |
| [crds/](crds/) | The same objects as Kubernetes custom resources |
| [fixtures/](fixtures/) | Canonical examples; tests check them against both |

## One model, two surfaces

API group `marina.dev`, version `v1`. Every object has the Kubernetes shape:
`apiVersion`, `kind`, `metadata`, `spec` (what the client asks for) and
`status` (what Marina observed).

- **Kubernetes:** `kubectl apply -f run.yaml`, GitOps, normal RBAC.
- **HTTP/JSON:** the same objects at Kubernetes-style paths,
  `/apis/marina.dev/v1/namespaces/{namespace}/{resource}/{name}`, for UIs and
  clients without cluster access, plus what resources cannot carry: ordered
  events, server-sent events and artifact downloads.

The resource is the request record. An HTTP create checks the caller, then
creates the same resource. Marina's controller reads resources into its journal
and writes status back. The journal is the execution record used for recovery;
status mirrors it. Status is written only by Marina.

## Identity and tenancy

A project lives in a namespace. Both surfaces use Kubernetes identities and the
same RBAC. The HTTP API accepts a Kubernetes bearer token, authenticates it with
TokenReview and authorizes each request with SubjectAccessReview for the
resource, verb and namespace. Three Roles cover the common clients:

| Role | Can |
| --- | --- |
| `marina-viewer` | get, list and watch every Marina resource; read events and artifacts |
| `marina-factory` | viewer, plus create Runs and Turns, cancel them, answer Decisions |
| `marina-admin` | factory, plus manage Projects and Workloads |

## Resources

| Kind | Scope | Created by | Purpose |
| --- | --- | --- | --- |
| `Capabilities` | cluster, read-only, HTTP only | Marina | Workload types, models, tools and limits this installation offers |
| `Project` | namespace | admin | Repository binding and default Workload |
| `Workload` | namespace | admin | A workload type plus the factory's choices |
| `Run` | namespace | factory | Bounded work, its progress, usage and evidence |
| `Turn` | namespace | factory | One agent turn on frozen context |
| `Decision` | namespace | Marina | A question Marina needs a client to answer |

### Platform invariants versus factory choices

Marina always enforces, whatever a Workload says: budgets and installation
ceilings, sandbox and credential isolation, idempotency, audit, frozen inputs
for a running Run, and no completion without independent evidence on the exact
source revision.

A `Workload` of type `repository-change` holds the factory's choices: checks,
allowed paths, independent review on or off, pull request or landing on main,
required CI names, plan approval on or off, and repair limits. A `Workload` of
type `conversation` holds the chat choices: repository read tools, MCP tools,
and whether a turn may propose work. Workloads name models and tools from
`Capabilities`; engine, sandbox, endpoints and credentials stay in the
installation.

### Run lifecycle

`status.phase`: `Pending` → `Planning` → (`WaitingDecision`) → `Running` →
`Verifying` → `Succeeded`, or `Blocked`, `Failed`, `Cancelled`. A Run's spec is
immutable after creation, except `spec.cancel`, which may only change from
false to true. Marina freezes the spec it accepted; a later edit is ignored and
reported as the `SpecChanged` condition. The resource schema also rejects
withdrawing a cancel and adding or removing fields. Deleting a Run also cancels
it; its journal history stays.

Usage, attempts, the source revisions, the landed commit and every evidence item
appear in `status`. Each evidence item names its kind, the source revision it
proves and the artifact digest.

### Turns and decisions

A `Turn` carries its frozen thread (at most 64 messages of 16 KiB) and selected
context with digests and trust. Marina never fetches them from the client. The
outcome in `status` is a reply plus optional proposals (one task, a PRD draft,
ordered PRD tasks); the client decides what to do with them.

When a Run needs plan approval or a Turn asks a question, Marina creates a
`Decision` owned by that Run or Turn. Its question fields are immutable; a client
answers by setting `spec.response`. An expired Decision rejects new answers; an
exact replay of the accepted answer is a no-op.

## Writes and idempotency

`metadata.name` is the idempotency key. Creating an existing name with the same
spec returns the original object (HTTP 200); a different spec returns 409
`AlreadyExists`. The HTTP API also accepts `Idempotency-Key` on the cancel and
answer actions. Errors use the Kubernetes `Status` object.

## Events and artifacts

`GET .../runs/{name}/events?after={sequence}` returns ordered events; with
`Accept: text/event-stream` it streams them. Events carry a sequence, type,
time, message and safe fields such as usage; never prompts, code or secrets.
`GET .../runs/{name}/artifacts/{digest}` downloads retained evidence files.
Turns have the same `events` path.

## What the API never contains

Engine, sandbox or scheduling names (kagent, Agent Substrate, gVisor, Pod, Job,
harness), credentials, model endpoints, prompts or model reasoning. Fixtures are
checked for these words.

## Versioning

Additive changes keep v1 semantics. A breaking change needs `v2`.
