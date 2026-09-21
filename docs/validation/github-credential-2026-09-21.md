# Local GitHub credential installation — 2026-09-21

The operator supplied a classic GitHub personal access token for the local pilot
after being informed that its `repo` scope covers their accessible private
repositories. This is broader than the repository-scoped credential originally
planned. Marina's configured destination remains `jmlero/marina-acceptance`.
The token's reported expiration is 2026-12-20 10:35:42 UTC.

## Changes

- Read the operator-supplied token from the existing private installation's
  `git-token` file; its permissions were already `0600`.
- Updated only `data.git-token` in Secret `marina-credentials`, namespace
  `marina-system`, context `kind-marina-local`.
- Verified every other Secret data entry and Marina's Deployment specification
  remained unchanged. No service restart was needed.

## Verification

- `python3 scripts/check-installation.py
  ~/.local/share/the-platform/marina-local/installation` passed with no
  configuration blockers.
- Live read-only GitHub requests from the Mac succeeded for repository metadata,
  the default-branch Git reference, check runs, combined commit status, and the
  pull-request list. GitHub reported `repo` scope and repository push permission.
  The default head included the configured `acceptance-regression` check.
- Confirmed the token projected into the running Marina Pod matched the private
  file, comparing bytes without printing credential content. The comparison used
  the dedicated `marina-local-control-plane` node on Podman connection
  `podman-machine-default-root`.
- `kubectl --context kind-marina-local -n marina-system exec deployment/marina
  -- /marina doctor` passed.
- Marina retained `serve --intake-enabled=false`. The existing control-only
  deployment was preserved; the prepared repository-workflow profile was not
  enabled as part of credential installation.

No GitHub mutations, model calls, or coding tasks were performed. Git write
operations, authenticated GitHub access from Marina's network environment, and
the complete repository workflow remain unverified by this check. This report
does not mark RUN-03 or live workflow acceptance complete.
