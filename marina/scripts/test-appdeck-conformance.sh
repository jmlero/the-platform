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
umask 077
if ! command -v openssl >/dev/null 2>&1; then
  echo "openssl is required to generate the ephemeral conformance credential" >&2
  exit 2
fi
service_token="$(openssl rand -hex 32)"
service_token_file="${temporary_directory}/appdeck-token"
service_auth_file="${temporary_directory}/appdeck-service-auth.json"
printf '%s\n' "${service_token}" >"${service_token_file}"
service_token_sha256="$(printf '%s' "${service_token}" | openssl dgst -sha256 -r | awk '{print $1}')"
cat >"${service_auth_file}" <<EOF
{
  "version": 1,
  "identities": [
    {
      "id": "marina-conformance",
      "token_sha256": ["${service_token_sha256}"],
      "projects": ["prj_default"],
      "capabilities": [
        "agent.register",
        "command.ack",
        "command.list",
        "decision.answer",
        "decision.inspect",
        "decision.list",
        "event.append",
        "event.list",
        "event.stream",
        "evidence.record",
        "provider.info",
        "run.assigned.list",
        "run.block",
        "run.complete",
        "run.lease.renew",
        "run.plan.submit",
        "run.progress.append",
        "run.start",
        "run.verify",
        "work.claim.atomic",
        "work.claim.lookup",
        "work.inspect",
        "work.ready.list"
      ]
    }
  ]
}
EOF
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
APPDECK_SERVICE_AUTH_FILE="${service_auth_file}" \
"${appdeck_binary}" >"${appdeck_log}" 2>&1 &
appdeck_pid=$!

if ! APPDECK_CONFORMANCE_URL="${conformance_url}" \
  APPDECK_CONFORMANCE_TOKEN_FILE="${service_token_file}" \
  APPDECK_FIXTURES_DIR="${appdeck_repository}/api/fixtures/v1" \
  GOCACHE="${temporary_directory}/go-cache" \
  go test ./internal/appdeck -run '^TestReal(AppDeckConformance|RepositoryWorkflow)$' -count=1 -v; then
  echo "AppDeck process log:" >&2
  sed -n '1,240p' "${appdeck_log}" >&2
  exit 1
fi
