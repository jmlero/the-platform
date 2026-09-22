# Candidate projects for a platform value pilot

- Reviewed: 2026-09-22
- Status: Context Lens selected by the operator on 2026-09-22; pilot preparation started
- Scope: 38 repositories visible through the operator's GitHub account, including
  two forks; deeper source/configuration review of the shortlist below

## Recommendation

The operator selected **Context Lens** for the first value pilot and confirmed
that **ubahndoku is completed**. Exclude ubahndoku from unfinished-project
recommendations; its stale checklist is historical documentation, not a request
to reopen the project. Prepare a small Context Lens increment to export an
explicitly selected, privacy-safe session summary and compare saved summaries.
**kipf** is the fallback when the immediate goal is the simplest possible useful
code-fix run rather than a richer product increment.

Starting another repository would add product definition and setup work without
improving this first proof. Existing projects already provide real constraints,
code to preserve, and useful follow-up work. None of this establishes product
market demand: personal utility and platform suitability are the selection
criteria, and sustained use must still be observed.

## What was reviewed and verified

Read current default-branch identities, root files, available READMEs, issue/PR
metadata, then inspect selected backlogs, source, tests, manifests and GitHub
Actions. Twelve repositories received recursive tree and targeted documentation
review. Counts of test-related paths are only discovery aids, not coverage
measurements. README claims and unchecked boxes are not treated as runtime proof.

For Context Lens, the clean local checkout matched GitHub commit
`5c7868925d255cb49d6b4b6e457adac0249975cf`. An archive of that commit was tested
in a temporary directory using the existing installed dependencies:

- `npm test`: **13 passed**, including parser accounting, partial writes,
  truncation/replacement, discovery, access restrictions and live local SSE.
- `npm run check`: passed.
- `npm run lint`: passed.
- `npm run build`: passed; Vite reported a JavaScript chunk above 500 kB and
  Node emitted a deprecation warning. Neither failed the build.

The build initially hit the filesystem sandbox because Vite writes temporary
configuration beside the reused dependencies; the approved rerun succeeded.
This was not a clean dependency reinstall, browser acceptance, or a Marina run.
No private session logs were needed: tests use synthetic inputs. Other candidate
suites were not executed; their CI results below are GitHub observations at the
recorded commits. The assessment did not modify candidate source or submit work.
After selection, [pilot preparation](validation/context-lens-pilot-preparation-2026-09-22.md)
built and tested the offline worker and saved an unauthorized Deck backlog draft.
No repository connection, production deployment, PR, or publication was initiated.

## Candidate comparison

The order balances a useful next outcome, inexpensive independent verification,
limited external dependencies, and fit with one-repository, sequential execution.
It is a judgment from repository evidence, not a numerical quality score.

