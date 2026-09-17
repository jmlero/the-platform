#!/usr/bin/env bash

set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
marina_repository="$(cd "${script_directory}/.." && pwd)"
deck_repository="${DECK_REPO:-${marina_repository}/../deck}"

if [[ ! -f "${deck_repository}/go.mod" || ! -f "${deck_repository}/api/fixtures/v1/manifest.json" ]]; then
  echo "DECK_REPO must point to a Deck checkout with the v1 fixtures" >&2
  exit 2
fi
deck_repository="$(cd "${deck_repository}" && pwd)"

temporary_directory="$(mktemp -d "${TMPDIR:-/tmp}/marina-deck-conformance.XXXXXX")"
deck_pid=""

cleanup() {
  if [[ -n "${deck_pid}" ]] && kill -0 "${deck_pid}" 2>/dev/null; then
    kill "${deck_pid}" 2>/dev/null || true
    wait "${deck_pid}" 2>/dev/null || true
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
service_token_file="${temporary_directory}/deck-token"
service_auth_file="${temporary_directory}/deck-service-auth.json"
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
deck_binary="${temporary_directory}/deck"
deck_log="${temporary_directory}/deck.log"
conformance_address="${DECK_CONFORMANCE_ADDR:-127.0.0.1:$((20000 + RANDOM % 20000))}"
conformance_url="http://${conformance_address}"

(
  cd "${deck_repository}"
  GOCACHE="${temporary_directory}/go-cache" go build -trimpath -o "${deck_binary}" ./cmd/deck
)

DECK_ADDR="${conformance_address}" \
DECK_DATA_DIR="${temporary_directory}/data" \
DECK_DEMO_AGENT=false \
DECK_LOG_LEVEL=warn \
DECK_SERVICE_AUTH_FILE="${service_auth_file}" \
"${deck_binary}" >"${deck_log}" 2>&1 &
deck_pid=$!

if ! DECK_CONFORMANCE_URL="${conformance_url}" \
  DECK_CONFORMANCE_TOKEN_FILE="${service_token_file}" \
  DECK_FIXTURES_DIR="${deck_repository}/api/fixtures/v1" \
  GOCACHE="${temporary_directory}/go-cache" \
  go test ./internal/deck -run '^TestReal(DeckConformance|RepositoryWorkflow)$' -count=1 -v; then
  echo "Deck process log:" >&2
  sed -n '1,240p' "${deck_log}" >&2
  exit 1
fi
