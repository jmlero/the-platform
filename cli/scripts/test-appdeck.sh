#!/usr/bin/env sh
set -eu
appdeck_repository="${APPDECK_REPO:-../deck}"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/platformctl-appdeck.XXXXXX")"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
(cd "$appdeck_repository" && go build -o "$test_directory/appdeck" ./cmd/appdeck)
APPDECK_TEST_BINARY="$test_directory/appdeck" go test ./internal/command -run '^TestRealAppDeckOperatorSession$' -count=1 -v
