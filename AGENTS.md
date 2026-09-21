# Working in The Platform

## Scope

This file applies to the complete monorepo. More specific instructions in a product directory add to these rules. Keep this file as a concise operating map; detailed architecture, plans, decisions, and test evidence belong in their dedicated documents.

## Read first

Before planning or changing cross-product behavior, read:

1. [README.md](README.md)
2. [ARCHITECTURE.md](ARCHITECTURE.md)
3. [TODO.md](TODO.md)

Then read the affected component's guidance before changing it:

| Component | Required guidance |
| --- | --- |
| Deck | [deck/AGENTS.md](deck/AGENTS.md), [deck/README.md](deck/README.md), and [VISION_DECK.md](VISION_DECK.md) |
| Marina | [marina/AGENTS.md](marina/AGENTS.md), [marina/README.md](marina/README.md), [VISION.md](VISION.md), and [DARK_FACTORY.md](DARK_FACTORY.md) |
| CLI | [cli/README.md](cli/README.md) and the public Deck API used by the change |

Read relevant records under [docs/decisions/](docs/decisions/) before revisiting an architectural or product choice. Explicit user decisions take precedence; update the relevant source document when direction changes. Visions include future targets, so check implemented behavior against source, tests, and the current release boundary.

## Product boundaries

- `cli/` owns `platformctl`: a stateless operator client that uses Deck's public HTTP/JSON API and owns no authoritative platform state.
- `deck/` owns Deck: human steering and the authoritative work-control ledger.
- `marina/` owns Marina: durable autonomous execution and reconciliation.
- Keep separate Go modules, binaries, databases where applicable, images, identities, deployments, migrations, and release versions.
- Neither product may import the other's internal packages. Integration goes through the versioned HTTP/JSON contract.
- Neither service may read the other's database. Terminal clients must not import product internals or read product databases; they interact through supported public APIs.
- Deck owns its provider-specific OpenAPI and canonical wire fixtures under `deck/api/`.
- Marina owns provider-neutral ports and the reusable consumer conformance suite under `marina/internal/workcontrol/`.
- Keep OpenAPI, capability discovery, fixtures, persistence, handlers, and consumer expectations synchronized. Additive changes must preserve version-1 semantics; breaking semantics require a new protocol major. See the [contract map](contracts/README.md).
- Preserve durable intent, idempotency, version checks, lease fencing, bounded budgets, and source-bound evidence. Handle lost responses, restart, cancellation, and partial success without duplicate work or false completion.

## Current supported behavior

- The supported Deck–Marina pair uses HTTP/JSON and separate stores. Headless and in-process packaging remain future work.
- The configured repository workflow supports one repository and one active task, with one implementation and one separate deterministic verification attempt. It ends at a PR ready for human review or a blocker; failed checks and requested changes block.
- Cancellation waits for observed worker termination. Pause, resume, replan, retry, and reduce-authority commands are rejected by this workflow. Automatic revision loops, independent semantic review, merge and deploy authority remain future work.
- Without workflow configuration, Marina remains in control-only mode. Disabling new task intake does not pause already journaled executions.
- Channel history and explicit task promotion exist. Versioned project knowledge, selected conversation excerpts, repository association, and immutable workflow context are implemented. Conversational agent dispatch and persistent unread-result state remain pending. Ledger records and reusable foundations alone do not establish a supported workflow capability.

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
- Keep [TODO.md](TODO.md) IDs stable. Mark an item Done only when its acceptance criteria are satisfied and link the evidence; retain Verify when installation-specific proof is still missing.
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

A joint contract item is complete only when both product checks and real-process conformance pass. Root `make conformance` exercises the CLI and Marina against real Deck processes; deterministic Git/runtime doubles do not establish live model, Git-provider, Kubernetes, or browser acceptance.

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
