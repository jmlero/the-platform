# Decision 0007: Own the execution system and focus the first usable release

- Status: accepted product direction; implementation and deployment gates remain explicit
- Date: 2026-09-09
- Scope: Deck and Marina

## Decision

Marina is a Kubernetes-native system installed on infrastructure controlled by
its operator. Ownership, sovereignty, privacy, and independence from a mandatory
hosted control plane are product requirements, including for prospective European
customers. Remote execution alone does not express that value.

The first usable release handles explicitly authorized, bounded repository fixes.
One operator submits work in Deck; Marina runs an isolated worker, verifies its
output, observes required CI, and returns a pull request
ready for human review or a focused explanation of why it stopped. Merging and
deployment stay under human control.

Deck focuses on task authorization, decisions, results, and exceptions. Its
existing channels and operational records remain compatible, but expanding chat,
general project management, and production-operation views is deferred. The two
services retain independent modules, databases, identities, and the public HTTP
contract. One supported installation should make the pair usable together.

Kubernetes ownership does not by itself keep model inputs private. The installation
must explicitly configure its model endpoint, Git service, artifact storage, and
permitted network destinations. Hosted inference is an operator-selected data
boundary, never an implicit dependency or a claim of fully local processing.
Private endpoints must be supportable without a hosted Marina control plane.

### Deployment-target clarification — 2026-09-17

The product targets vanilla Kubernetes through standard APIs, without requiring
a particular cloud provider or distribution. Each installation supplies the
documented storage, DNS, and network-policy capabilities. Use the operator's
local Podman/Kind Kubernetes for development tests; no hosting-vendor choice is
needed to proceed.

### Local acceptance scope clarification — 2026-09-17

The operator removed the laptop-off acceptance task (RUN-10). The current test
cluster runs on the Mac, so the Mac stays running during acceptance. An independent
host and a disconnected-laptop demonstration are not first-release prerequisites.
RUN-09 retains the real repository outcome: one review-ready PR, a separate
verification Job, current required CI and downloadable evidence. Recovery,
cancellation, failure and budget exercises run against that same local installation.
The operator also clarified that this test installation is localhost-only, not a
LAN service. RUN-08 verifies trusted TLS and operator authentication through the
existing `https://localhost:8443` Podman/Kind mapping, without an interactive
port-forward. LAN exposure and a second-device sign-in are not required.

Keep the remaining task IDs stable and retire RUN-10 without reusing its ID.
The broader always-on product direction is unchanged; local evidence must identify
its host and does not claim operation while that host is asleep or powered off.

### Worker-failure acceptance scope clarification — 2026-09-18

The operator removed RUN-17: deliberately failing or evicting an active worker
and demonstrating a follow-up task is not a first-release acceptance requirement.
Retire its ID without reusing it. The remaining live exercises are RUN-09 and
RUN-11 through RUN-16; RUN-18 records their results. This removes that acceptance
exercise, not the invariant that a failed worker cannot produce false completion.

## First implementation

One configured Codex CLI image uses one explicit Responses-compatible model
endpoint. GitHub/GHES is the initial source adapter; the Kubernetes Job adapter
uses the cluster API directly. These are replaceable implementations, not a
requirement to use a hosted model or Git service. One coding attempt and one
independent verification attempt form the current workflow. Failure or requested
review changes produces a blocker and a new task; automated revision loops wait.

The plan binds the starting commit in its immutable context and text. Its optional
`source_revision` is empty because the output commit does not exist at planning
time. Verification evidence binds the actual published commit, and Marina checks
the current branch/PR head and required CI again before completing. Deck retains
its existing exact-revision gate for plans that provide a fixed source revision.
This uses the existing version-1 contract rather than mislabeling base-revision
evidence as proof of a changed output.

Installation configuration is hashed into durable workflow state. Changing it
during a run blocks that run for review; it cannot silently widen authority.
Worker and artifact credentials are distinct, Deck remains the human ledger,
and Marina's artifact byte store is accessed through authenticated HTTP.

## Delivery discipline

- Reuse one coding harness and one Kubernetes Job runtime before extending adapters.
- Keep workers separate from control-plane and source-publication authority.
- Bind task authorization, runtime configuration, and verification to immutable inputs.
- Preserve durable results outside disposable Jobs and enforce bounded cancellation.
- Require real-process protocol checks and a repeatable end-to-end acceptance path.
- Keep implemented, locally verified, and verified on a real installation distinct.
- Expand task classes only after real use demonstrates lower total operator effort.

Production remediation, autonomous discovery of arbitrary work, multi-tenancy,
additional provider families, and a plugin marketplace are outside this release.

## Superseded direction

The broader dark-factory vision remains a long-term direction. It does not authorize
parallel feature expansion before the first usable repository workflow. Deck's
"better Linear" and "Slack-like" ambitions no longer define its current scope.
