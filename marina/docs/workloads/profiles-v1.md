# Version 1 workload and harness profiles

AGENT-02 defines two bounded workload types in the existing Marina service.
The installation format accepts `version: 1`; an omitted version retains the
existing repository configuration. Unknown fields and versions fail closed.
Deck remains the task and authorization ledger. Marina owns profiles and worker
inputs. AGENT-07 through AGENT-10 add an installed-profile catalog, operator
API invocation, selected immutable context, one governed HTTP tool and shared
inspection. See the [API client guide](api-v1.md).

| Property | Repository workflow | Diagnostic analysis |
| --- | --- | --- |
| Workload | `repository/v1` (legacy runtime records omit this field) | `diagnostic-analysis/v1` |
| Harness | Existing Codex CLI invocation in a pinned worker image | `langchain-create-agent/v1` (fixture only), `/v2` (selected context/tool), or `langchain-create-agent/kagent-byo-v1` (pinned BYO runtime); LangChain `create_agent` on LangGraph |
| Configuration | Existing `git`, `checks`, `allowed_paths`, `required_ci`, `kubernetes.image` | Optional `analysis` object: version, revision, image, harness_version, max_tokens, budget_minutes, fixture, tools |
| Inputs | Authorized task, exact repository commit, immutable context package | Authorized task, profile/input digests, frozen diagnostic sources and, for v2 or BYO, selected context package |
| Output | Scoped source changes and separate deterministic verification result | JSON `summary` and `findings`, each with code, source_id, quote and explanation |
| Completion | Separate clean verification Job, current PR head and required CI | Independent Go validator checks fixture findings and source quotes; evidence binds input, profile, output and v2 context digests |
| Tools | Bounded coding harness; trusted verification commands | `DiagnosticReport` structured output; v2 and BYO optionally allow `read_context_source` through Marina |
| Source authority | Explicit `source.read`, `source.write`, `pull_request.create` | None; Git gateway routes deny access |
| Model | Installed Responses-compatible endpoint/name and file credential reference | Same governed endpoint; self-contained requests, no stored conversations or hosted tools |
| Runtime | Isolated Kubernetes Job or selected kagent engine, one implementation and one verification attempt | Isolated Kubernetes Job or selected kagent BYO engine, one analysis attempt |

Both use the same persisted claims, leases, fences, task budgets, model admission,
usage records, worker credentials, cancellation, artifact storage and recovery.
No new service, database, workflow engine, vendor control plane or AWS dependency
is introduced. Repository fields are optional for an analysis-only installation;
analysis never checks out a repository or creates a commit or PR.

## Authorization and immutable identity

The analysis task's existing `budget.authority` contains exactly one scoped
grant: `analysis.run.<digest>`. The digest is the SHA-256 of Marina's canonical
JSON encoding of the complete analysis profile, model settings (including the
credential *reference*, never its value), and policy version. It is available via
`marina workload-profile --config /path/to/workflow.json` (local inspection,
without reading credentials or contacting a service). Arbitrary
or stale grants and combinations with source/mutation authority fail closed.
This preserves the existing extensible authority field. The public invocation
API additionally pins the selected profile metadata on the task; discovery alone
does not confer permission. Legacy tasks without that field remain supported.

A task must have acceptance criteria, a positive token limit within the profile,
and a time limit within the profile. Installation ceilings still apply. Profile
edits change the grant; they cannot authorize pending work against a different
fixture, image, model destination or budget. Active work retains persisted inputs
and profile identity. A changed installation blocks further execution instead of
silently migrating a run. Restoring the original configuration permits recovery
only while the original run still has authority. Cancelled/blocked work requires
new authorization.

New runtime specifications identify workload, profile digest, input digest and
worker image. Only installed digest-pinned images may start. Old repository
records and their digests retain their original representation. Unsupported
workloads or repository fields on analysis specifications are rejected.

## Synthetic diagnostic fixture

[`blocked-runs-v1.json`](../../internal/analysis/fixtures/blocked-runs-v1.json),
embedded by `analysis.DiagnosticFixture()`, is the authoritative `blocked-runs`
revision 1 fixture. It contains three immutable text sources:

| Source | Required result | Causal source evidence |
| --- | --- | --- |
| `model-outage` | `provider_unavailable` | `upstream HTTP 503` |
| `token-limit` | `budget_exhausted` | `requested 12000 tokens; remaining budget 4000 tokens` |
| `healthy-worker` | No finding | Successful worker and independently checked result |

