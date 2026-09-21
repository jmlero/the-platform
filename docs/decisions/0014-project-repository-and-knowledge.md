# Decision 0014: Project repository and reusable knowledge

- Status: accepted; explicit-source workflow implemented 2026-09-21
- Date: 2026-09-17
- Scope: CTX-01 through CTX-08; Deck projects, Marina project bindings, and task context

## Decision

A project brings together its repository, tasks, conversations, and reusable
knowledge. Start with one GitHub repository per project. The operator manages
the project through Deck, associates its repository, and maintains custom
instructions, project goals, selected documents, and approved decisions once.
Tasks in that project inherit this context without requiring the operator to
repeat it. Selected conversation excerpts supply task-specific context.

Reuse Deck's existing project identity and Marina's versioned project binding.
Marina references the Deck project ID and owns the repository connection and
execution configuration. This direction does not introduce another project
identity or change the current limit of one configured repository workflow and
one active task.

## Ownership

| Information | Authoritative owner | Use by the other components |
| --- | --- | --- |
| Project identity, name, goals, tasks, and conversations | Deck | Marina references the project and snapshots authorized work. |
| Custom instructions and documents authored in the project knowledge base | Deck | Marina consumes explicit revisions through the public contract. |
| Selected context sources and their project applicability | Deck | Marina resolves the selected references for the task. |
| Code, repository instructions, and documents or decisions maintained in Git | Git repository | Deck registers source references; Marina reads them at a pinned commit. |
| Decisions and selected conversation excerpts already recorded in Deck | Existing Deck records | Knowledge entries reference their IDs and revisions instead of creating another authoritative copy. |
| Repository connection, default branch, runtime, policy, and credential references | Marina project binding | Deck provides the setup surface and displays safe configuration summaries through a supported contract. |
| Assembled context and the record of exactly what a worker received | Marina | Deck exposes the run's source list and immutable manifest reference for inspection. |

The knowledge base is persistent project information managed through Deck.
Marina's saved context packages are derived execution snapshots, not a second
editable knowledge base. Git remains authoritative for files maintained there.
Registering or indexing such a file does not move its ownership into Deck.
The services keep separate stores and exchange information over their versioned
HTTP/JSON contract; neither reads the other's database.

## Minimum context references

The implemented contract identifies each selected input by its project, source
owner, kind, stable reference, revision, and content digest, together with scope,
provenance, trust, freshness, and superseded or revoked state where applicable.

- Repository inputs identify the repository, exact commit, and file path.
- Deck-authored instructions and documents have stable IDs and explicit revisions.
- Existing Deck decisions retain their canonical IDs and revision references.
- Conversation excerpts identify selected message IDs and their exact revisions
  or immutable content digests; a live channel URL alone is insufficient.

The additive version-1 wire contract is published in [Deck OpenAPI](../../deck/api/openapi.yaml)
and its canonical fixtures. `context.read`, `repository.read`, and
`repository.write` are implemented capabilities, exercised by joint conformance.

## Use in a task

When work is authorized, capture the applicable project-source revisions and
selected conversation references. Marina combines them with the frozen task,
policy, and pinned repository revision, persists the context package before
dependent execution, and delivers it to the worker. Reuse the existing context
package and journal foundations.

Restart and retry must use the saved references and package rather than resolving
an unrecorded "latest" version. Routine edits affect newly authorized work;
they do not silently alter an active run. Revocation must be enforced explicitly,
with the required stop or clarification remaining visible in the run history.
The current workflow still uses cancellation and a new task when work must be
re-authorized; this decision does not add pause, resume, or automatic replan.

The operator can inspect which repository revision, instructions, documents,
decisions, and conversation excerpts a run received. Context supplies knowledge
and guidance within existing task authority; it cannot grant new permissions or
increase budgets. Conflicting instructions require a focused clarification rather
than a silent override. Retrieved text and conversation remain data unless
explicitly validated and designated as guidance.

For example, a project has a custom instruction to preserve its public API and
references an architecture decision in Git. Two tasks inherit those sources
without repeating them. Updating the instruction affects a newly authorized
third task; the earlier runs still show their original inputs.

## Delivery and deferred choices

CTX-01 through CTX-08 have implementation and test evidence in the
[context validation report](../validation/ctx-02-08-2026-09-21.md).

Deck owns durable association intent and a safe binding projection. Marina
validates repository/base-branch access through its installed GitHub origin,
keeps credential references private, records version history and a receipt before
confirming to Deck, and reconstructs an execution using its original binding.
A request may change owner/repository and base branch within that origin; changing
hosts, credentials, trusted checks, or runtime policy remains installation setup.
Missing credentials or a transient provider outage defer validation; a permanent
access/branch failure produces a visible rejection that can be corrected.

Explicit expiry and declared conflicting source IDs govern freshness/conflicts.
A conservative override filter omits known injected data or blocks such guidance;
this is not general semantic contradiction or prompt-injection detection. Trust
labels and the existing external authority/budget/worker gates remain mandatory.

Begin with explicitly maintained project sources. Automatic discovery,
agent-suggested knowledge, and promotion of learned guidance need subsequent
decisions. This minimum contract does not require a retrieval framework, vector
database, additional service, multiple repositories per project, or concurrent
project execution.

This implements the operator's agreed direction as a subsequent increment to
[decision 0007](0007-sovereign-first-usable-release.md). It adds no automatic
revision loops, merge/deploy authority, or additional active task concurrency.
