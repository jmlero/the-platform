# PRD-01 implementation and landing verification follow-up

Date: 2026-10-03. Environments: macOS development host and local Podman on
`podman-machine-default-root`. This is source, browser and offline packaging
evidence, not acceptance of the updated installed native workflow.

This follows the [LAND-02/03, VER-01 and TEST-01 report](land-02-03-ver-01-test-01-2026-10-03.md)
without changing that historical evidence. The operator explicitly chose
**Keep GitHub unchanged**. No commits, pushes, remote ref mutations, model calls,
deployment updates or private installation configuration changes were made in
this follow-up.

## Implemented PRD path

[Decision 0029](../decisions/0029-approved-prd-publication.md) defines the handoff.
A chat outcome can contain a bounded, immutable Markdown draft under `docs/prd/`.
Deck binds its text and path to the invocation's repository and records its
digest. Publishing the draft creates no task. The thread displays its complete
text, repository and destination, then lets the operator select bounded time and
token limits and explicitly approve publication using an installed
`repository/v2` profile.

Approval atomically records the operator, freezes the document and budget into
one Ready task, and links that task to the thread. Service bearer credentials
cannot approve. Changed digests, repository bindings, replay budgets and invalid
or unavailable profiles are rejected. Restart, new-key approval replay and
project export/import retain the original publication task and typed document.

Marina retains the typed draft in durable conversation output and passes the
approved document to the worker. Before review and again before source
publication, it requires exactly the approved file and bytes, as an ordinary
file, with no deletion, extra changes or executable mode. The task uses existing
independent review, clean checks, exact-commit CI and evidenced landing. It does
not derive or implement product tasks from the PRD.

The change includes migration `021_prd_drafts.sql`, additive v1 capability
`conversation.prd.draft`, OpenAPI schemas and a canonical provider/consumer
fixture. Matching Deck, Marina and conversation BYO releases are required.

## Checks run

| Check | Result |
| --- | --- |
| Root `make check` | Passed: installation scripts (28 tests), CLI/Deck/Marina Go checks and vet, 30 frontend unit tests, Svelte diagnostics with no errors or warnings. Existing non-fatal Python SQLite ResourceWarnings remain. |
| Root `make conformance` | Passed against real Deck processes, including CLI, authenticated durable threads, protocol fixtures and repository workflow scenarios. |
| `make -C deck test check` after the final browser event fix | Passed; 30 frontend unit tests and no Svelte errors or warnings. |
| `npm --prefix deck/web run test:e2e` on macOS Chrome | All four browser tests passed in 5.6 seconds. |
| Native image: offline npm restore, frontend unit and browser suites | All 94 locked packages restored; 30 unit tests and four Chromium browser tests passed; browser run 10.7 seconds. |
| Verifier image: the same offline suites | All 94 locked packages restored; 30 unit tests and four Chromium browser tests passed; browser run 10.4 seconds. |

The new real-process `landing_prd` scenario approves through Deck's HTTP API,
reopens Marina's journal between workflow transitions and finishes with three
runtime starts, one commit and one landing. `landing_prd_changed` changes the
approved text and blocks after one runtime start with no source write. The
existing landing, prerequisite, moved-main rebase, conflict, failed CI, repair and
recovery scenarios also pass. Runtime, model and Git boundaries in these tests
are deterministic doubles; their commits and landings are not GitHub evidence.

Focused tests cover PRD path/content validation, approval identity and budget,
operator-only HTTP authorization, restart/export/import, provider-consumer
digest agreement, structured model output and lost-response conversation
recovery without another model run or task creation.

The fourth browser scenario reviews the exact draft in its thread, approves a
bounded budget, verifies frozen task bytes, observes the approval in a second
open tab, reloads the page and retries with another idempotency key without
creating a second task. The other three scenarios retain project/chat, evidenced
landing and legacy operator-acceptance coverage. These use a real disposable
Deck process and its embedded production frontend; conversation and execution
results are service fixtures. They do not establish live model or authenticated
installation browser acceptance.

Failing tests first exposed three supporting defects: ready conversation
delegation rejected a valid three-attempt independent-review profile, the agent
selector assumed capabilities could never be absent, and an already-open tab
did not refresh on `prd.approved`. Those regressions now pass. The conformance
fixture also needed explicit existing outcome-publication and message-list
grants; production permissions were not broadened.

## Offline artifacts

The final disposable source archive had SHA-256
`07043e1102acbe829e62a7b59eb6d243f713052627de1b318df1762a07793ee7`.
Both image runs used `--network=none --read-only --user 65532:65532
--cap-drop=all --security-opt=no-new-privileges`, a writable temporary filesystem
and a fresh source copy. Commands inside each container were:

```sh
npm --prefix deck/web ci --offline
npm --prefix deck/web test
npm --prefix deck/web run test:e2e
```

The suites built the production frontend and real Go test backend using packaged
dependencies; they did not download dependencies or browsers at runtime.
The shared toolchain and native adapter preparation are described in the
[earlier packaging report](land-02-03-ver-01-test-01-2026-10-03.md).

Final candidate images were built locally with cached inputs, `--pull=never`
and `--network=none`. These are Podman manifest digests, not registry publication
or containerd installation pins. The native toolchain image is reused unchanged.

| Local image | Podman manifest digest |
| --- | --- |
| `localhost/the-platform/deck:platform-prd01-20261003` | `sha256:1ab796095e06169568fbf996436ad439b9da855ba40fe6baaf38e83d718a7ba2` |
| `localhost/the-platform/marina:platform-prd01-20261003` | `sha256:fe1bce7bda372074d45daef7c663b64f5e270d557c17b043733bbebcd6c2ba79` |
| `localhost/the-platform/marina-conversation-byo:platform-prd01-20261003` | `sha256:0e4174c17e48c80239d6630771e8c5d05cae9b10d7bc10f80fa86cb0590e9329` |
| `localhost/the-platform/marina-verifier:platform-prd01-20261003` | `sha256:ef0f13e4e88eea7c5ed6f1fa3ad457b76ca6de746ad1e12cfc54f87b14aa8271` |
| `localhost/the-platform/marina-codex:platform-ver01-20261003-final` | `sha256:ffef74008564a7356cae14951dd8cae570198526c377a00632b8f013c140e150` |

## Remaining acceptance

Read-only inspection of `kind-marina-local` found Deck and Marina ready, retained
PVCs bound, and Marina still running with `--intake-enabled=false`. Its existing
repository file scope allows `src` and `tests`, so PRD publication would need an
explicit `docs/prd/` scope update before an installed acceptance run. That scope,
the running images, Harnesses, native snapshots and intake were left unchanged.

LAND-02, LAND-03, VER-01, TEST-01 and PRD-01 remain **Verify**. Pending work is
matching installation and native snapshot preparation, then live local Kind
proof of prerequisite release, successful rebase with fresh review/checks/CI,
conflict blocking and approved PRD publication. These publication cases require
acceptance-repository mutations excluded by the operator's current instruction.
TEST-01 also needs an authorized source publication and remote CI run. Local
source conformance and cached images do not replace those acceptance results.
