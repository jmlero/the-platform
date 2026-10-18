# Working in The Platform

## Scope

This file applies to the complete monorepo. More specific instructions in a product directory add to these rules. Keep this file as a concise operating map; detailed architecture, plans, decisions, and test evidence belong in their dedicated documents.

## Read first

Start with [docs/INDEX.md](docs/INDEX.md): it maps every document, says who it is for, and says where each kind of fact belongs. Before planning or changing cross-product behavior, read:

1. [docs/VISION.md](docs/VISION.md): what we are building and why
2. [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): components, tech stack and safety rules
3. [docs/status.md](docs/status.md): what is supported today
4. [docs/backlog.md](docs/backlog.md): the order of work

Then read the affected component's guidance before changing it:

| Component | Required guidance |
| --- | --- |
| Deck | [deck/AGENTS.md](deck/AGENTS.md) and [deck/README.md](deck/README.md) |
| Marina | [marina/AGENTS.md](marina/AGENTS.md) and [marina/README.md](marina/README.md) |
| CLI | [cli/README.md](cli/README.md) and the public Deck or Marina API used by the change |

Read relevant records under [docs/decisions/](docs/decisions/) before revisiting an architectural or product choice. Explicit user decisions take precedence; update docs/VISION.md when the direction changes and docs/status.md when the current state changes. Check claimed behavior against source and tests.

## Product boundaries

