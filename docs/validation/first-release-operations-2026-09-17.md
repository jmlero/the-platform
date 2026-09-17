# First-release and operations follow-up — 2026-09-17

Scope: RUN-03/05/08–18 and OPS-03–06 from TODO.md. This follows the
[earlier local installation report](run-01-18-2026-09-17.md). No live OpenRouter
inference, repository mutation, PR creation or laptop-off result is claimed.

The operator confirmed that only local Podman/Kind is available and deferred model
selection, provider configuration and the test-spend decision. A private
`model-token` file is now populated; its value was not printed, recorded or sent
to a provider. The repository-scoped `git-token` remains absent/empty. Intake is
still disabled. Backup/restore remains deferred, outside this work.

## Results by TODO item

| Item | Result | Evidence / remaining condition |
| --- | --- | --- |
| RUN-03 | Prepared; open | GitHub read-only inspection confirms private `jmlero/marina-acceptance`, default branch `main`, not archived. Baseline `10a96c6221f5c8a128ccb2617d8be27213d7b296` and intentionally failing regression CI remain intact. The fixture, allowed paths and offline checks exist; scoped Git credentials are still missing. |
| RUN-05 | Prepared; open | Added a local, redacted configuration preflight; private overlay pins the new Marina image below and retains existing stores/identities. Model placeholders, model rates, scoped Git credential and chosen test budget remain unresolved. No placeholder is treated as an executable model choice. |
| RUN-08 | Open | The existing endpoint remains trusted localhost TLS. No independent host, permanent second-device ingress or second-device sign-in evidence was supplied. |
| RUN-09 | Verify | No inference attempted. Exact model, nonzero pricing and authorized total spend remain necessary even though a private key file exists. |
| RUN-10 | Verify | Requires RUN-09 plus a host independent of the Mac; local Kind cannot supply laptop-off evidence. |
| RUN-11 | Verify | Certificate and image rollouts preserve the idle installation. Active implementation/CI-wait restarts still need a configured live workflow. |
| RUN-12 | Verify | No live coding worker exists to cancel. Gateway fencing and cancellation checks remain deterministic evidence, not this acceptance result. |
| RUN-13 | Verify | Real-process failed-verification and missing-CI cases pass. The known fixture CI remains failed. Selected-installation failed/missing gates still await live tasks. |
| RUN-14 | Verify | Real-process changed-head rejection passes. There is no live verified PR to change. |
| RUN-15 | Verify | Added precise provider recovery procedures. Live active-run provider interruptions are pending configuration. |
| RUN-16 | Verify | Budget tests pass; added proof that locally missing model credentials do not reserve budget. Actual configured-model time/token/attempt/nonzero-cost exhaustion remains pending. |
| RUN-17 | Verify | Failure and follow-up recovery procedures are documented. No Marina-managed model attempt was failed or evicted in this follow-up. |
| RUN-18 | Partial report; open | This report records all scoped items and missing evidence. Live run/attempt IDs, PR revision and RUN-09–17 installation results do not yet exist. |
| OPS-03 | Done locally | Both leaf certificates renewed and installed; served Deck certificate verified; existing session, task states and Marina identity survived both rollouts. Localhost leaf trust updated. |
| OPS-04 | Partial; Verify | Live operator/service overlap, revocation, mounted-file rotation and session semantics passed without process restart. Original credentials restored. Git/model file-reload and fail-closed behavior pass local tests; actual issuer credential rotation and revocation remain pending. |
| OPS-05 | Done | Implemented offline bounded retention with reviewed dry-run plans, historical reference protection, exclusive maintenance lock, active-work refusal and stale-plan/integrity checks. Behavior tests and CLI application against temporary real SQLite/filesystem stores pass. No production/persistent evidence was deleted. |
| OPS-06 | Done | [Operations runbook](../../deploy/kubernetes/first-release/OPERATIONS.md) covers detection, containment and recovery for disk full, unavailable providers, stuck cancellation and failed workers without ledger edits; deferred restore procedures are explicitly excluded. |

## Local TLS and identity evidence