| Candidate | Evidence and remaining work | Pilot judgment |
| --- | --- | --- |
| **Context Lens** | Existing parser/API/UI; three test files and 13 passing local tests. No workflow files in the inspected tree and no returned Actions runs. Browser QA is explicitly unverified in README. Current API/UI has live session inspection, but no dedicated portable report export or saved-report comparison. | **First choice.** Public, useful to the platform's operator, fixture-testable, no application model call/database/cloud deployment required. Establish CI before the feature pilot. |
| **ubahndoku** | Current-head CI and deployment workflows succeeded. Substantial deterministic backend/frontend tests and written product constraints. Search feedback, archive and history code already exist although backlog items remain unchecked. | **Completed, per operator confirmation.** Excluded from pilot selection. Open boxes in the inspected document do not override that direction. |
| **kipf** | Nine tracked files, stdlib-only Go module, one test file covering three output parsers, no inspected CI workflow. The subprocess wrapper buffers output, does not attach stdin in its compact path, and drops process-start error details. | **Lowest-friction fallback.** Small fixes can prove compatibility with real subprocess fixtures. Too small by itself to prove backlog preparation or long-running product development. |
| **agent-suitup** | Dependency-free Node CLI, seven test files, explicit ownership and conflict rules, no inspected CI workflow. Current commands are already broad; no open issue/PR backlog was returned. | Strong technical fit, weaker immediately evidenced feature gap. Select a demonstrated lifecycle defect or user need; adding more instruction content alone would give weak value evidence. |
| **ingame-startup / Datacenter Tycoon** | Current-head CI passed; deterministic gameplay and substantial follow-up backlog. Source already has transaction helpers/PRNG persistence changes listed as unfinished in TODO. Python/PostgreSQL plus frontend. | Strong medium-term game pilot. A bounded save/replay improvement could show real progress, but first distinguish remaining defects from stale checklist entries and prepare PostgreSQL verification. |
| **app-meerkat-cli** | Current-head CI passed and command tests exist. Remaining v1 work is largely live backend validation, optional backend auth discovery, and publishing; stdin uploads are an explicit future feature. | Good bounded alternative for a fixture-tested stdin-upload increment. Finishing its release requires external backend/account and publication steps beyond the current platform PR boundary. |
| **app-meerkat** | Current-head push CI/CD succeeded; nine open issues and twenty open PRs at inspection. Real receipt/product workflow, substantial tests, and an explicit product-identity/data-quality blocker. | Valuable later workload, but wider integration surface and overlapping PRs increase context/review effort. Prefer an isolated deterministic API or normalization fix over extraction-quality claims or auth migration. |
| **ingame-hex-strategy** | Current-head CI passed; original MVP checklist explicitly marked complete. Pure rules plus a Go multiplayer server. | Technically suitable, but needs a new agreed product outcome rather than an assumption that old MVP work is missing. |
| **ingame-toy-battlefield** | Prototype checklist marked complete. Latest inspected CI: quality job passed, browser job failed at `npm run test:e2e`. | Useful bounded repair candidate after reproducing the browser failure. Failed CI alone does not identify the defect; game balance and visual quality still need human playtesting. |
| **app-findash** | Core application largely checked off; remaining work centers on Cognito/AWS provisioning, migration, session/security acceptance and operator inputs. Current-head CI was cancelled, not green. | Defer as a first pilot. Deployment/identity integration and correctness of portfolio data create a broader acceptance burden. Synthetic calculation fixes could be scoped separately. |
| **app-aidr** | Local MVP code checked off; remaining explicit acceptance needs real model responses and a judgment that rewrites are useful. Workflow uses shared self-hosted infrastructure. | Small application, but model output quality and deployment setup obscure the platform's first engineering proof. Deterministic API tests cannot prove rewrite usefulness. |
| **ptncli** | Detailed Proton integration plan, 23 tracked files, no discovered test files, minimal README. Current Go dependencies include Cobra, while much of the document describes future integration. | Genuinely early-stage, but authentication, encrypted calendar/mail data and API integration make it a poor first bounded pilot. Start with offline contracts if revisited. |

## Findings that change the choice

### Historical backlog discrepancies must not create duplicate work

The operator subsequently confirmed ubahndoku is completed; do not reopen it.
The inspection found a useful historical discrepancy: at ubahndoku commit `9e2d26a52209c4dcc6bd2976177afabc2b9e6938`,
`StationInput.tsx` already distinguishes loading, empty and failed searches,
provides retry, and ignores aborted requests. Its tests cover those behaviors.
`SEARCH-001` still describes them as missing. Archive handlers, integration tests,
return pages and history helpers also exist. Their presence does not prove every
ARCH-001/STAT-001 acceptance criterion; it does rule out delegating "implement
archives/history" without an audit of what remains.

At Datacenter Tycoon commit `f5485aa63fc2024bcb8a0b13bce1140be733e1a6`,
`process_turn` uses `flush`, and the route invokes `commit_game_action` after
`game_random`. TODO still asks to remove the inner commit and persist PRNG state
atomically. Transaction failure and concurrent-request behavior need tests before
calling that item complete, but the original missing-code description is stale.

This is a concrete input for [work preparation](work-preparation.md): classify a
candidate as missing behavior, implemented but awaiting evidence, stale/obsolete,
or blocked on a product/operational decision. A planner should cite the source
and tests behind that classification. Avoid generating duplicate features or
counting documentation reconciliation as a newly delivered product feature.

### Context Lens is a workload, not assumed platform telemetry

Its current inputs are local Codex JSONL logs. The current platform and planned
kagent integration do not automatically expose those logs in that format. Do
not add a platform telemetry integration to this pilot or infer platform billing
from Context Lens totals. Its existing documentation correctly distinguishes
context size, cumulative token processing, cache counts, and account billing.

The useful increment is a developer-controlled report over already-supported
inputs. Whether smaller context leads to better results must be assessed
separately using outcomes and review effort.