- `cli/` owns `platformctl`: a stateless operator client of Deck's operator API and Marina's platform API; it owns no authoritative state.
- `deck/` owns Deck: human steering (projects, threads, backlog, PRDs, acceptance). It is a client of Marina's platform API.
- `marina/` owns Marina: the platform API (`marina.dev/v1`, HTTP/JSON and Kubernetes resources), the run ledger, and durable autonomous execution ([decision 0033](docs/decisions/0033-marina-platform-api.md)). Marina calls no client.
- Keep separate Go modules, binaries, databases where applicable, images, identities, deployments, migrations, and release versions.
- Neither product may import the other's internal packages. Integration goes through the versioned HTTP/JSON contract.
- Neither service may read the other's database. Terminal clients must not import product internals or read product databases; they interact through supported public APIs.
- Marina owns the platform contract under `marina/api/`: OpenAPI, CRDs and canonical fixtures. Deck's `deck/api/` documents only its operator API.
- Marina's API sandbox (`go build -tags sandbox`, command `api-sandbox`) is the real process every client's contract tests run against.
- Keep OpenAPI, capability discovery, fixtures, persistence, handlers, and consumer expectations synchronized. Additive changes must preserve version-1 semantics; breaking semantics require a new protocol major. See the [contract map](docs/ARCHITECTURE.md#contract).
- Preserve durable intent, idempotency, version checks, lease fencing, bounded budgets, and source-bound evidence. Handle lost responses, restart, cancellation, and partial success without duplicate work or false completion.

## Documentation

Follow the layout in [docs/INDEX.md](docs/INDEX.md). Capitalized documents (INDEX, VISION, ARCHITECTURE) are for the human: keep them short, plain and free of low-level detail. Lowercase documents are working documents that agents keep current.

- One fact lives in one place; link to it instead of restating it.
- Do not add Markdown files at the root or directly under `docs/` unless the user asks. New material goes into an existing document, `docs/decisions/`, `docs/plans/` or `docs/validation/`.
- Delete a plan under `docs/plans/` once its work ships and ARCHITECTURE and status describe the result.
- Do not edit validation reports after the fact, except to repair links.

## Current state

[docs/status.md](docs/status.md) is the only description of what is supported, implemented but not yet accepted, and not supported. Link to it; do not restate it in other documents. Update it in the same change that ships, removes or re-scopes a capability. Ledger records and reusable foundations alone do not establish a supported workflow capability.

For workload profile work, follow the [profile contract](marina/docs/workloads/profiles-v1.md), [API guide](marina/docs/workloads/api-v1.md) and [installation guide](deploy/kubernetes/agent-platform/README.md). The diagnostic workload is retired ([decision 0023](docs/decisions/0023-focus-kagent-cutover.md)); do not extend it.

## Project repository and context

Follow [decision 0014](docs/decisions/0014-project-repository-and-knowledge.md) when implementing project context; its explicit-source workflow is implemented under CTX-02 through CTX-08; evidence and boundaries are in [the context validation report](docs/validation/ctx-02-08-2026-09-21.md).

- Start with one GitHub repository per project. Reuse Deck's project identity and Marina's versioned project binding; repository connection, execution settings, and credential references remain Marina-owned.
- Deck owns project-authored knowledge and selected source registrations. Git remains authoritative for repository-owned instructions, documents, and decisions. Reference existing Deck decisions and messages by identity and revision instead of duplicating their authoritative records.
- Marina assembles immutable execution snapshots from explicitly selected, versioned sources. Persist provenance, digests, trust, and revocation information; recovery uses recorded inputs, and routine edits must not silently change active work.
- Context cannot expand authority or budgets. Treat retrieved text and conversations as data unless explicitly validated as guidance. Begin with explicit sources and existing context-package/journal foundations; an automatic memory or retrieval framework is not required.

## Workflow

- Inspect the complete root Git state, including staged, unstaged, and untracked changes, before editing. Preserve unrelated work and stage only changes belonging to the requested task.
- Make cross-product contract changes together; when committing is authorized, keep mutually dependent changes in one atomic commit.
- Keep future schemas visibly marked as planned; do not advertise a provider capability before its behavior exists.
- Keep [backlog](docs/backlog.md) IDs stable. Mark an item Done only when its acceptance criteria are satisfied and link the evidence; retain Verify when installation-specific proof is still missing.
- Record consequential cross-product decisions under `docs/decisions/`.
- Continue work within the user's existing authorization. Ask only for missing intent or authority, and report concrete results, verification, and remaining blockers.

## Verification

Run the narrowest relevant checks first, from the owning Go module or frontend directory. Root `go.work` joins independent modules; it does not create a root Go module.

| Change scope | Broader checks from the repository root |
| --- | --- |
| CLI | `make -C cli check` |
| Deck backend or frontend | `make -C deck test check` |
| Marina | `make -C marina check` |
| Installation scripts or configuration | `make check-installation`, then relevant documented local runtime checks |
| Joint HTTP/JSON protocol | `make check` and `make conformance` |

A joint contract item is complete only when both product checks and real-process conformance pass. Root `make conformance` runs Deck and the CLI against a real Marina API sandbox, the CLI against a real Deck, and Marina's own API contract tests; deterministic Git/runtime doubles do not establish live model, Git-provider, Kubernetes, or browser acceptance.

For documentation-only changes, verify links, referenced commands, and `git diff --check`. Report checks actually run and distinguish deterministic checks from live local runtime evidence. Store redacted installation evidence under `docs/validation/`.

## Installation and Kubernetes testing

- The user authorizes two development/test environments: local Podman/Kind and the existing GCP Kubernetes cluster. Use the [local Podman/Kind profile](deploy/kubernetes/local-podman/README.md) for local checks and `/Users/jmolero/personal/github/infra-gcp` as the GCP infrastructure source of truth; read its `AGENTS.md` and `README.md` before GCP work. See [decision 0010](docs/decisions/0010-initial-installation-services.md).
- Inspect and reuse the dedicated `kind-marina-local` context on `podman-machine-default-root`. Select that context and connection explicitly; preserve unrelated containers, contexts, credentials, and persistent data. Required test deployments and service restarts in this profile are covered by the local testing authorization.
- GCP uses project `jmlero`, region `europe-west3`, GKE Autopilot cluster `autopilot`, and the configured `gcloud` CLI. Use its IAM-authenticated DNS endpoint and explicit context `gke_jmlero_europe-west3_autopilot`; use a separate kubeconfig to preserve the local default. Relevant test deployments and restarts are authorized within the infrastructure repo's EUR 10/month total budget; check existing spend and projected total before adding billable workloads or storage. Keep temporary workloads short-lived and clean up only resources created for the test.
- Follow [decision 0010](docs/decisions/0010-initial-installation-services.md): vanilla Kubernetes, OpenRouter inference, GitHub/GHCR, separate SQLite stores, and artifact files on persistent volumes. Backup automation and restore drills are deferred for the pilot; persistence and restart recovery remain required.
- Keep build/setup downloads separate from runtime egress. Prepackage worker and verification dependencies; preserve gateway-mediated model/source access and worker isolation.
- Inspect current private configuration and the installation guide before live tests. Do not infer cluster credentials from developer logins, enable new task intake, or spend beyond existing authorization merely because the local services are healthy.
- First-release acceptance uses local Kubernetes with the Mac running. The operator removed RUN-10; do not require an independent host or laptop-off proof. Identify the local environment when recording Jobs, persistence, recovery, cancellation, and network-isolation evidence.
- Keep the local test installation accessible through `https://localhost:8443`; LAN exposure and second-device sign-in are outside the current acceptance scope. GCP availability does not transfer local acceptance evidence or require a migration; record each test's environment explicitly.

## Git identity and external effects

- Use `jmlero@hotmail.com` for the user's Git author and committer email unless explicitly told otherwise. Before committing, check effective identity, including repository-local configuration and environment overrides. Do not infer identity from old commits or GitHub metadata; `jose.molero.ext@riag-digital.com` is not the user's email.
- Do not commit, push, create a remote, merge, deploy outside the authorized local and GCP development/test environments, modify production, or send external messages unless explicitly requested or already authorized for the task. Backlog entries and product workflow capabilities do not themselves authorize these actions.
- Never commit credentials, local databases, generated dependency trees, runtime data, or `.codex/` artifacts. Keep secret values out of source, prompts, logs, task content, and evidence; use private files and credential references.

<!-- harness-workshop:start block/ponytail -->
<!-- harness-workshop:source block/ponytail@1.0.0 integrity sha256-8ozvvuvXP6Df83cMrc2DMjIzolw40npIkxiog5OXA9w= -->
## Minimal implementation

- Choose the smallest safe change that fully meets current requirements.
- Reuse project code, standard libraries, native features, and installed
  dependencies before adding code or dependencies.
- Avoid speculative abstractions, configuration, extension points, and wrappers.
- Fix shared root causes; preserve validation, error handling, security,
  accessibility, and data safety.
- Verify non-trivial behavior with the smallest relevant check.
<!-- harness-workshop:end block/ponytail -->

<!-- harness-workshop:start block/tdd -->
<!-- harness-workshop:source block/tdd@1.0.0 integrity sha256-54BMZaEMWeVKGxmv91DjPLn5qEtdyCv6L6WtnNQTPHY= -->
## Test-driven development

For features and bug fixes:

1. Write a failing test that describes one expected behavior.
2. Make the smallest change that passes it.
3. Refactor while keeping the tests green.

Keep tests independent, name them by behavior, mock external boundaries when
appropriate, and run the relevant checks after each change.
<!-- harness-workshop:end block/tdd -->

<!--as:rules-->
Project rules: read and follow @.agents/rules.md before starting any task.
<!--/as:rules-->
