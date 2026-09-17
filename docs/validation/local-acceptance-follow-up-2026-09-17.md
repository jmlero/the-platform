# Local first-release acceptance follow-up — 2026-09-17

## Scope and result

The operator requested the remaining first-release acceptance work and removed
RUN-10 because Kubernetes runs on the Mac. The operator subsequently clarified
that this installation is localhost-only, not a LAN service. [Decision 0007](../decisions/0007-sovereign-first-usable-release.md)
records both changes. RUN-10 is retired without renumbering other tasks.

**RUN-08 is complete for authenticated local access.** Fresh local installation,
network, fixture and idle-restart checks passed. The live repository exercises
remain blocked by an empty repository-scoped `git-token` file. The operator
confirmed they will supply it later. No developer Git credential was copied
into the installation, no live coding task was admitted, and no inference request,
commit, PR or merge was made in this follow-up. Existing intake remains disabled.

The normal repository outcome remains required under RUN-09: a real coding Job,
separate verification Job, one review-ready PR, current required CI and
downloadable evidence. Removing the disconnected-laptop task does not establish
that outcome or waive recovery, cancellation, failure or budget checks.

## Environment and configuration

- Source HEAD before documentation changes: `63fdd09ee78f0d7c4b2cb01f00499053ef722514`.
- Kubernetes context: `kind-marina-local`; Podman connection:
  `podman-machine-default-root`; existing `marina-local-control-plane` node.
- Existing `appdeck-data` and `marina-data` PVCs remain bound and preserved.
- Deck URL: `https://localhost:8443`, using the existing loopback-bound
  `8443 -> 30443` Podman/Kind mapping. No interactive `kubectl port-forward`.
- Private workflow SHA-256:
  `77b46b3c4ea67a908e9d188eb7c8d1a56013409b0e67d6b8cda1a391add3fb6a`.
- Worker image:
  `localhost/the-platform/marina-worker@sha256:2ac47cfd4a6ea0dc81d478e507b68b21d5e50a49e9b29caa76d8c551dc7def47`.
- Model configuration remains `z-ai/glm-5.3-flash` at the explicit OpenRouter
  endpoint, with the existing nonzero USD admission rates and previously
  recorded €5 total test authorization. This follow-up adds no model spend.
- Read-only GitHub inspection confirmed private, unarchived
  `jmlero/marina-acceptance`, default branch `main`, unchanged baseline
  `10a96c6221f5c8a128ccb2617d8be27213d7b296`. Both baseline Git emails are
  `jmlero@hotmail.com`.

`python3 scripts/check-installation.py <private-installation>` renders the
overlay and verifies both TLS chains. Its sole reported blocker remains:
`RUN-03: git-token is absent or empty`. This does not establish token permissions
or prove that the configured repository workflow has executed.

## Checks actually run

### Installation and offline fixture

`make check-installation` passed all eight existing tests.

`python3 scripts/verify-local-podman-acceptance.py <pinned-worker-image>` passed
with real temporary Kubernetes Jobs. The unique resource prefix was
`acceptance-1789657190407820000`.

- The unrestricted baseline reached all tested destinations.
- The controller-labelled probe reached Deck over trusted TLS, Kubernetes,
  GitHub API/source endpoints and OpenRouter; direct GHCR and `1.1.1.1` traffic
  were blocked.
- The isolated worker reached Marina over trusted TLS; direct GitHub,
  OpenRouter, GHCR, Kubernetes API, Deck and `1.1.1.1` connections were blocked.
- The original offline fixture produced three passing tests and four expected
  failures. Applying the known repair in the disposable probe workspace passed
  all seven tests without downloading packages.
- All three probe Jobs and their ConfigMaps were removed. The worker namespace
  was empty at the final check.

This revalidates RUN-04/06 preparation. The known repair was applied by the probe,
not a model; this is not RUN-09 live harness evidence.

### Local access and idle recovery

`python3 scripts/verify-local-podman-ledger.py <private-installation>` passed:

- Trusted localhost TLS and readiness; unauthenticated operator access rejected.
- Operator login succeeded; its session survived the Deck restart.
- Deck and Marina were restarted separately and returned ready.
- The task and Marina logical identity survived both restarts.
- Disabled intake did not claim the task or create a run.
- Verification task `tsk_5ed44aad5ca19be4a528` was left cancelled.

These observations satisfy the revised RUN-08 and reconfirm idle persistence.
They do not satisfy RUN-11, which requires restarts during implementation and CI
waiting with a real run, worker and external Git identities.

### Localhost-only final state

A LAN proxy was briefly prepared while interpreting the original RUN-08 wording.
After the operator clarified the scope, it was fully reverted: its LaunchAgent
was unloaded and removed, its separate certificate/key/configuration/log directory
was deleted, and the newly installed Caddy package was uninstalled. The preparation
script and tests created for it were also removed. No original installation
certificate, credential, volume or image was replaced.

The final listener check found nothing on TCP 9443. A trusted TLS readiness
request to `https://localhost:8443/readyz` returned `ready` at
`2026-09-17T15:52:45Z`. Marina still runs `serve --intake-enabled=false`, with
service-account token mounting disabled. The private LAN proxy directory is absent.

## Remaining acceptance conditions

| Item | State | Evidence or remaining condition |
| --- | --- | --- |
| RUN-01/02/04/06/07 | Done | Existing installation evidence remains; fresh network, fixture and idle-restart checks above also passed. |
| RUN-03 | Open | Repository and fixture confirmed; scoped Git credential still absent. |
| RUN-05 | Open | Existing overlay renders with pinned images and valid TLS; credential setup and executable workflow validation remain. |
| RUN-08 | Done | Authenticated localhost access and persistent session verified; second-device/LAN testing removed from scope. |
| RUN-09 | Verify | Needs the real pinned harness/gateway/repository run, separate verification, PR/CI and evidence. |
| RUN-10 | Removed | Explicit operator decision; ID retired. |
| RUN-11 | Verify | Needs Deck/Marina restarts during implementation and CI waiting. Idle restarts are insufficient. |
| RUN-12 | Verify | Needs cancellation of a real Marina-managed active worker and ordered termination/acknowledgement evidence. |
| RUN-13 | Verify | Needs live tasks exercising failed local checks, failed CI and missing required CI. |
| RUN-14 | Verify | Needs an actual verified disposable PR whose head is then advanced normally. |
| RUN-15 | Verify | Needs separate Deck, model and Git interruptions during live work, followed by restoration and recovery evidence. |
| RUN-16 | Verify | Needs time, token, attempt and nonzero cost exhaustion, including uncertain model usage reservations. |
| RUN-17 | Verify | Needs failure of a Marina-managed worker and a separately authorized follow-up. |
| RUN-18 | Open | This report records preparation and blockers; live task/run/Job IDs, PR revision and the remaining exercise results are still required. |

Resume with the supplied repository-scoped credential, validate its configured
repository access without logging it, synchronize it through the existing Secret
mechanism, and validate the execution overlay before admitting bounded fixture
tasks. Keep the previously authorized total spend limit and localhost-only access.
