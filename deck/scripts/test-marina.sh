#!/usr/bin/env sh
# Real-process conformance: a real Deck against Marina's API sandbox (MAR-08).
set -eu
marina_repository="${MARINA_REPO:-../marina}"
test_directory="$(mktemp -d "${TMPDIR:-/tmp}/deck-marina.XXXXXX")"
trap 'rm -rf "$test_directory"' EXIT HUP INT TERM
(cd "$marina_repository" && go build -tags sandbox -o "$test_directory/marina" ./cmd/marina)
go build -o "$test_directory/deck" ./cmd/deck
MARINA_TEST_BINARY="$test_directory/marina" DECK_TEST_BINARY="$test_directory/deck" \
	go test ./internal/marina -run '^TestRealMarinaWorkflow$' -count=1 -v
