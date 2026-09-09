#!/bin/sh
# Run manually or through a user LaunchAgent. Never changes Podman's default
# connection and only starts the named local cluster if it already exists.
set -eu
podman_bin=$(command -v podman)
machine_state=$("$podman_bin" machine inspect podman-machine-default --format '{{.State}}')
if [ "$machine_state" != running ]; then
  "$podman_bin" machine start podman-machine-default
fi
if "$podman_bin" --connection podman-machine-default-root container exists marina-local-control-plane; then
  "$podman_bin" --connection podman-machine-default-root start marina-local-control-plane
fi
