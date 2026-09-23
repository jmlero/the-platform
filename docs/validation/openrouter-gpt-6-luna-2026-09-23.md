# OpenRouter GPT-6 Luna default — 2026-09-23

Environment: local `kind-marina-local` on `podman-machine-default-root`.

The operator selected `openai/gpt-6-luna` as the default model for newly
prepared workflows and the existing local development installation. The
configuration retains the OpenRouter `https://openrouter.ai/api/v1` endpoint and
private model credential reference. The configured USD rates are 100000 input
and 500000 output microdollars per million tokens, matching OpenRouter's
published USD 0.10/M and USD 0.50/M prices at the time of this change.

Verification:

- `make check-installation`: 18 tests passed.
- Private installation preflight: `configuration_ready: true`, no blockers;
  intake unchanged.
- One direct authenticated OpenRouter Responses request with `store: false` and
  `max_output_tokens: 64` returned HTTP 200, `completed`, model
  `openai/gpt-6-luna`, 10 input and 17 output tokens. Reported cost was
  USD 0.0000095. No token value or response content was recorded.
- Before the local config change, Marina had no active executions or worker
  Jobs. A later active planning execution was allowed to reach a terminal state
  before the local ConfigMap was patched and Marina restarted.
- The restarted Marina deployment was Ready (1/1) with
  `--intake-enabled=false`. Its mounted `workload-profile` reported model
  `openai/gpt-6-luna` at the OpenRouter endpoint, profile revision 4. The live
  repository-engine settings were preserved in the private workflow file.

The direct request and mounted-profile inspection establish the selected model
and basic Responses compatibility. A complete repository and diagnostic worker
acceptance run under this model was not performed; the earlier
`z-ai/glm-5.3-flash` acceptance does not transfer to this model.
