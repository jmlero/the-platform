# Nine pending items: PRD and project-profile follow-up

Date: 2026-10-05. Environments: macOS development host, local Podman on
`podman-machine-default-root`, and read-only inspection of `kind-marina-local`.
This report records source, real-process and offline packaging checks. It does
not establish installed native workflow acceptance.

The operator selected LAND-02, LAND-03, VER-01, TEST-01, PRD-01, PRD-02, PRD-03,
FLOW-01 and PROJ-01, excluding deferred work. The earlier instruction **Keep
GitHub unchanged** remains in force. This follow-up made no commits, pushes,
remote ref changes, model calls, deployment changes or private configuration
changes. Existing unrelated work was preserved; nothing was staged.

This follows the [landing and packaging report](land-02-03-ver-01-test-01-2026-10-03.md)
and [PRD publication report](prd-01-and-landing-checks-2026-10-03.md).

## Implemented changes

[Decision 0030](../decisions/0030-ordered-prd-tasks-and-acceptance.md) defines the
ordered PRD handoff. A conversation proposal must name an approved, landed PRD
publication in the same project and thread, with its exact revision. Deck
freezes the source path, repository, revision and digest. A proposal creates no
work until an operator approves the complete ordered list with explicit
per-task limits and an available execution profile. Approval atomically creates
Ready tasks, dependency order and immutable PRD references. Replays return the
same tasks; changed inputs conflict. Marina checks PRD source ancestry even if
ordinary dependencies are subsequently edited.

The originating thread displays approved tasks, criteria and exact landed
results. Operator acceptance binds the current result-set digest and requires
valid evidence for every task. An explicit follow-up request produces another
proposal in the same thread; its approval and new results require fresh
acceptance. Export/import preserves source links, approvals and acceptance.
Import rejects tampered plans, detached task references and forged acceptance
of unfinished work, while retaining historical acceptance after later follow-ups.

The strict model response schema now includes both nullable `prd` and
`prd_tasks`; previously the gateway schema could not produce a typed PRD outcome.
Structured conversation recovery retains the proposal without another model run.
Migration `022_prd_tasks.sql`, capability `conversation.prd.tasks`, operator
routes, OpenAPI and canonical provider/consumer fixtures accompany these changes.
Matching Deck, Marina and conversation BYO images are required.

[Decision 0031](../decisions/0031-explicit-per-project-profiles.md) defines explicit
private Marina project profiles. Legacy single-profile configuration remains
valid. A versioned profile list selects each project's repository, checks,
execution settings and credential references. Shared service settings must
agree. One control controller and journal admit at most one active repository
task across all configured projects, including ambiguous acquisition recovery.
Gateways select the project from durable attempt identity, retain downstream
credential checks and reject unknown or unconfigured projects. Installation
preflight requires each project's own service grants.

## Verification

| Check | Result |
| --- | --- |
| Root `make check` | Passed: 29 installation-script tests; CLI, Deck and Marina Go checks and vet; 30 frontend unit tests; Svelte diagnostics with zero errors and warnings. Existing non-fatal Python SQLite ResourceWarnings remain. |
| Root `make conformance` | Passed against real Deck processes, including CLI and the new two-project profile case. |
| macOS `npm --prefix deck/web run test:e2e` | Five production-frontend browser tests passed in 7.2 seconds. |
| Native image: offline npm restore, unit and browser suites | 94 locked packages restored offline; 30 unit tests and five Chromium browser tests passed. Browser run: 12.2 seconds. |
| Matching verifier image: same offline suites | 94 locked packages restored offline; 30 unit tests and five Chromium browser tests passed. Browser run: 12.0 seconds. |

Focused regressions were first observed failing, then passed for ordered
approval, incomplete acceptance, import integrity, PRD ancestry independent of
editable dependencies, strict response schema, global single-task admission,
gateway project isolation and per-project service grants. The last import
regression demonstrated that a manufactured current acceptance could otherwise
be imported for unfinished tasks; import now validates landing evidence at the
recorded acceptance time.

`TestRealProjectProfiles` passed in 0.02 seconds. It creates two projects through
a real Deck operator API, publishes distinct repository/profile bindings, then
uses shared Marina admission to claim the higher-priority project. Its frozen
repository, checks, budget and credential-file reference belong to that project;
the other task remains unclaimed. Authentication grants are synthetic, confined
to the disposable fixture and restored afterward.

