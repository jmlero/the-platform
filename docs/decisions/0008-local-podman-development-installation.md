# Local Podman development installation

- Status: accepted for local development; repository and two-workload acceptance later passed
- Date: 2026-09-09
- Scope: Deck and Marina installation

The operator requested a real local Kubernetes installation using the existing
Podman setup on macOS and the Podman CLI.

Use a dedicated `marina-local` Kind cluster in the existing VM's root Podman
connection. Preserve the rootless default connection and its containers. Allocate
8 GiB to the VM on the validated 48 GiB host. Pin the node image and use Calico
to enforce the existing worker network policies. Separate product PVCs, service
identities, TLS, and operator authentication remain in place.

Expose only Deck through a loopback-bound host port, with a narrowly scoped
ingress rule for the observed Podman forwarding gateway. Store installation
credentials outside the repository. Trust only the Deck leaf certificate for
localhost in the operator's user keychain. A user LaunchAgent can start the
existing VM and cluster independently of an interactive terminal.

Repository and model selection remain explicit. The base local profile runs
Marina with new work intake disabled; private overlays enable the repository and
diagnostic workflows only after their credentials, checks, profiles, and budgets
are supplied. Disabling intake preserves recovery of already journaled work; it
is not a pause switch. TLS is configurable without enabling execution.

The initial installation validated packaging, public HTTP integration, persistent
stores, worker tooling, and network isolation. Later local exercises validated
the repository and diagnostic workflows; see the
[repository report](../validation/poc-mvp-2026-09-21.md) and
[two-workload report](../validation/agent-11-13-2026-09-22.md). The profile still
does not satisfy the always-on goal while the host Mac is asleep or off. GCP
workflow acceptance and backup/restore remain outside this local result.

See the [local installation guide](../../deploy/kubernetes/local-podman/README.md)
for operation, reproduction, and revalidation requirements.
