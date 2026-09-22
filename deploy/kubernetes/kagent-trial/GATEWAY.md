# Isolated KINT-04 gateway acceptance

This recipe extends the [locked trial](README.md); install it and obey its
ownership, preparation-egress and teardown rules first. Use only
`kind-marina-local` on `podman-machine-default-root`. Do not enable Marina intake.
The [acceptance report](../../../docs/validation/kint-04-2026-09-22.md) records
observed results. This fixture does not select kagent for an installed workload.

## Gateway fixture

`marina/internal/workerapi/substrate_live_test.go` contains the opt-in
`TestLiveSubstrateGateway`. Build it from the Marina module:

```sh
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go test -c \
  -o /private/tmp/kint04/gateway.test ./internal/workerapi
```

Package that binary in the cached Alpine 3.22 image; do not add model credentials
or network downloads. Run `/gateway.test -test.run '^TestLiveSubstrateGateway$'
-test.v -test.timeout 60m` in a disposable `kint04-gateway` Pod in `kint-engine`,
with `KINT04_SUBSTRATE_FIXTURE=1`, UID/GID 10001, read-only root, no capabilities,
no privilege escalation, no service-account token and an empty `/tmp` volume.
Mount a newly generated test CA's leaf/key read-only at `/fixture/tls.crt` and
`/fixture/tls.key`, readable by its group. The leaf SAN must include
`kint04-gateway.kint-engine.svc`. Keep private keys and generated files outside Git.

The fixture listens on TLS port 8443. A Service named `kint04-gateway` must expose
**443**, targeting 8443. Allow ingress only from the `atenet-egress` Pods in
`ate-system`; deny all fixture Pod egress. Its synthetic TLS model listens only on
loopback. Its private HTTP control listener binds only `127.0.0.1:8080`; do not
expose it in the Service or NetworkPolicy. Reach it only by authenticated
`kubectl --context kind-marina-local -n kint-engine port-forward --address
127.0.0.1 pod/kint04-gateway 18084:8080`.

Mount the public test CA into the trial's egress router and set the dynamic HTTPS
backend's `backendTLS.root` to that PEM file. Restart only the trial router and
wait for readiness. Keep the existing TLS checks and actor MITM trust projection.
Neither insecure TLS nor a credential inside the actor is needed. A destination
on port 8443 failed at the pinned router's outer TLS listener; port 443 passed.

The trial values now grant the credential provider exactly `atespace:
kint-engine`, `allowedNamespaces: [kint-engine]`. Both this policy and the scoped
Kubernetes Secret RBAC are required. The former empty policy denied every
injection request even though the actor and template were Ready.

## Prepare and bind an attempt

Use the locked native Harness and a **fresh** AgentTemplate name for every case.
Use a same-name Secret with an initially random nonfunctional `gateway` key and
a dedicated ModelConfig with `provider: OpenAI`, `model: gpt-5`,
`openAI.apiFormat: responses`, and
`openAI.baseUrl: https://kint04-gateway.kint-engine.svc/engine/model/v1`.
Do not configure memory, extra tools, delegation or provider passthrough.

Wait for the exact prepared revision. The control API is test-only:

| Method/path | Input / result |
| --- | --- |
| `POST /init` | JSON `Template`, `Revision`, `ExpirySeconds`; returns attempt ID, expiry and **secret** `credential`. Write the credential directly into that attempt's Secret using a private pipe; never print it. |
| `POST /bind` | After real kagent creation, JSON `Instance`, `Context`, `Route`, `Revision` records the observed exact instance, A2A context, authority and prepared revision. |
| `POST /command` | Plain shell body to return as the next synthetic model's `exec_command` call. Use only the bounded [probes](gateway-probes.sh). |
| `GET /stats` | Redacted tool output, HTTP statuses, synthetic upstream call count and durable usage totals. |
| `POST /rotate` | Rotate the fixture's upstream credential file; the next actual native call must succeed. |
| `POST /cancel` | Persist cancellation intent without deleting the actor or changing its Secret. |
| `POST /finish` | End the fixture, asserting positive model calls, exactly three fixture tokens per call and no pending reservations. |

Create the real instance through the loopback kagent port-forward, with a fresh
request ID and the chosen native Harness/template. Bind its returned IDs before
invocation. Close `kint-preparation-downloads` before making any model call.
The fixture uses real gateway admission and SQLite accounting with synthetic Deck
and context-authority responses. It does not prove Deck claim/lease integration,
workflow result admission, real source checkout or a paid model.

## Cases and evidence

1. Select `gateway-probes.sh`, invoke the real instance and require completed
   native execution, `probe_complete`, exit code zero and two model calls/six
   fixture tokens. Inspect gateway 404s for unsupported routes and router 403s
   for direct destinations; a failed client alone is not enough evidence.
2. Call `/rotate`, invoke again and require success with three additional tokens.
3. Test the injected Secret cache separately: replacing its value with an invalid
   fixture value can leave the original cached value usable. Restore the valid
   value through a private write. For cache refresh acceptance, replace it with
   an invalid value, restart only the trial egress router, invoke and require
   denial with unchanged upstream usage. Restore the original scoped credential,
   refresh that cache again and require the **same instance** to succeed.
4. Call `/cancel` while its original Secret stays installed. Invoke the same
   instance and require gateway 403s on every retry, zero new upstream calls and
   unchanged accounting. This tests revocation independently of cached Secrets
   and termination. Then delete that instance through the real lifecycle API.
5. Use a separate fresh attempt with `ExpirySeconds: 120`. Invoke successfully,
   leave its Secret/actor unchanged, wait past the returned expiry, and require
   every native retry to receive 403 without changing the upstream totals.
6. Capture redacted instance/revision IDs, statuses and counters. Finish the
   fixture; default Go checks skip it, so record its live result separately.

Both expired/cancelled native tasks truthfully fail; they must not become
successful workflow results. Unknown usage, active-stream interruption, context
revocation and source binding also retain the deterministic gateway tests.
Full Deck/Marina/kagent workload failure acceptance remains KINT-09.

Delete the synthetic instances before the trial WorkerPool/namespaces. Follow the
trial teardown to remove its cluster resources, generated trust bundles and
restore the exact API-server manifest. Close only task-owned port-forwards.
Verify original Deck/Marina Deployment and PVC UIDs/specifications, disabled
intake and HTTPS 200 at `https://localhost:8443`.