The five browser tests use a disposable real Deck process and its production
frontend. The new case approves ordered PRD tasks, reloads retained state,
rejects premature acceptance, observes landed results in another tab, accepts
the result set, requests and approves a follow-up, then accepts the new result
set. Conversation outcomes and execution evidence are deterministic service
fixtures. Existing real-process landing, moved-base rebase, conflict, repair and
restart scenarios also pass with deterministic Git/runtime boundaries. None of
these tests establishes live model, GitHub or authenticated installation proof.

## Local artifacts

The tested source archive has SHA-256
`4d34eb6f435c2b462b172779b5ebb70d1dc4278908aa6ea133cd210e2064d2b1`.
It captures the final executable sources before this final report and date-only
documentation updates. It excludes ignored private configuration, dependency
trees and runtime data. Both image runs used `--network=none --read-only
--user 65532:65532 --cap-drop=all --security-opt=no-new-privileges`, a writable
temporary filesystem and a fresh source copy. They ran:

```sh
npm --prefix deck/web ci --offline
npm --prefix deck/web test
npm --prefix deck/web run test:e2e
```

The suites build the production frontend and Go backend with packaged inputs.
Candidate images were built using cached inputs, `--pull=never` and
`--network=none`. These are local Podman manifest digests, not registry releases
or containerd installation pins. The native toolchain image is reused unchanged.

| Local image | Podman manifest digest |
| --- | --- |
| `localhost/the-platform/deck:platform-pending-20261004` | `sha256:1ca722644cb98c056590a363df513c8b55dbaa7bb250d55b58446251a41cda8e` |
| `localhost/the-platform/marina:platform-pending-20261004` | `sha256:ad8c342ee9e495bcbe4cc7b5de76518120b672fb2310b389a2f0d2dde85cb86e` |
| `localhost/the-platform/marina-conversation-byo:platform-pending-20261004` | `sha256:bc041efa587ada5daa3c27c4ad1124997e5d5038c794bdcb8de98b6ced8dc9eb` |
| `localhost/the-platform/marina-verifier:platform-pending-20261004` | `sha256:c9de349d6bfd1061aad2a1e5ff1a550b5addfbaa11874d1f66e171742b0df7c5` |
| `localhost/the-platform/marina-codex:platform-ver01-20261003-final` | `sha256:ffef74008564a7356cae14951dd8cae570198526c377a00632b8f013c140e150` |

Linux arm64 executable SHA-256 values:

| Executable | SHA-256 |
| --- | --- |
| Deck | `b1295d34cf52c242562831c2e5ecfca30afefac97eafc7de7b23ad7aa6dbf7ea` |
| Marina, including verifier copy | `f00fe2503b64e46fdfbc12a2027c50097fc0576b5dba9bfd5d1060977e0817ad` |
| Conversation BYO | `dcf70cf1aaf015241533bb5ae501cb091efbe7e919b878290eba4fa31e6833ac` |

## Acceptance still required

Read-only local Kind inspection found Deck and Marina ready on the earlier
release, with Marina retaining `--intake-enabled=false`. Installed Deck is
`sha256:57fb39f7e0a2a9b2f971a66e111b59f714d73031a06ef9a04954bde65ded159d`;
Marina is `sha256:b76171054f3d34831246755dadef801f43e72b29f50ef7b83e561ea9e224f0a2`.
The private workflow still allows `src` and `tests`, excluding `docs/prd`, and
selects verifier `sha256:873282b0b43aa72279414fd8121680af902f4a50cfc49511cfec6c5602f793ac`.
Those images, scope, snapshots, intake and private settings were left unchanged.

| Item | Status retained | Missing acceptance |
| --- | --- | --- |
| LAND-02 | Verify | Updated local native installation: evidenced landing and prerequisite release. |
| LAND-03 | Verify | Live moved-main rebase with fresh review/checks/CI, and conflict blocking. |
| VER-01 | Verify | Matching installed verifier/native profiles and engine golden snapshot, followed by native execution. |
| TEST-01 | Verify | Authorized source publication and observed remote CI browser run. |
| PRD-01 | Verify | Matching installation and live approved PRD publication with explicit document scope. |
| PRD-02 | Verify | Installed native proposal, ordered approval and implementation from the landed PRD. |
| PRD-03 | Verify | Installed result acceptance and follow-up flow after evidenced implementation landings. |
| FLOW-01 | Open | Complete local Kind milestone: PRD and task landings, unit/end-to-end checks, one repaired failure and task restart. |
| PROJ-01 | Verify | Installed two-project proof after FLOW-01, preserving global single-task admission. |

Live publication/rebase/milestone cases require acceptance-repository writes
excluded by the current GitHub instruction. Remote CI also requires authorized
publication. The deterministic checks and local images do not replace those
results. No item was marked Done on this evidence.
