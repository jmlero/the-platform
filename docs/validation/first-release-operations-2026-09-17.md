# First-release operations validation — 2026-09-17

This record supports OPS-03, OPS-04, OPS-05, and OPS-06. It is a dated local
Podman/Kind result, not current installation guidance. Use the
[operations runbook](../../deploy/kubernetes/first-release/OPERATIONS.md) for
procedures and the [final repository acceptance](poc-mvp-2026-09-21.md) for later
live workflow results.

## Results

| Item | Result | Evidence |
| --- | --- | --- |
| OPS-03 | Passed locally | Both TLS leaf certificates were renewed without replacing the CA, operator/service credentials, task state, or Marina identity. Deck served the renewed certificate and both services restarted successfully. |
| OPS-04 | Partially passed; remains Verify | Operator and Deck-service credentials overlapped, reloaded, and revoked without Pod restart. Existing sessions retained their documented lifetime. Git/model file reload and empty-file denial passed local tests; issuer-side Git/model revocation was not exercised. |
| OPS-05 | Passed | Offline artifact retention produced a reviewable dry-run plan, protected every referenced artifact, refused active maintenance, bounded deletion, and rejected stale or modified plans. No persistent installation evidence was deleted. |
| OPS-06 | Passed | The runbook covers disk-full, unavailable-provider, stuck-cancellation, and failed-worker recovery without direct ledger edits. Restore procedures remain deferred with OPS-01/02. |

## Certificate and identity evidence

`scripts/renew-installation-certificates.py` renewed only the existing Deck and
Marina leaf key/certificate pairs. Rollback files remained outside Git.

| Certificate | Serial | Validity (UTC) |
| --- | --- | --- |
| Deck | `6441A5B66FB705BB72DA84C7319D5A11` | 2026-09-17 11:41:03 through 2026-12-16 11:41:03 |
| Marina | `D141AD2BCB88969930C4661CE59707C1` | 2026-09-17 11:41:03 through 2026-12-16 11:41:03 |

The rehearsal checked the certificate actually served by Deck. Existing
authenticated session, task-state, and agent-ID sets survived both rollouts.
CA rollover was not tested.

## Credential rotation evidence

`verify-local-podman-operations.py`:

1. added temporary operator and service fingerprints beside the originals;
2. authenticated both generations without restarting Pods;
3. replaced Marina's mounted service credential and observed rejection of the
   retired value;
4. confirmed pre-existing session behavior; and
5. restored the original private configuration and logged out temporary sessions.

Missing or empty Git/model credential files denied upstream requests. Model
credential checks occurred before budget reservation; requests already sent with
unknown usage retained their reservation. Provider-side token revocation was not
part of this proof.

## Artifact retention evidence

The retention CLI and behavior tests covered maintenance exclusion, historical
reference protection, dry-run/apply integrity, stale plans, age and batch limits,
and temporary real SQLite/filesystem stores. The runbook requires draining work,
holding the maintenance lock, reviewing the plan, and verifying the result.

## Checks run

- `make check`
- `make conformance`
- certificate/identity preservation tests
- credential reload and empty-file denial tests
- artifact-retention and maintenance-exclusion tests
- the local operations rehearsal
- `verify-local-podman-ledger.py` after upgrade

The conformance suite used real Deck with deterministic Git/runtime dependencies.
This report did not establish live model or repository-workflow acceptance; that
evidence was recorded later in
[poc-mvp-2026-09-21.md](poc-mvp-2026-09-21.md).