The report needs exactly the two expected findings, the matching source IDs,
verbatim supporting quotes and bounded nonempty explanations and summary.
Duplicate, missing, invented or mismatched findings fail validation. Expected
answers are retained in Marina; workers receive only source records and output
schema. Fixture contents are untrusted data and cannot alter task authority.
This proves a narrow diagnostic report contract, not general semantic review or
live production-feedback integration.

The initial profile allows at most 20,000 tokens and five minutes. Acceptance
uses a nonzero USD cost cap based on the existing configured rates and remains
within the operator's existing EUR 5 total model-test authorization. There are
no runtime dependency downloads or direct public destinations from the worker.
Model requests use only Marina, which forwards to the installed OpenRouter
endpoint with the existing private credential reference. The worker has neither
an upstream model credential nor a Kubernetes service-account token.

## Harness packaging and compatibility

The analysis image packages the Go worker and Python harness together. Pinned
and hash-checked dependencies are in
[`requirements.txt`](../../harnesses/langchain/requirements.txt). The initial
resolved libraries include LangChain 1.4.2, LangGraph 1.2.12 and
langchain-openai 1.6.2. They execute locally; tracing is disabled. LangSmith's
optional OSS client is a transitive dependency, but no LangSmith account, hosted
service or tracing endpoint participates in execution.