`scripts/renew-installation-certificates.py` passed its behavior test, then renewed
the existing installation's two leaf key/certificate pairs while preserving CA,
operator/service credentials, signing key, artifact credential and all other
configuration. Rollback files remain outside Git in the private installation's
`certificate-backup-20260917T114103Z-b8f508f7` directory.

| Certificate | Serial | Validity (UTC) |
| --- | --- | --- |
| Deck | `6441A5B66FB705BB72DA84C7319D5A11` | 2026-09-17 11:41:03 through 2026-12-16 11:41:03 |
| Marina | `D141AD2BCB88969930C4661CE59707C1` | 2026-09-17 11:41:03 through 2026-12-16 11:41:03 |

The operations rehearsal checked the actual TLS leaf served by Deck against the
renewed file, not merely certificate-chain validity. Both services rolled out
successfully; the existing authenticated session and exact task-state/agent-ID
sets survived. The Mac keychain now trusts the new Deck leaf for localhost only.
CA rollover was not performed; the renewal helper intentionally preserves it.

## Credential rotation evidence

The finalized `verify-local-podman-operations.py` completed all four phases:

1. Added temporary operator/service fingerprints alongside the originals; both
   generations authenticated without restarting Pods.
2. Replaced Marina's mounted service credential, retired the old fingerprints,
   observed rejection of old tokens and successful `/marina doctor` with the new
   credential. Exact Pod UIDs and task/agent identities remained unchanged.
3. Proved sessions created before token retirement remain valid, as documented.
4. Restored original Secret data, revoked temporary fingerprints, checked Marina
   connectivity and logged out temporary probe/rehearsal sessions. Private login
   and service-token files were not replaced.

New gateway/API tests also exposed and fixed empty-credential handling: a missing
or empty configured Git/model token now prevents upstream requests; the model
check happens before reservation. Actual sent requests with uncertain usage keep
their reservation. Tests use local doubles and synthetic credentials; they do not
establish OpenRouter/GitHub issuer revocation.

## Source and deployed image

Validation started from base `fdbed84fbb1e28eadf43e814226c113b2655889c` with the
operations changes in the working tree. No registry publication or production
deployment was performed.

The Marina Linux/ARM64 image was built with the repository Dockerfile (Go 1.26.5)
and version `ops-20260917`, imported into the existing Kind node, and deployed by
its containerd manifest digest:

`localhost/the-platform/marina@sha256:f6d22d81d3ff287ebe290ce7765e84dbdc70fb850245f01fc7fd88d0c21a2711`

The private overlay pins the same digest; `kustomization.before-ops-20260917.yaml`
retains its prior image configuration for rollback. Deck and worker image digests
are unchanged from the earlier report. The existing `appdeck-data` and
`marina-data` PVCs, disabled intake and local cluster were preserved. `/marina
version` reports `ops-20260917`; `/marina doctor` reports Deck available.

## Verification and next execution

- `make check`: passed installation Python checks, all product tests, Go vet,
  CLI packaging checks and Svelte diagnostics (zero errors/warnings).
- `make conformance`: passed the CLI session test, provider conformance and real
  Deck repository-flow tests for completion, promoted completion, failed local
  verification, changed head, closed PR, missing CI and missing authority.
- Narrow behavior-first tests: certificate/identity preservation; private
  configuration blocker redaction; artifact retention and maintenance exclusion;
  Git/model credential reload and empty-file denial without budget reservation.
- Local operations rehearsal: passed renewed TLS and restored-credential checks.
- Post-upgrade `verify-local-podman-ledger.py`: passed trusted TLS, authentication,
  session/task/Marina identity persistence through another restart of each service
  and disabled-intake behavior. Existing verification task
  `tsk_5ed44aad5ca19be4a528` is cancelled with no execution run.

The conformance suite uses real Deck with deterministic Git/runtime dependencies.
It is not live model/provider/Kubernetes-workflow acceptance. The named remaining
exercises and per-run evidence fields are in
[ACCEPTANCE.md](../../deploy/kubernetes/first-release/ACCEPTANCE.md). Run preflight,
choose model/rates/spend and configure scoped Git credentials before RUN-09. Keep
RUN-10 open until an independent host and second-device result are available.
