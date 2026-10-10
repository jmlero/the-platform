# Decision 0023: Focus the kagent cutover on the factory

- Date: 2026-09-25
- Status: accepted
- Scope: the KINT gates in [decision 0017](0017-kagent-core-engine.md) and the
  [kagent plan](https://github.com/jmlero/the-platform/blob/b7ccd54cdf79f3f669d23c75eb5c9840f43de953/docs/plans/kagent-integration.md), and the diagnostic workload of
  [decisions 0015](0015-marina-agent-platform.md) and
  [0016](0016-workload-invocation-and-context-tool.md)

## Context

[Decision 0022](0022-autonomous-software-factory.md) aims the platform at one
milestone: one real repository, one PRD from the chat, and agents landing its
tasks on main. Both the chat agent and native Codex run on kagent, but kagent has
only run as isolated trials that were torn down afterwards.

Before cutover, KINT-09 required every row of the live failure matrix on both
workloads, and KINT-10 a schema-aware rollback with active kagent data. All
user-visible progress waited on that. The installation is single-user on a
laptop, and backups are already deferred
([decision 0010](0010-initial-installation-services.md)).

The LangChain diagnostic workload proved that a second harness fits the
governance model (LATER-02). It is not part of the factory, and it doubles the
KINT test, cutover and removal work.

## Decision

The operator decided:

1. **Core gates only.** Cutover (KINT-11) needs these live cases on real Deck,
   Marina and kagent processes with repository work:
   - duplicate starts and sends, and lost create, send and publication responses:
     no duplicate work, commits or spend;
   - Marina, the engine and the worker restarting during work: the work recovers
     or blocks truthfully;
   - cancel during a model call, during a tool call and while waiting for input:
     the work stops and late results are ignored;
   - time, token or cost budget exhausted, and final usage lost: consumption
     stops and the reservation is kept;
   - failed verification or CI: no false completion.

   KINT-10 becomes: upgrade with active kagent data and keep history and
   in-flight work; roll back by restoring the previous release and the retained
   data copy.
2. **Hardening later.** The other matrix rows move to KINT-17: event reordering,
   lease loss and context revocation during a call, bypass attempts through
   model egress, MCP or the engine API, template edits during a call, artifact
   storage failure, schema-aware rollback, and a full footprint measurement.
   KINT-17 must pass before the platform runs on an independent cluster or for
   other users.
3. **Retire the diagnostic workload.** The LangChain workload is no longer
   supported and is not a cutover target. KINT-12–14 delete its code, image,
   profile and installation. Its tasks and evidence stay readable as history.
   kagent's bring-your-own path stays, because the chat agent uses it.

The rules themselves do not change. Fencing, budgets, termination, source
binding and evidence stay in the code and in deterministic tests. Only the live
proofs that gate cutover change.

## Consequences

- The chat agent can go live first, on a permanent kagent installation
  (KINT-16), while repository work stays on the current runtime until KINT-11.
  That dual-engine period ends with KINT-12–14.
- The repair loop (AUTO) starts after KINT-11 instead of KINT-15, so KINT-12–15
  can run alongside it.
- Decision 0015's choice of LangChain as the selected harness ends. Its
  two-workload proof remains valid history.