The implementation uses the documented
[`create_agent`](https://docs.langchain.com/oss/python/langchain/agents) and
[ChatOpenAI Responses integration](https://docs.langchain.com/oss/python/integrations/chat/openai).
LangChain's [MIT license](https://github.com/langchain-ai/langchain/blob/master/LICENSE)
permits local use. The worker has no delegation, filesystem, shell, search or MCP
tools. V2 adds one HTTP tool: `read_context_source` accepts an exact URI from the
immutable package and returns that recorded source with provenance and trust.
The gateway validates the attempt credential, allowlist, arguments, live authority,
expiry and revocation, and persists an audit decision. It cannot fetch arbitrary
URLs or expose credentials. Python framework types stay inside the harness boundary.

The `langchain-create-agent/kagent-byo-v1` profile packages the same pinned
`run.py` and hash-checked Python dependencies with a small A2A server in
[`Dockerfile.analysis-byo`](../../Dockerfile.analysis-byo). Marina sends the
immutable fixture and selected context as one bounded message. The BYO server
emits a bounded `marina-result.json` artifact; Marina checks the artifact against
the frozen input and independently validates every finding and source quote.
The BYO profile has its own revision and image digest. Historical `/v1` and
`/v2` profile digests remain readable and keep their original runtime path.
The BYO actor has no repository authority or publication step.

Both workloads use the context assembler described by
[decision 0014](../../../docs/decisions/0014-project-repository-and-knowledge.md).
Active project source revisions and explicitly selected messages are captured at
task authorization; recovery uses the persisted package. Revision changes do not
replace it, while explicit revocation blocks further use. Git sources require an
installed repository binding and exact commit. Trusted guidance remains bounded
by structured task authority; diagnostic records and conversations stay untrusted.
Additional selected context does not expand the fixed report validation contract.

## Local installation and invocation

Build `Dockerfile.analysis-worker` from the `marina/` context. Dependencies are
installed at build time with hashes; load the image and use its registry/containerd
manifest digest, not a mutable tag, in `analysis.image`. Add this profile object
to a version-1 workflow configuration, using the fixture file above as the
`fixture` value. Existing Git configuration can coexist with it or be omitted
for an analysis-only installation. Replace both placeholders before loading:

```json
{
  "version": 1,
  "revision": 1,
  "image": "registry/analysis-worker@sha256:<64 hexadecimal characters>",
  "harness_version": "langchain-create-agent/v2",
  "tools": ["read_context_source"],
  "max_tokens": 20000,
  "budget_minutes": 5,
  "fixture": "<replace with the fixture JSON object>"
}
```

V2 requires an image built from this implementation. Merely changing the harness
name on an older image does not upgrade it. Legacy `langchain-create-agent/v1`
profiles remain valid with no tools; they cannot advertise selected context, and
tasks that select project sources block with an upgrade explanation. Optional
fields preserve old profile and runtime digests when absent.

For the selected kagent BYO path, build `Dockerfile.analysis-byo` from
`marina/`, make its digest-pinned image available to the Substrate actor image
cache during preparation, and prepare a
`kint-analysis-byo` Harness with that image and a `kint-analysis-task` compatible
AgentTemplate. Pin the controller's **prepared revision** and the same image
digest in a new analysis profile revision (at least 3). Set
`harness_version` to `langchain-create-agent/kagent-byo-v1` and select the
engine in the workflow configuration:

```json
{
  "analysis_engine": {
    "backend": "kagent",
    "version": "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1",
    "endpoint": "kint-engine-controller.kint-engine.svc:8083",
    "namespace": "kint-engine",
    "harness": "kint-analysis-byo",
    "template": "kint-analysis-task",
    "revision": "<prepared revision digest>"
  }
}
```

The template's OpenAI ModelConfig must point at Marina's governed HTTPS model
gateway and reference an attempt-exclusive `gateway` Secret. Marina installs
that Secret before dispatch or recovery. Substrate injects an inert API-key
placeholder into the actor and substitutes the current Secret at the gateway;
the actor never receives the real token. Give the BYO Harness only the pinned
image, fixed entry command, isolated WorkerPool and private snapshot location.
Keep preparation downloads separate from runtime network access. These
settings change the analysis grant; previously authorized tasks retain their
original profile digest and cannot silently move to the BYO runtime.
The pinned actor mounts its egress trust bundle at
`/run/kagent/egress/trust-bundle.pem`; the BYO image sets Python's TLS trust
environment to that file. The router separately verifies Marina's HTTPS
service certificate.

## Optional read-only conversation profile (CHAT-04)

Build `marina/Dockerfile.conversation-byo` as a pinned image and prepare a
`kint-conversation-byo` Harness with no source, shell, browser, task or tool
capabilities. Give it a dedicated AgentTemplate, a same-name `gateway` Secret,
and a Responses ModelConfig routed through Marina's HTTPS conversation gateway.
The private workflow may then select the exact prepared revision:

```json
{
  "conversation": {
    "engine": {
      "backend": "kagent",
      "version": "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1",
      "endpoint": "kint-engine-controller.kint-engine.svc:8083",
      "namespace": "kint-engine",
      "harness": "kint-conversation-byo",
      "template": "<dedicated template name>",
      "revision": "<prepared revision digest>"
    },
    "budget_minutes": 5,
    "max_tokens": 10000,
    "delegation": {
      "time_minutes": 30,
      "max_attempts": 2,
      "max_tokens": 20000,
      "max_cost_minor": 0,
      "currency": "USD",
      "max_concurrency": 1,
      "authority": ["source.read", "source.write", "pull_request.create"]
    }
  }
}
```

Omit `delegation` for reply-only operation; the illustrated grant also requires
an enabled repository profile whose installed limits admit that budget. The
service identity needs `conversation.outcome.publish` for terminal replies and
Backlog drafts, plus `conversation.task.delegate` for Ready work, scoped to the
project. An interpreted task cannot add its own budget or grants. The gateway URL is
`https://marina.marina-system.svc/conversation/model/v1/responses`.
Marina registers its stable agent with `conversation.invoke.{project_id}` only
while this profile is configured. It scans the project channel ledger and runs
one frozen, bounded turn through the pinned engine. The template and Secret are
attempt-exclusive and cannot be reused by another turn. Provision a new pair
and select its revision before another invocation; a second invocation against
the used template is recorded as blocked. The result stays in Marina's artifact
store and its exact publication request is journaled before Deck receives a
reply and optional task. Local model and engine proof is still required; see
[CHAT-04 evidence](../../../docs/validation/chat-04-2026-09-23.md) and
[CHAT-06–08 evidence](../../../docs/validation/chat-06-08-2026-09-25.md).

Use the [public API](api-v1.md) to discover and invoke an installed profile, or
`workload-profile` for offline inspection of the exact authority grant. A task
needs that sole grant, explicit acceptance criteria, at least one attempt, a
positive token/time budget within the profile limits, and a bounded cost in the
configured currency. Retain the installed approval policy. For a controlled
acceptance run, `serve --intake-task-id <task-id>` discovers only that task;
`--intake-enabled=false` disables all new discovery. Both settings still
reconcile already journaled work and cancellation.

The date-scoped [acceptance helper](../../../scripts/validate-agent-poc.py)
captures the local installation proof. It is not a general deployment or public
profile-selection API. The [AGENT-07 through AGENT-10 report](../../../docs/validation/agent-07-10-2026-09-21.md)
records the implemented API/tool/context/inspection checks separately from the
PoC's live model proof. The [AGENT-11 through AGENT-13 acceptance](../../../docs/validation/agent-11-13-2026-09-22.md)
records the shared failure matrix and combined live v2 MVP proof. Use the
[two-workload installation guide](../../../deploy/kubernetes/agent-platform/README.md)
for pinned upgrades, context/profile compatibility, credential reuse and recovery limits.
