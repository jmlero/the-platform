# Marina alone on a fresh local Kind cluster

- Date: 2026-10-20
- Environment: a new Kind cluster `kind-marina` (Kubernetes 1.37.0, one node)
  on its own Podman network `marina`, Podman 6.1.1 rootful connection
  `podman-machine-default-root`, ARM64 Mac, 8 GiB VM. The existing
  `kind-marina-local` installation was stopped during the test and left unchanged.
- Guide and script: [Marina on a local Kind cluster](../../deploy/kubernetes/marina-local/README.md),
  `scripts/marina-local.py`
- Result: one command installs Marina and every platform component without
  Deck; `verify` passes on a fresh cluster. A Turn, your own agent and a Run
  created with kubectl worked. One real model call was made. No commit or
  branch reached GitHub.

## Fresh install

`marina-local.py down` then `up` with no keys: 5 min 10 s with warm image
caches. `verify` then took 1 min 3 s:

| Check | Result |
| --- | --- |
| `https://marina.localhost:9443/readyz`, kagent UI, Jaeger UI through kgateway | PASS, 200 each |
| Turn created with kubectl, through Marina, kagent, a gVisor sandbox, the egress proxy, Marina's gateway and the fake model | PASS, `Succeeded`, 20 tokens |
| `examples/hello-agent` built, pushed to the local registry, prepared and invoked with `kagent-cli` | PASS, reply returned; prepared after setup downloads were closed |
| Unpinned image in `marina-system` | PASS, denied by Kyverno |

The default kubectl context stayed `kind-marina-local`.

## Real model and GitHub

On the earlier hand-built version of the same cluster, Marina's model was
switched to agentgateway with `openai/gpt-5-mini`. One Turn succeeded with
1021 tokens through agentgateway and OpenRouter; agentgateway's traces reached
Jaeger. An earlier attempt was rejected by agentgateway before reaching the
provider (see the key finding below).

`up` was then rerun on the fresh cluster with the OpenRouter key, the
`jmlero/marina-acceptance` repository and token, check
`node --test tests/clamp.test.cjs` and allowed paths `src`, `tests`. The rerun
took 1 min 13 s and changed nothing else.

| Step | Result |
| --- | --- |
| Project `demo/hello` | `Bound` to `jmlero/marina-acceptance` |
| Workload `change` (repository-change, pull request, review by `openai/gpt-6-luna`) | Accepted |
| Run `demo/run-1` created with kubectl, intake closed | `Pending`, Accepted |
| `intake.runID=demo.run-1` | claimed, base `151d521` pinned, plan revision 1, Decision `run-1-plan-1`, `WaitingDecision`, 0 tokens |
| `POST .../runs/run-1/cancel` with an Idempotency-Key | `Cancelled`; intake closed again |
| GitHub | `main` still `151d521`; no branch for this run |

Approving the plan and landing a Run stays open (MAR-11), by the operator's
choice for this test.

## Found and fixed

| Finding | Fix |
| --- | --- |
| Every Turn created through the platform API failed at its first model call (401): the conversation gateway required a Deck-style `msg_` ID and split the credential at the first dot, while platform Turn IDs are `<namespace>.<name>`. | The gateway splits at the last dot and no longer requires the prefix; the ID is still checked against the journal and the HMAC credential. Test `TestGatewayAdmitsPlatformTurnIdentity`. |
| agentgateway compares client and provider keys byte for byte; key files with a trailing newline are rejected. | The script writes keys without newlines and strips given key files; the guide says so. |
| `install-kagent-trial.py` only accepted the old node name, address and context. | `--context`, `--node`, `--node-ip`; policies follow the node address. Test added. |
| The registry container shares the node's network, so the node could not be deleted first. | `down` removes the registry first. |
| The certificate-renewal script and its test used OpenSSL-only options, so `make check-installation` failed with macOS LibreSSL. | Both read the certificate text instead; the test passes with LibreSSL 3.3.6 and OpenSSL 3.6.4. |

## Switch to the new cluster and restart

The old `kind-marina-local` cluster, its registry and its registry volume were
deleted at the operator's request. After a Podman VM restart the new node came
back at `10.89.1.4` (was `10.89.1.3`); `up` was rerun with the same options.

| Finding | Fix |
| --- | --- |
| `verify` failed: the chat Turn stayed pending until its deadline (`Failed`, 0 tokens). The sandbox agent tried to fetch its pause image from `registry.k8s.io`, which is blocked once setup downloads are closed; its cached copy did not survive the restart. | `up` mirrors the pinned pause image into the local registry and points the `gvisor-default` SandboxConfig at it. |

Then `atelet` and `kint-worker` were restarted and `verify` ran again: all
PASS. The Turn used the real model through agentgateway (1531 tokens); your own
agent used the fake model.

## Checks

- `make -C marina check`: pass.
- `make check-installation` (48 tests, with `HELM` set): pass.
- Not run: `make conformance` and Deck checks; Deck code was not changed.

## Not covered

- Docker as the Kind provider: supported by the script, not run here.
- x86-64 machines: the engine lock pins ARM64 images only.
- A Run landing on main, and own agents' spend limits beyond their own
  agentgateway key.
