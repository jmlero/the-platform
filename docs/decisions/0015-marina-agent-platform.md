# Decision 0015: Extend Marina into a bounded Kubernetes agent platform

- Status: accepted direction; PoC and MVP implementation pending
- Date: 2026-09-21
- Scope: Marina execution, Deck governance, and AGENT-01 through AGENT-13

## Decision

Extend Marina toward an operator-owned agent execution and governance platform
on Kubernetes, with software engineering as its first supported workflow.
"AgentCore for Kubernetes" describes the capability direction; it does not
require AWS API compatibility, feature parity, or reuse of `platform-marina`.

Keep the existing Marina Go module, service and durable journal. Separate shared
execution capabilities from workflow-specific steps through internal packages
and narrow interfaces. A third product, separate runtime service, new database
or workflow engine is not required for this proof. Consider a service split only
when measured scaling, security or failure-isolation needs justify it.

The first extension is one bounded, read-only analysis workload using immutable
documents or diagnostic data and a genuinely different harness. The existing
repository workflow continues to produce a PR ready for human review. The
analysis workload produces a validated report without requiring a repository,
source commit or PR. The harness ecosystem is selected below; define its exact
fixture and bounded configuration under AGENT-02.

## Harness ecosystem choice

On 2026-09-21, the operator selected the **LangChain open-source ecosystem** for
the second harness. This is an accepted implementation direction, not a claim
that the harness or its gateway integration is implemented or validated.

Start with LangChain's `create_agent`, using its underlying LangGraph runtime,
inside a Marina-managed Kubernetes worker. Use custom LangGraph workflows when
a workload needs explicit branching or checkpoints within an agent run. Deep
Agents remains a possible later harness when its context management, skills or
delegation capabilities serve a concrete workload; it is not required for the
initial read-only analysis fixture. See the [OSS stack overview](https://www.langchain.com/oss-overview)
and [LangChain documentation](https://docs.langchain.com/oss/python/langchain/overview).

The selection covers **open-source, self-hostable libraries only**. Run the
agent framework and harness on operator-controlled Kubernetes without AWS
services or a vendor-hosted control plane. LangSmith, Managed Deep Agents, and
proprietary deployment or governance products are outside this choice. Check
the license and self-hosting requirements of any additional ecosystem component
before adopting it; the ecosystem brand alone does not establish eligibility.

The choice favors reusable model/tool integrations and a path from a standard
agent loop to richer harnesses. Pydantic AI was considered for the focused typed
analysis worker; ecosystem reuse across Marina's broader agent-platform direction
was the deciding preference.

Deck retains human intent, authorization, decisions and results. Marina retains
Jobs, isolation, leases, budgets, cancellation, durable execution and evidence
admission. Keep framework types and any agent-step checkpoints behind the
harness boundary; adopting LangGraph does not replace Marina's control service
or introduce a separate platform workflow engine. Model and tool access must
use Marina's governed boundaries.

The operator confirmed on 2026-09-21 that AGENT-01 is the completed harness
decision. AGENT-01 is **Done**; specifying the read-only fixture, expected
findings, output checks, model/tool destinations and bounded budgets belongs
to the versioned workload and harness profiles in AGENT-02.
Gateway compatibility, usage accounting, cancellation and live Kubernetes
execution still require evidence under the implementation and acceptance tasks.

## Ownership and contracts

- Deck remains authoritative for human intent, task authorization, approvals,
  decisions, commands and visible results. API invocation uses that same ledger.
- Marina owns workload/harness configuration, immutable execution inputs,
  runtime attempts, scoped tool access, budgets, recovery and artifact bytes.
- Preserve separate stores and the versioned HTTP/JSON integration. Profile
  discovery and selection must not introduce a second task ledger or let clients
  bypass authorization by calling a worker endpoint.
- Share lifecycle, authority, cancellation, usage, artifacts and recovery across
  workloads. Keep Git publication, PR/CI checks and source-revision semantics in
  the repository workflow. Each workload declares its own admissible evidence.
- Pin workload and harness revisions for each execution. Later edits must not
  silently change active work; unsupported capabilities fail closed.
- Preserve version-1 semantics. Update provider contracts and consumer
  conformance together; use a new protocol major if a change is incompatible.
- Reuse context-package foundations and the ownership in
  [decision 0014](0014-project-repository-and-knowledge.md). Execution state and
  authority remain structured records, independent of retrieved agent memory.

## Delivery boundaries

The [TODO](../../TODO.md#agent-platform-poc-and-mvp-on-kubernetes) defines two
gates: AGENT-01 through AGENT-06 prove that two workloads and different harnesses
share the execution foundation; AGENT-07 through AGENT-13 make that bounded
platform usable through a documented API, governed tool/context access,
inspectable outputs, failure recovery and a repeatable installation.

Start with one operator, one active task, Kubernetes Jobs and the supported Deck
pair. Interactive sessions, pause/resume, headless packaging, multi-tenancy, HA,
general memory/evaluation/browser services, a marketplace and autonomous
merge/deploy remain outside this PoC/MVP. Full AgentCore parity is not an exit
criterion. Existing isolation must be tested and described accurately; running
in a Pod does not establish VM isolation.

This is follow-on work after the local repository acceptance recorded on
2026-09-21. [Decision 0007](0007-sovereign-first-usable-release.md) continues to
define that release's supported behavior. This decision activates the second
harness proof previously deferred under LATER-02; it does not claim the new
capabilities are implemented. VALUE-01 remains closed by the operator and is not
a prerequisite. Environment, credential, spending and external-action boundaries
remain unchanged.
