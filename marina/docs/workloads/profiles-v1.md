# Version 1 workload and harness profiles

The installation format accepts `version: 1`. Unknown fields and versions fail
closed. Deck owns task authorization; Marina owns repository bindings, installed
profiles and immutable execution inputs. Current acceptance is recorded in
[status](../../../docs/status.md); clients use the [public API guide](api-v1.md).

| Property | Repository workflow | Conversation |
| --- | --- | --- |
| Identity | `repository/v1` for PR delivery; `repository/v2` for guarded main landing | Explicit Deck agent invocation, separate from task intake |
| Harness | Native Codex on the pinned kagent engine | Pinned `kint-conversation-byo` Harness |
| Configuration | `git`, `checks`, `allowed_paths`, `required_ci`, `kubernetes.image`, `repository_engine`, `repository_engine_gateway_url`, `reviewer_model`; optional `delivery: "fast_forward"` | Optional `conversation` with engine, token/time ceilings and fixed delegation budget |
| Inputs | Authorized task, exact Git commit, versioned repository binding and selected immutable context | Frozen originating thread, repository binding/commit/file index and explicit project-source revisions |
| Runtime | Native implementation; independent text-only review before every commit; native diagnosis before each repair; clean Kubernetes verification per commit | One attempt-exclusive BYO engine instance |
| Completion | Exact-patch review, clean checks and required CI; v2 also requires guarded fast-forward | One canonical reply and optional bounded task in the originating thread |
| Authority | `source.read`, `source.write`, plus `pull_request.create` (v1) or `source.fast_forward` (v2) | No repository mutation; Ready delegation needs separate installation limits and service capability |

The LangChain diagnostic profile is removed under
[decision 0023](../../../docs/decisions/0023-focus-kagent-cutover.md).
Historical specifications, profile digests, journal records and artifacts remain
readable. They cannot select a new legacy agent Job. The Kubernetes image is a
**verifier**, built from `marina/Dockerfile.verifier`; it contains trusted offline
check dependencies and no Codex or Python agent harness.

## Immutable selection and authorization

Repository configuration requires this engine selection:

```json
{
  "repository_engine": {
    "backend": "kagent",
    "version": "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1",
    "endpoint": "kint-engine-controller.kint-engine.svc:8083",
    "namespace": "kint-engine",
    "harness": "kint-codex"
  },
  "repository_engine_gateway_url": "https://marina.marina-system.svc"
}
```

No shared template or mutable revision belongs in this selection. Marina creates
an attempt-exclusive AgentTemplate, ModelConfig and same-name gateway Secret,
waits for preparation, then freezes the exact revision in the attempt. Resource
names cannot be reused. Template/model/tool edits cannot replace an admitted
instance's revision. Engine completion alone cannot complete the Deck task.

Invocation pins the discovered profile ID/digest and explicit task authority.
A task needs acceptance criteria, positive token/time limits within installation
ceilings, one active task and at least three attempts for implementation, review
and independent verification. Configure nonzero rates for both models in the
task's currency for cost admission. Unknown model usage keeps its entire
reservation across restart.

`max_attempts` counts runtime starts, including review, clean verification and
repair diagnosis. Three starts cover an initial implementation, review and
verification; seven can cover one complete repair; eleven can cover two. The
installation caps starts at eleven, and a smaller frozen task budget remains
smaller. Independently, three failed reviews or implementations block. All starts
share the original time/token/cost budget; no repair or restart grants more. Chat
delegation retains its explicitly configured budget and must offer at least three
starts for this profile. The version-1 runtime-start meaning from
[decision 0025](../../../docs/decisions/0025-repair-attempts-and-budget.md) is
preserved; [0026](../../../docs/decisions/0026-read-only-review-and-exact-patch.md)
adds review.

`reviewer_model` uses the same fields as `model`: an explicit endpoint, different
model ID, credential file reference, bounded output, currency and price rates.
Build the current `Dockerfile.conversation-byo` and pin it in the existing
`kint-conversation-byo` Harness even when conversation is disabled. Marina creates
a fresh text-only review instance per candidate using this Harness. It receives
complete old/new files, criteria, frozen context and repair history, with no Git
or tool access. Inputs exceeding 256 KiB block rather than being truncated.
Only a valid passing verdict for the exact candidate and base permits commit;
review failures feed diagnosis, while disputed findings or invalid verdicts block.

Active work retains its original task, binding, profile, context and attempt.
Installation changes block further execution rather than silently selecting new
inputs. Ordinary source edits do not replace frozen context; expiry or explicit
revocation denies further use. Retrieved text cannot enlarge authority or budget.

