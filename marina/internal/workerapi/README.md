# Governed worker and engine gateway

`workerapi.Server` owns admission and bounded model/source/context access for
both existing workers and the KINT-04 engine integration. Installed workflows
still launch the supported workers. Engine resource provisioning and workflow
selection remain pending; do not treat these routes as an enabled kagent profile.
See [KINT-04 acceptance evidence](../../../docs/validation/kint-04-2026-09-22.md).

## Engine contract

Pass the configured `*workerapi.Server` as the required `kagent.Admission` argument
to `kagent.New`. It rechecks the exact journaled specification, Deck run/fence,
leases, deadlines, current context and remaining budget before creation or initial
message delivery. A context validator and at least a 32-byte signing key are
required. Observation/cancellation must remain possible during authority outages.

The persisted specification must contain one `engine-gateway` credential
reference, `secret://<namespace>/<template>/gateway`, expiring within the attempt's
deadline. Template names are unique per attempt for the installed canonical
engine endpoint; the journal permanently prevents reuse, including after cleanup.
Keep each engine's endpoint identity stable, without interchangeable aliases.

`Server.EngineCredential(ctx, persistedSpecification)` returns the attempt
selector and specification HMAC for the injection Secret. Treat the return value
as a secret. It belongs only in a private Secret write, never in a specification,
context artifact, prompt, command argument, log or result. The gateway revalidates
live authority for every request, so deleting/rotating a cached Secret is not the
revocation mechanism.

| HTTPS route | Supported authority |
| --- | --- |
| `POST /engine/model/v1/responses` | Bounded self-contained Responses for implementation/analysis stages. |
| `GET /engine/input` | Exact persisted worker input. |
| `GET /engine/authority` | Current fence and bounded expiry. |
| `GET /engine/git/info/refs?service=git-upload-pack` | Advertisement for the original repository only. |
| `POST /engine/git/git-upload-pack` | Read-only Git transport for that repository. |
| `POST /engine/tools/read-context-source` | Existing frozen tool allowlist and selected immutable source URI. |

All routes require direct TLS and `Authorization: Bearer <engine credential>`.
TLS-forwarding headers are not trusted. There are no generic proxy, Git-write,
engine-control, memory, delegation or server-managed Responses history routes.
The existing artifact route requires its separate service credential.

Repository native requests have only two normalization rules: drop
`client_metadata`, and omit the pinned Codex tool declaration containing exactly
`type: web_search` and boolean `external_web_access`. Explicit remote search
selection remains denied. No remote search tool reaches the upstream. Other
provider-hosted tools, stored context/prompts, background requests and unknown
Responses fields fail closed. Analysis tools retain their frozen allowlist.

Each accepted model attempt/retry reserves tokens and cost in SQLite. Engine
cancellation and credential expiry are checked again in the reservation
transaction. Unknown usage remains reserved across restart. Existing live
connection checks run every two seconds; an already admitted upstream call can
remain uncertain and must not be counted as zero usage.

## Governed installation wiring

The isolated local installation proof passed. This is a manual preparation
contract; automatic profile provisioning and dispatch remain KINT-06/07 work. Follow the [locked isolated trial](../../../deploy/kubernetes/kagent-trial/README.md)
and its ownership/teardown rules.

1. Choose a fresh attempt-exclusive AgentTemplate name and same-name Secret in
   the engine namespace. Initially use a random nonfunctional Secret value under
   `gateway`. Use a dedicated ModelConfig referring to that Secret and key:
   `provider: OpenAI`, `openAI.apiFormat: responses`, and
   `openAI.baseUrl: https://<gateway DNS>/engine/model/v1`. Substrate requires an
   exact DNS hostname; do not use an IP endpoint or an upstream provider Secret.
   Use Service port 443 (it may target gateway container port 8443). The pinned
   router rejected the trial destination on 8443 at its outer TLS listener.
   Configure the credential provider with the exact atespace-to-namespace grant
   (`kint-engine` to `kint-engine` in the trial), in addition to scoped Secret RBAC.
2. Bind the exact installed native harness/template and inspect the prepared
   resources. Permit egress only to Marina/DNS. Do not configure memory,
   shared/delegated agents, remote model endpoints, extra MCP tools, skills,
   plugins, telemetry destinations or credentials. The diagnostic BYO worker can
   call the existing HTTP context tool; no MCP bridge is needed for that client.
   Preserve its no-repository authority.
3. Establish genuine TLS trust from Substrate's egress router to Marina and the
   generated MITM trust bundle inside the native client. The pinned native
   compiler rejects ModelConfig custom TLS fields and caller-token passthrough;
   neither is a substitute for installed trust. Do not disable verification.
4. Persist the runtime specification with the observed exact prepared revision,
   profile/context digests, limits and credential reference. Obtain the scoped
   credential only after the journal assigns the attempt ID. Secret **values** do
   not affect prepared revision identity. Replace only this attempt's Secret
   value before creating its instance. The adapter then rechecks current
   admission and the prepared revision before sending work.
5. Exercise injected HTTPS calls, cached-credential cancellation/expiry, rotation
   and denied bypasses. Source credentials, upstream model credentials and
   Kubernetes tokens must remain outside the actor. Only Marina publishes source
   or admits results. Record actual actor/runtime evidence, not just Ready Pods.
6. Revoke through durable attempt intent before termination. After observed
   termination, remove only the attempt-owned instance and prepared resources/
   Secret. Never reassign those names to another attempt. Full automated
   provisioning, workflow dispatch and resource cleanup belong to KINT-06/07;
   installation upgrade/rollback remains KINT-10.

## Verification

```sh
cd marina
go test ./internal/kagent ./internal/workerapi ./internal/runtime ./internal/journal/sqlite
go test -race ./internal/kagent ./internal/workerapi ./internal/runtime ./internal/journal/sqlite
```

The opt-in `TestNativeCodexGateway` uses the actual `codex-cli 0.148.0` from the
locked native image with the App Server initialization/thread/turn protocol.
It generates a CA and signed TLS certificate and uses a synthetic Responses
provider. Run it only in an isolated container; it starts the native client with
the pinned harness's `danger-full-access` configuration. It is not an injection
or live engine proof.

With that binary saved as `/private/tmp/kint04-native/codex` and no secrets in the
fixture directory, the local ARM64 reproduction is:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c \
  -o /private/tmp/kint04-native/gateway.test ./internal/workerapi
podman --connection podman-machine-default-root run --rm \
  --name kint04-native-gateway --network none --read-only --cap-drop ALL \
  --security-opt no-new-privileges --user 10001:10001 \
  --tmpfs /tmp:rw,nosuid,size=128m \
  --volume /private/tmp/kint04-native:/fixture:ro \
  --env KINT04_CODEX_BINARY=/fixture/codex docker.io/library/alpine:3.22 \
  /fixture/gateway.test -test.run '^TestNativeCodexGateway$' -test.v -test.timeout 55s
```

The regular suite skips this opt-in test. Its successful isolated run is recorded
separately from deterministic tests and from the Substrate acceptance described
in [the live fixture recipe](../../../deploy/kubernetes/kagent-trial/GATEWAY.md).
