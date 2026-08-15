#!/bin/sh

set -eu

cli_dir=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
test_dir=$(mktemp -d)
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM

target_os=$(go env GOOS)
target_arch=$(go env GOARCH)

make -C "$cli_dir" package \
  VERSION=v0.0.0-test \
  GOOS="$target_os" \
  GOARCH="$target_arch" \
  DIST_DIR="$test_dir"

archive="$test_dir/platformctl_0.0.0-test_${target_os}_${target_arch}.tar.gz"
if [ "$target_os" = "windows" ]; then
  archive="$test_dir/platformctl_0.0.0-test_${target_os}_${target_arch}.zip"
fi

test -f "$archive"

extract_dir="$test_dir/extracted"
mkdir -p "$extract_dir"
if [ "$target_os" = "windows" ]; then
  unzip -q "$archive" -d "$extract_dir"
  binary="$extract_dir/platformctl.exe"
else
  tar -xzf "$archive" -C "$extract_dir"
  binary="$extract_dir/platformctl"
fi

test -x "$binary"
test "$("$binary" --version)" = "platformctl v0.0.0-test"

if make -C "$cli_dir" package \
  VERSION=dev \
  GOOS="$target_os" \
  GOARCH="$target_arch" \
  DIST_DIR="$test_dir/invalid" >/dev/null 2>&1; then
  echo "package unexpectedly accepted a development version" >&2
  exit 1
fi