The gateway checks the exact live Deck lease/fence on every request and during
long responses. When Deck cannot renew a lease, local expiry stops the recorded
runtime even while Deck is offline. Recovery inspects recorded identities;
it never blindly replaces an uncertain creation or message delivery.

## Conversation selection

Build `marina/Dockerfile.conversation-byo`, pin its OCI digest in the installed
Harness and configure:

```json
{
  "conversation": {
    "engine": {
      "backend": "kagent",
      "version": "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1",
      "endpoint": "kint-engine-controller.kint-engine.svc:8083",
      "namespace": "kint-engine",
      "harness": "kint-conversation-byo"
    },
    "budget_minutes": 5,
    "max_tokens": 20000
  }
}
```

Deck saves the repository binding and active explicitly registered project-source
revisions with the invocation. Marina resolves that selection through
`GET /api/v1/conversation/invocations/{id}/context`, then saves the exact branch
commit, complete regular-file index and selected content before engine dispatch.
Recovery uses these retained inputs; source edits and a moved branch do not
replace them. Revocation, expired guidance and declared conflicting sources deny
further use. Selected Git documents keep their explicitly registered commits,
which may differ from the turn's repository commit. Guidance retains its trust
and provenance; other repository files, thread text and tool results remain data
and cannot grant authority. See
[decision 0027](../../../docs/decisions/0027-read-only-repository-context-for-chat.md).

The actor calls only Marina's model gateway. The gateway fixes the instructions
and validates the frozen transcript, then offers `list_repository_files` and
`read_repository_file` for the recorded repository and commit. It lists at most
100 files per page from a complete index of at most 10,000 tree entries. Reads
accept regular UTF-8 files up to 256 KiB, returning at most 200 lines and 16 KiB
per read. Tool results total at most 64 KiB per exchange. Links, submodules,
incomplete trees, binary files, unsafe paths and unknown tools are rejected.
The actor receives no Git or upstream model credential and has no source-write,
shell, browser or arbitrary tool authority.

An optional `conversation.mcp` adds read-only MCP tools to a turn with a bound
repository: an HTTPS streamable-HTTP `endpoint`, an optional `token_file` and an
allow-list of one to eight `tools` that must not reuse the repository tool
names. The gateway opens one MCP session per model exchange, offers only allowed
tools the server lists, and calls them itself under the same authority checks,
call and round limits and 64 KiB result budget as repository reads. Arguments
are limited to 4 KiB and each result to 16 KiB of text; a larger result returns
an error to the model. Without a reachable endpoint the turn keeps its
repository tools only. Tool schemas come from the server at each exchange and
are not frozen with the turn. Results are data and grant nothing. See the
[platform guide](../../../deploy/kubernetes/platform/README.md#mcp-tools-plat-08).

An initial model exchange permits up to four upstream requests, including read
tool continuations. Every intermediate response and final answer counts against
the original turn's token limit and deadline. Missing or uncertain usage retains
the reservation and prevents replay. The actor can ask one typed question with
up to six choices. Deck records the expiry and closes
pending questions when the turn ends. An exact answer replay preserves the same
human message; a changed, expired or closed pending answer conflicts. Marina
sends the answer only to that recorded waiting operation and admits one bounded
follow-up exchange of at most four upstream requests, using the same frozen
repository, token limit and deadline. Uncertain delivery retains reservations
and blocks instead of sending the answer again blindly.

The service identity needs `conversation.invocation.list`, `channel.message.list`,
`conversation.context.read`, `conversation.question.write` and
`conversation.outcome.publish`, scoped to the project. Optional Ready delegation
additionally needs `conversation.task.delegate`
and a fixed `conversation.delegation` budget admitted by the repository profile.
An interpreted task cannot invent limits or grants. Publications are journaled
before Deck receives them and replay with their original identity.

Invocations made before context capture was installed return missing context;
they never acquire a new selection from current project sources. Previously
published replies and retained active-turn artifacts remain historical inputs.

## Installation and inspection

Use the [installation guide](../../../deploy/kubernetes/agent-platform/README.md)
and [permanent local engine guide](../../../deploy/kubernetes/local-podman-kagent/README.md).
`marina check-config --config workflow.json` validates configuration without
reading referenced credentials or contacting providers. It reports the selected
profile digest. `scripts/check-installation.py` checks the private rendered
installation, including exact engine wiring, digest pins and disabled intake.

`serve --intake-task-id <id>` permits discovery of one selected task;
`--intake-enabled=false` disables new discovery. Both continue recovery and
cancellation for journaled work. These flags do not disable explicit chat
invocations. Keep credentials in private files and references out of task text.
Use `marina diagnostics` for redacted attempt, usage, cleanup and conversation
metadata. Old schema records are retained for history and recovery; never drop
records to repair an upgrade.