## Proposed first pilot: a portable Context Lens report

Parent outcome: **save a privacy-safe account of one session's context usage and
compare it with another, without exporting conversation content or requiring the
original log directory.** This is a proposed feature, not existing backlog work.

| Sequence | Focused work order | Selected context | Acceptance |
| --- | --- | --- | --- |
| Preparation | Establish trusted CI and a reproducible verification image for the pinned revision. | `package.json`, lockfile, existing tests, README, platform installation constraints. | Existing test/check/lint/build commands pass at the exact baseline; PR CI runs; runtime workers need no dependency downloads. Record setup separately from product work. |
| 1 | Define and implement one versioned report serializer and validator. | `shared/types.ts`, parser summary semantics, privacy/accounting tests, an explicit report field allowlist. | Fixed synthetic inputs produce exact expected counts. Exclude paths, raw IDs, prompts, reasoning, tool arguments/output, and free-form event/warning text by default. Unknown versions and malformed/nonfinite/out-of-bounds values fail clearly. Preserve limitations using fixed diagnostic codes. |
| 2 | Add an explicit "Export summary" action for the selected snapshot. | Accepted report contract, dashboard selection/pause behavior, existing formatting and API tests. | Export corresponds to the displayed snapshot, including when paused; no background upload; empty/error cases are clear; keyboard activation works. Downloaded bytes pass independent report validation. |
| 3 | Compare two explicitly selected reports locally. | Versioned report contract, existing charts/types, fixtures with differing models/windows and legacy counters. | Show context peak/latest, input/output processing, cache and compaction differences accurately. Keep denominators and incompatible/partial-data limitations visible; do not sum possibly inherited sessions or claim causation/billing savings. Invalid files fail without crashing or replacing the current valid view. |

Keep source parsing semantics unchanged unless a separately reproduced defect
requires a fix. No agents execute reported commands, no remote uploads, no new
backend storage, and no new log-format support in this increment. Report fixture
validation and privacy checks belong to the trusted verification baseline before
the implementation attempt; worker-written tests alone are insufficient evidence.
The browser flows also need independent human review of the resulting PR/build.

This gives three small, linked PR-sized outcomes. Under today's release, manually
prepare each task in Deck, freeze explicit context, and run one task at a time.
Human review and merge remain required before the next task consumes the changed
base. Do not claim PREP decomposition or automatic dependent release until its
implementation and acceptance are complete. If a new automated browser harness
is needed, prepare it as a separate verification prerequisite rather than grant
workers browser/system authority implicitly.

## What would prove value

Record evidence for each pilot task, not just screenshots of a working board:

- Original request, acceptance criteria, exact selected sources, pinned base,
  configured limits and final source-bound verification.
- Whether the result needed duplicate explanation, manual code repair, a
  follow-up task, or a blocker; failed attempts count in the pilot outcome.
- Whether the operator accepted the change and actually uses the report/comparison.
- Whether disconnecting or a normal service restart preserved task identity,
  context and results without duplicate work.

Success means this useful increment reaches reviewable, independently verified
results with less repeated coordination while preserving correctness and privacy.
Three green PRs alone establish neither user value nor fully autonomous delivery.
Optionally compare review/intervention time with similar manual work, but do not
reopen the operator-declined VALUE-01 billing/effort study or impose new measurement
requirements. A simple qualitative task record is enough to begin.

## Other repositories and a new-project option

- `ingame-f1` is an existing authored game with optional model narration; open
  issues request AI competition and monetization. Those need a chosen product
  slice rather than blanket completion of the game.
- `app-comeflywithme` has a simple travel app, an auth issue and a self-hosted CI
  dependency. A small local feature is possible, but the shortlist has stronger
  verification evidence.
- `loopbot`, `panza` and `platform-marina` are agent/runtime projects. Developing
  another engine while The Platform adopts kagent risks making the pilot about
  platform construction instead of a useful external outcome.
- `the-platform` remains the system being evaluated; `marina-acceptance` remains
  its deliberately defective fixture. They prove controls, not independent
  product value.
- `infra-gcp`, `infra-aws`, `dotfiles`, `test-renovate` and `operator-kafka` involve
  environment maintenance or infrastructure. They are not the initial product
  pilot; production and spending gates still apply.
