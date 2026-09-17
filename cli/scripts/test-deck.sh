#!/usr/bin/env sh
set -eu
deck_repository="${DECK_REPO:-../deck}"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/platformctl-deck.XXXXXX")"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
(cd "$deck_repository" && go build -o "$test_directory/deck" ./cmd/deck)
DECK_TEST_BINARY="$test_directory/deck" go test ./internal/command -run '^TestRealDeckOperatorSession$' -count=1 -v
