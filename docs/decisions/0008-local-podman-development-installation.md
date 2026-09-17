# Local Podman development installation

- Status: Accepted for local development
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

Repository and model selection remain explicit. The local profile runs Marina
without a workflow and with new work intake disabled until those choices and
their credentials/checks are supplied. Disabling intake preserves recovery of
already journaled acquisitions, commands, and progress; it is not a pause switch.
TLS must therefore be configurable without enabling repository execution.

This installation validates packaging, public HTTP integration, persistent stores,
worker tooling, and network isolation. It does not validate model compatibility,
repository mutations, or the complete autonomous workflow. It also does not
satisfy the always-on requirement while the host Mac is asleep or off. The
production cluster, backup policy, and full workflow acceptance remain open.

See the [local installation guide](../../deploy/kubernetes/local-podman/README.md)
for operation, reproduction, and the remaining execution prerequisites.
