#!/usr/bin/env sh
set -eu
marina_repository="${MARINA_REPO:-../marina}"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/platformctl-marina.XXXXXX")"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
(cd "$marina_repository" && go build -tags sandbox -o "$test_directory/marina" ./cmd/marina)
MARINA_TEST_BINARY="$test_directory/marina" go test ./internal/command -run '^TestRealMarinaOperatorSession$' -count=1 -v
