# First-release model configuration and provider probe — 2026-09-17

The later operator-requested [GLM trial](glm-model-trial-2026-09-17.md) supersedes
the model configuration below. This report preserves the original Luna evidence.

This follows the [operations report](first-release-operations-2026-09-17.md).
The operator authorized a **€5 total acceptance-test budget**, reported a cap
configured in OpenRouter, and delegated selection of an inexpensive, effective
model. The repository-scoped GitHub credential remains undecided. Only the local
Podman/Kind installation is available; there is no independent Kubernetes host.

## Model and budget

Selected **`openai/gpt-5.6-luna`**, catalog canonical slug
`openai/gpt-5.6-luna-20260709`. The live OpenRouter catalog lists $0.20 per million
input tokens and $1.20 per million output tokens, with function calling and
reasoning supported. This is an inexpensive candidate for the small clamp
regression; provider protocol success does not establish coding quality or
compatibility with the pinned Codex harness.

Sources checked on 2026-09-17:

- [OpenAI model documentation](https://developers.openai.com/api/docs/models/gpt-5.6-luna)
- [OpenRouter model and pricing](https://openrouter.ai/openai/gpt-5.6-luna-20260709)
- [OpenRouter live model catalog](https://openrouter.ai/api/v1/models)
- [OpenRouter Responses contract](https://openrouter.ai/docs/api/api-reference/responses/create-responses)

At the end of this initial Luna exercise, the private `workflow.json` and the
cluster's `marina-workflow` ConfigMap agreed on:

| Setting | Value |
| --- | --- |
| Model | `openai/gpt-5.6-luna` |
| Endpoint base | `https://openrouter.ai/api/v1` |
| Output cap per request | 4,096 tokens |
| Input price | 200,000 micro-USD per million tokens |
| Output price | 1,200,000 micro-USD per million tokens |
| Accounting currency | USD, matching published provider prices |
| Workflow SHA-256 | `795b5f574cce1ac0b03c592e0769f82e0b672a4e316b8acbfa95cc05c711451a` |

The original workflow is backed up privately beside the updated file. The existing
private OpenRouter key was synchronized into only the cluster Secret's
`model-token` entry; verification confirmed the remaining Secret entries were
unchanged. No new credential was created. No identity, certificate, image,
persistent volume, egress rule or intake setting was changed.
`MARINA_CONFIG_FILE` remains unset, service-account token mounting
remains disabled, and the running controller retains `--intake-enabled=false`.
The ConfigMap is prepared configuration, not an enabled repository workflow.

OpenRouter's authenticated key inspection returned a **5 USD limit** with no
reset interval. Preserve that existing limit; the operator's authorization is
still €5 total, not €5 per task. Do not label USD token rates or provider costs
as EUR. Future acceptance tasks must use explicit nonzero USD cost caps and the
same accounting currency as the model configuration. The current gateway uses
conservative admission accounting, not the provider's exact invoice calculation.

## Live provider protocol result

At **12:46:12–12:46:15 UTC**, two direct HTTPS requests from the Mac to the exact
configured `/responses` endpoint passed. They used the existing private key,
`store: false`, streaming, low reasoning effort, and a 512-token output ceiling
per request. The probe had no retries, alternative endpoint or alternative model.

1. A forced local `acceptance_probe` function call arrived as streamed argument
   events and completed with a usage record.
2. A self-contained follow-up supplied the local tool result. Streamed text
   returned exactly the expected `marina-luna-ok` marker and a second usage record.

| Response | Input tokens | Output tokens | Reported cost (USD) |
| --- | ---: | ---: | ---: |
| `gen-1789649172-aAsVOL9Sc74s0zphCPzT` | 130 | 15 | 0.0000440 |
| `gen-1789649173-LYK2HltEHEHhgWHTRWZJ` | 162 | 34 | 0.0000732 |
| Total | 292 | 49 | **0.0001172** |

Both responses identified `openai/gpt-5.6-luna`. The immediate account usage
counter still reported zero after the requests; costs above come from the
individual response usage records, not that delayed aggregate counter.

This is **partial RUN-09 preparation**. It did not run Codex, use Marina's model
gateway, persist model usage in Marina, launch a worker, repair the fixture,
publish a commit/PR, or exercise cancellation/recovery. RUN-09 remains Verify.

## Installation and fixture checks

- Source HEAD during the checks: `fc1125918bd871f5aef6a41511c50dfef5113adf`.
  Existing context/vision document edits were preserved.
- Both services were ready in `kind-marina-local`; both original PVCs were
  bound. The worker namespace contained no Jobs or Pods.
- The Podman node still publishes Deck only on `127.0.0.1:8443`.
- Read-only GitHub inspection confirmed private, unarchived
  `jmlero/marina-acceptance`, default branch `main`, baseline
  `10a96c6221f5c8a128ccb2617d8be27213d7b296`. Its intentionally failing
  [regression CI](https://github.com/jmlero/marina-acceptance/actions/runs/35211512509)
  remains unchanged. No developer credential was copied into the installation.
- `python3 -m unittest discover -s scripts -p test_acceptance_fixture.py -v`
  passed: the local fixture detects its defect and accepts the known repair.
- `python3 -m unittest discover -s scripts -p test_check_installation.py -v`
  passed: configuration blockers remain redacted.
- After the model update, installation preflight reports exactly one blocker:
  **`RUN-03: git-token is absent or empty`**. No placeholder or model-rate blocker
  remains. A clean configuration preflight will still not prove credential scope,
  harness compatibility, an enabled workflow, or live acceptance.
- A fresh authenticated Deck probe was not completed: its sandboxed localhost
  connection failed and the retry was interrupted. Earlier TLS/session/restart
  evidence remains historical; no new browser or login result is claimed here.

## Remaining acceptance work

| Item | Current result | Remaining condition |
| --- | --- | --- |
| RUN-03 | Open | Configure the repository-scoped GitHub credential. Repository, fixture, allowed paths, offline checks and required CI are prepared. |
| RUN-05 | Open | Model, pricing, bounded output and pinned images are prepared; scoped Git credentials and final executable-overlay validation remain. Intake stays disabled. |
| RUN-08 | Open | A trusted Deck URL and actual second-device sign-in remain absent. Current access is Mac loopback only. |
| RUN-09 | Verify; provider probe passed | Run the pinned Codex worker through Marina's gateway with the fixture; observe streamed tools, output collection and durable accounting. |
| RUN-10 | Verify; host unavailable | Complete the PR/verification/CI/evidence workflow on a host independent of the disconnected Mac. |
| RUN-11 | Verify | Restart each service during implementation and CI waiting; preserve exact run, Job, branch, commit and PR identity. No active run exists yet. |
| RUN-12 | Verify | Cancel a real Marina-managed worker through Deck; observe authority revocation, termination before acknowledgement, cleanup and retained evidence. |
| RUN-13 | Verify | Exercise failed local checks, failed CI and missing required CI through live tasks. Fixture CI failure alone does not pass. |
| RUN-14 | Verify | Advance a disposable PR head after verification and observe stale-evidence rejection. No live PR exists yet. |
| RUN-15 | Verify | Interrupt Deck, model and Git paths during actual work, restore their configuration and inspect bounded recovery. |
| RUN-16 | Verify | Exhaust task time, token, attempt and nonzero cost caps, including an interrupted model response with retained reservation. The provider key cap and protocol probe do not establish this. |
| RUN-17 | Verify | Fail a Marina-managed active worker and demonstrate cleanup plus a separately authorized follow-up. |
| RUN-18 | Partial report; open | This report adds model/configuration/provider evidence. Live task/run/Job IDs, PR head, artifact digests and the remaining acceptance results are still missing. |

No remaining RUN item is marked Done by this report. The next executable step
requires the scoped Git credential; the independent-host proof additionally
requires infrastructure beyond the Mac. No merge or production deployment was
performed.
