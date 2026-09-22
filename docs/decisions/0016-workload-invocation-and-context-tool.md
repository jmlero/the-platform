# Decision 0016: Invoke installed workloads through Deck and govern context reads

- Status: accepted and implemented for AGENT-07 through AGENT-10
- Date: 2026-09-21
- Scope: additive work-control v1 API, immutable context, gateway and inspection

## Decision

Marina publishes a safe projection of its installed workload profiles to Deck.
The project-scoped service identity needs `workload.publish`; the original
publisher owns the projection. Marina refreshes a two-minute catalog, and Deck
rejects publication lifetimes above five minutes. Expired or withdrawn profiles
cannot authorize new invocations. Discovery includes only workload/harness IDs,
digests, required authority, token/time ceilings, minimum attempts, tool names
and supported context kinds. Endpoints, image locations and credential references
remain in Marina. A capability advertisement means the API exists; a live catalog
means that installation has a profile. Neither changes task intake settings.

Authenticated operators discover and invoke profiles through Deck's operator API.
Invocation requires a stable idempotency key, exact profile digest, acceptance
criteria, explicit authority and bounded task budgets. One transaction captures
the Ready task, profile metadata, selected context and replay receipt. Identical
retries return the original task even after profile withdrawal; changed requests
conflict. The task's profile stays pinned during edits and export/import. Catalogs
are installation-local and are not portable. Marina checks the complete selected
profile against its installed configuration before using it; an unavailable or
changed profile blocks execution. Existing tasks without profile selection retain
their v1 behavior. No additional task ledger or worker invocation API is added.

The second harness's first reusable HTTP tool is `read_context_source`. It accepts
only a selected source URI and returns bytes already in the execution's persisted
context package, including trust, provenance and digest. It performs no arbitrary
network fetch. The existing attempt credential reference, signed specification,
live Deck lease/fence, profile allowlist, source expiry and explicit revocation
govern access outside the harness. The worker receives no Deck, GitHub or upstream
model credential. Allowed and denied authenticated decisions enter the durable
outbox before a response; audit records contain argument digests, not source text
or credentials. Identical attempt/argument/outcome decisions are deduplicated.

Both workflows use the existing immutable context assembler. Deck owns authored
sources and selections; Git owns pinned repository content. Ordinary revisions
do not alter an active run, while explicit revocation prevents further use.
Recovery loads the recorded package; each input keeps its provenance, content
digest, trust and expiry. Selected Git inputs use the task's captured repository
binding. Trust labels never add authority or increase budgets. The diagnostic
report's semantic validator remains deliberately limited to the fixed fixture.

`langchain-create-agent/v2` explicitly introduces selected context and the tool.
The legacy v1 harness cannot advertise these capabilities, and tasks selecting
project sources need v2. An upgrade requires a newly built, digest-pinned image,
updated profile and newly authorized task. Existing v1 profile/specification
digests remain stable when optional fields are absent. V2 validation evidence
must bind the context digest; old v1 evidence remains readable.

Deck shows profile/harness identity, current progress, usage and downloadable
evidence. Usage distinguishes provider-confirmed totals from unresolved reserved
calls. Marina's redacted diagnostics correlate tasks, executions, attempts,
model calls, tool decisions and artifacts through durable IDs without exposing
their private contents or connection configuration.

## Evidence and limits

The [validation report](../validation/agent-07-10-2026-09-21.md) links real-process
API conformance, pinned LangChain harness tests, browser inspection and observed
network isolation on local Podman/Kind. New code was not deployed for a live model
run in this tranche. The installed analysis image supplied the network probe;
that observation does not establish deployment of harness v2. The subsequent
[AGENT-11 through AGENT-13 report](../validation/agent-11-13-2026-09-22.md) records
the deployed v2 harness, both API-invoked workloads, shared failure matrix and
[repeatable installation](../../deploy/kubernetes/agent-platform/README.md).
It distinguishes safe blockers from uninterrupted worker recovery. This decision extends
[0015](0015-marina-agent-platform.md) and preserves context ownership in
[0014](0014-project-repository-and-knowledge.md).
