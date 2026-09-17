# GLM-5.3 Flash trial — 2026-09-17

The operator requested a trial of `z-ai/glm-5.3-flash` after the
[initial Luna configuration and probe](first-release-model-2026-09-17.md).
The existing €5 total authorization and OpenRouter key's $5 limit remain in
place. This trial adds **$0.0003630** in response-reported inference costs;
all six model requests across both reports total **$0.0004802**.

## Result and current configuration

GLM passed the same streaming/function-call round trip used for Luna. Both models
then independently returned the same correct one-line repair for the clamp
fixture. Each repair passed all seven unchanged regression tests in a separate
isolated container. GLM's coding request was **45.6% cheaper**, but took **16.017
seconds versus 2.405 seconds** for Luna. These are single observations on a tiny
fixture, not a broad coding-quality or latency benchmark.

GLM is now the model prepared for the next acceptance run. The private workflow
and cluster `marina-workflow` ConfigMap were updated and their digests compared.
The prior Luna workflow is retained in a private backup alongside the active
file. Credentials, worker/control images, volumes, network policy and intake
settings were preserved.

| Setting | Value |
| --- | --- |
| Model | `z-ai/glm-5.3-flash` |
| Catalog canonical slug | `z-ai/glm-5.3-flash-20260826` |
| Endpoint base | `https://openrouter.ai/api/v1` |
| Maximum output per workflow request | 4,096 tokens |
| Budget accounting currency | USD |
| Conservative input reservation rate | 450,000 micro-USD per million tokens |
| Conservative output reservation rate | 1,500,000 micro-USD per million tokens |
| Private and cluster workflow SHA-256 | `77b46b3c4ea67a908e9d188eb7c8d1a56013409b0e67d6b8cda1a391add3fb6a` |

OpenRouter's catalog price was $0.09/$0.30 per million input/output tokens, but
provider-specific prices vary. The observed coding request billed at
$0.15/$0.50. Because this installation does not pin an upstream host, the budget
reservation rates use the highest current listed endpoint rates ($0.45/$1.50),
rather than assuming the catalog's inexpensive route will always serve a request.
These conservative estimates are distinct from actual billed usage. Recheck the
endpoint price snapshot before enabling repository execution; it is not a
guarantee about future pricing.

Sources checked on 2026-09-17:

- [OpenRouter GLM model information](https://openrouter.ai/z-ai/glm-5.3-flash)
- [Live model catalog](https://openrouter.ai/api/v1/models)
- [Provider-specific endpoints and pricing](https://openrouter.ai/api/v1/models/z-ai/glm-5.3-flash/endpoints)

## Streaming and function-call round trip

At **13:08:31–13:08:38 UTC**, the same prompt and local function schema as the
Luna probe were sent to GLM through the explicit `/responses` endpoint. Both
requests used streaming, `store: false`, low reasoning effort and a 512-token
output limit. The first returned the requested local function call. A
self-contained follow-up supplied the function result and returned the exact
expected marker. Both responses named `z-ai/glm-5.3-flash` and supplied usage.
There was no client retry, alternative model or alternative endpoint.

| Response | Input tokens | Output tokens | Reported cost (USD) | Request time |
| --- | ---: | ---: | ---: | ---: |
| `gen-1789650511-e3e1c5IK79AShfOfanKs` | 165 | 7 | 0.00002825 | 5.014 s |
| `gen-1789650516-9DDtLkdvYC7hL96v8sML` | 44 | 49 | 0.00002155 | 1.483 s |
| Total | 209 | 56 | **0.00004980** | |

The initial Luna round trip cost $0.0001172, so GLM was 57.5% cheaper on that
particular exchange. Different upstream hosts served different effective rates;
this is a measured request-cost comparison, not a fixed-price promise.

## Fixture comparison

At **13:10:28–13:10:46 UTC**, each model received the same source file and trusted
test file. Each was asked to return only a replacement `src/clamp.cjs` through
the same `submit_fix` function schema. Both used streaming, `store: false`, low
reasoning effort and a 2,048-token output limit, with one request per model.
There was no repair retry, model substitution or automatic source execution.

Both proposed exactly this change, which was inspected before testing:

```diff
-  return Math.min(Math.max(value, minimum), maximum - 1);
+  return Math.min(Math.max(value, minimum), maximum);
```

| Model | Response ID | Input / output tokens | Reported cost (USD) | Request time | Tests |
| --- | --- | ---: | ---: | ---: | --- |
| GLM-5.3 Flash | `gen-1789650628-9na7egQpWWSwcqWhqXLr` | 496 / 72 | **0.0001104** | 16.017 s | 7 passed |
| GPT-5.6 Luna | `gen-1789650644-BQOGE3iiv86BARati82e` | 462 / 92 | **0.0002028** | 2.405 s | 7 passed |

The unchanged baseline separately produced three passes and four expected
failures. Each variant ran `node --test tests/clamp.test.cjs` in a fresh
Podman container using the locally installed `node:24-bookworm-slim` image ID
`5c24c5d1bf4c19805b692346798c62097f27d1f60e2a9e24adc9368ea96cda4e`.
Containers used UID/GID 65532, no network, no host mounts or credentials, a
read-only root, no added capabilities, no-new-privileges, bounded CPU/memory/PIDs
and a 16 MiB temporary work directory. Fixture files arrived over stdin; the
containers were removed after execution. No dependency download was needed.

SHA-256 evidence:

| Subject | Digest |
| --- | --- |
| Original source | `a25f788cf53e173601b73e0ca4473209783d02332eba122bb014280a7d0a9a6f` |
| GLM and Luna repaired source | `5332bdf725944e1316df44453a49e8de1efc106e3df7737b3b8be6e7df21a154` |
| Unchanged regression tests | `c4a546f6cb0177bb69d79f57202843f15a67acdccffac09e6bbf26f8f29c3061` |
| Baseline test output | `87f9cbf19ea9399f17a6a77a5341990f0cff017eef617a7be1c58827e4587397` |
| GLM test output | `efa80388661abf89a96ce83eeb7fdfda57a19b9b42470dcd336279e66980a942` |
| Luna test output | `c69ada6a83c291836299a321faf57763472ae4a278f9b3b25f0e36a0ede265c0` |

The first summary parser expected TAP output; the installed Node image emitted
its spec reporter instead. The saved test output was re-parsed and the exact
7/3/4 baseline and 7/7/0 repaired totals asserted. No extra inference or repeated
test execution was needed to correct that reporting mismatch.

## Acceptance boundary

This tested direct provider calls and isolated deterministic verification of
their proposed source. It did **not** exercise the pinned Codex CLI, Marina's
gateway or durable accounting, Kubernetes worker lifecycle, Git publication, CI
observation or laptop-off operation. The verification image above is separate
from the acceptance worker image. **RUN-09 remains Verify**, and no other RUN
item is marked complete by this trial.

The current installation preflight still reports exactly one blocker:
`RUN-03: git-token is absent or empty`. The operator has not selected that
credential. `MARINA_CONFIG_FILE` remains unset, service-account token mounting
remains false, and `--intake-enabled=false` remains in effect. Only local
Podman/Kind is available, so the independent-host and second-device conditions
in the [per-task report](first-release-model-2026-09-17.md#remaining-acceptance-work)
also remain unresolved. The disposable GitHub repository and the committed
fixture were not changed.
