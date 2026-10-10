# Decision 0026: Read-only review of the exact candidate patch

- Date: 2026-10-01
- Status: implementation choice for AUTO-08; installation acceptance is in [status](../status.md)
- Scope: Marina repository execution and its existing Deck evidence contract
- Builds on [0021](0021-semantic-review-gate.md), [0022](0022-autonomous-software-factory.md) and [0025](0025-repair-attempts-and-budget.md)

## Reviewer and inputs

Reuse the installed text-only `kint-conversation-byo` image for a separate review
role. Every review gets its own prepared template, instance, credential and model
call. It uses the repository engine's endpoint and namespace, with no shell,
source, tool, question or publication route. Conversation and review retain
separate input contracts, credentials, accounting and result artifacts.

Repository configuration requires an explicit `reviewer_model`, different from
the producer's `model`. Its endpoint, credential reference, output ceiling,
currency and price rates participate in the immutable installation profile.
Drain and reauthorize work when that profile changes; an active task cannot
silently acquire a new reviewer or more budget.

Marina supplies the original task and acceptance criteria, allowed paths, trusted
checks, frozen context, candidate operations, complete original files at the
candidate's exact base, and repair history/diagnosis when present. Original files
come from the pinned Git tree and blobs. Missing paths require a complete tree;
links, submodules, oversized files and truncated trees block admission. Text is
supplied as readable UTF-8; binary bytes retain their encoding. Complete input is
bounded to 256 KiB and is never truncated to obtain a verdict.

The gateway fixes the upstream model, installed review instructions and exact
input. It rejects tools, session continuation, arbitrary instructions, changed
input and Git access. Instructions require separate acceptance, scope, test
preservation, repair-approach and defect verdicts, each with reasons and file/line
citations. Missing or malformed verdicts block without counting an implementation
failure. A model verdict does not replace clean verification or required CI.

## Commitment and evidence

The patch identity covers its base revision and every path, byte, mode and delete
operation, with canonical path ordering. Both the candidate and review result
are durable before branch creation or publication. Publication revalidates the
producer artifact, independent attempt, profile, input digest and passing verdict,
then commits the saved candidate through the guarded source adapter. A changed
patch or base cannot reuse the earlier review.

After commit, immutable evidence joins that patch identity, base, resulting
commit, reviewer attempt, configured model, profile and verdict. Rejected reviews
are evidence at the candidate base with no resulting commit. Earlier records stay
as history; completion selects the latest commit's review and clean checks.
Existing evidence fields and artifact downloads suffice; no new wire fields or
database migration is needed. Deck accepts the advertised review minimum while
retaining older repository profiles and the original runtime-start budget meaning.

A rejected review counts as one failure and enters diagnosis before a different
implementation approach, including before a task branch exists. A diagnosis
disputing a finding with evidence, or requiring work outside the frozen task,
blocks and exposes its reason instead of admitting another implementation.

## Budget and recovery

Runtime starts still include every engine instance and verifier Job. Review adds
one start to each implementation; current ceilings and examples live in the
[profile contract](../../marina/docs/workloads/profiles-v1.md#immutable-selection-and-authorization).
Smaller task budgets remain smaller, and the three-failure limit remains separate.
All producer, diagnosis and reviewer calls share the original token, cost and
time limits. No review can commit when usage is uncertain.

The journal admits at most one upstream call per review attempt. A known response
is retained with its accounting before delivery and can replay without new spend.
Unknown usage keeps its full reservation and denies another call. Runtime, commit
and evidence recovery reuse original keys and artifacts; a restart does not
rerun a passing review or substitute a candidate.
