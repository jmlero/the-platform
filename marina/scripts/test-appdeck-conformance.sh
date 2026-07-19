#!/usr/bin/env bash

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
marina_repository="$(cd "${script_directory}/.." && pwd)"
appdeck_repository="${APPDECK_REPO:-${marina_repository}/../deck}"

if [[ ! -f "${appdeck_repository}/go.mod" || ! -f "${appdeck_repository}/api/fixtures/v1/manifest.json" ]]; then
  echo "APPDECK_REPO must point to an AppDeck checkout with the v1 fixtures" >&2
  exit 2
fi
appdeck_repository="$(cd "${appdeck_repository}" && pwd)"

temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/marina-appdeck-conformance.XXXXXX")"
appdeck_pid=""

cleanup() {
  if [[ -n "${appdeck_pid}" ]] && kill -0 "${appdeck_pid}" 2>/dev/null; then
    kill "${appdeck_pid}" 2>/dev/null || true
    wait "${appdeck_pid}" 2>/dev/null || true
  fi
  if [[ -n "${temporary_directory}" && -d "${temporary_directory}" ]]; then
    rm -rf -- "${temporary_directory}"
  fi
}
trap cleanup EXIT INT TERM

mkdir -p "${temporary_directory}/data" "${temporary_directory}/go-cache"
appdeck_binary="${temporary_directory}/appdeck"
appdeck_log="${temporary_directory}/appdeck.log"
conformance_address="${APPDECK_CONFORMANCE_ADDR:-127.0.0.1:$((20000 + RANDOM % 20000))}"
conformance_url="http://${conformance_address}"

(
  cd "${appdeck_repository}"
  GOCACHE="${temporary_directory}/go-cache" go build -trimpath -o "${appdeck_binary}" ./cmd/appdeck
)

APPDECK_ADDR="${conformance_address}" \
APPDECK_DATA_DIR="${temporary_directory}/data" \
APPDECK_DEMO_AGENT=false \
APPDECK_LOG_LEVEL=warn \
"${appdeck_binary}" >"${appdeck_log}" 2>&1 &
appdeck_pid=$!

if ! APPDECK_CONFORMANCE_URL="${conformance_url}" \
  APPDECK_FIXTURES_DIR="${appdeck_repository}/api/fixtures/v1" \
  GOCACHE="${temporary_directory}/go-cache" \
  go test ./internal/appdeck -run '^TestRealAppDeckConformance$' -count=1 -v; then
  echo "AppDeck process log:" >&2
  sed -n '1,240p' "${appdeck_log}" >&2
  exit 1
fi