- `jmlero`, `jmlero.github.io`, `cv`, `claude-toolkit`, `qliz`,
  `100daysofcloudnative`, `CKAD-preparation`, `ai-engineering-workshop-python`,
  `sandbox-go-restapi`, `o11y-otel-demo`, `beegfs-web-overview` and
  `python-compressandmove` received portfolio-level screening, not a code audit.
  Their profile/content/tutorial/older utility focus gives less evidence for a
  stronger immediate pilot than the shortlist.
- `walgit` and `pleng` are forks; upstream contribution constraints would add
  another variable to the first proof.

A new project would make sense if none of the proposed outcomes is useful to the
operator. A small offline deterministic app with fixtures and no accounts would
be the right shape, but Context Lens already offers that low-dependency starting
point plus real code and semantics to preserve. Prefer continuing it.

## Source anchors

GitHub links below identify inspected revisions or actual workflow observations;
private repositories require the operator's access. These sources are snapshots,
not claims about live production health.

- Context Lens: [README](https://github.com/jmlero/context-lens/blob/5c7868925d255cb49d6b4b6e457adac0249975cf/README.md), [scripts](https://github.com/jmlero/context-lens/blob/5c7868925d255cb49d6b4b6e457adac0249975cf/package.json), [API](https://github.com/jmlero/context-lens/blob/5c7868925d255cb49d6b4b6e457adac0249975cf/server/api.ts), [data model](https://github.com/jmlero/context-lens/blob/5c7868925d255cb49d6b4b6e457adac0249975cf/shared/types.ts).
- ubahndoku: [backlog](https://github.com/jmlero/ubahndoku/blob/9e2d26a52209c4dcc6bd2976177afabc2b9e6938/TODO.md), [search tests](https://github.com/jmlero/ubahndoku/blob/9e2d26a52209c4dcc6bd2976177afabc2b9e6938/frontend/src/StationInput.test.tsx), [CI](https://github.com/jmlero/ubahndoku/actions/runs/34327192842).
- kipf: [subprocess implementation](https://github.com/jmlero/kipf/blob/37a389cb8fe8c29e02fff966f2d6131634db6ca9/run.go), [tests](https://github.com/jmlero/kipf/blob/37a389cb8fe8c29e02fff966f2d6131634db6ca9/main_test.go).
- agent-suitup: [vision](https://github.com/jmlero/agent-suitup/blob/eb2b01c90ef224540a8a7f68070f6ae97d66fad9/VISION.md).
- Datacenter Tycoon: [backlog](https://github.com/jmlero/ingame-startup/blob/f5485aa63fc2024bcb8a0b13bce1140be733e1a6/TODO.md), [turn route](https://github.com/jmlero/ingame-startup/blob/f5485aa63fc2024bcb8a0b13bce1140be733e1a6/backend/app/routers/game.py), [CI](https://github.com/jmlero/ingame-startup/actions/runs/31910438936).
- Meerkat CLI: [remaining work](https://github.com/jmlero/app-meerkat-cli/blob/f6e3851c5ec21be4efe9d01052a5843998108755/TODO.md), [CI](https://github.com/jmlero/app-meerkat-cli/actions/runs/31910365920).
- Meerkat: [product boundary](https://github.com/jmlero/app-meerkat/blob/01168b317c60f7cd66f6e1526270f22403d37b1c/docs/PROJECT.md), [data-quality issue](https://github.com/jmlero/app-meerkat/issues/76), [push CI/CD](https://github.com/jmlero/app-meerkat/actions/runs/35523382487).
- Games: [Waterloo CI](https://github.com/jmlero/ingame-hex-strategy/actions/runs/31930312122), [Toy Battlefield failed browser CI](https://github.com/jmlero/ingame-toy-battlefield/actions/runs/31929439904).
- FinDash: [backlog](https://github.com/jmlero/app-findash/blob/1beb68c1b6a03b9c6385224c9393b693093e7c44/TODO.md), [cancelled current-head CI](https://github.com/jmlero/app-findash/actions/runs/32153196461).
- Other shortlisted constraints: [app-aidr checklist](https://github.com/jmlero/app-aidr/blob/9b491ebe0d48241489ef3845adb09c31fb14f262/TODO.md), [ptncli module](https://github.com/jmlero/ptncli/blob/7d0b6264a5863582ab4984c2fcff364a5219bd0b/go.mod).
