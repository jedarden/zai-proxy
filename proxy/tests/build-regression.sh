#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
image_tag="${IMAGE_TAG:-zai-proxy:ci}"
build_output="$(mktemp)"
trap 'rm -f "$build_output"' EXIT

cd "$repo_root"

if formatted=$(gofmt -l proxy/main.go); [[ -n "$formatted" ]]; then
  echo "unformatted Go files:" >&2
  printf '%s\n' "$formatted" >&2
  exit 1
fi

go build -buildvcs=false -o "$build_output" ./proxy
go test ./proxy

docker build \
  --pull=false \
  --file proxy/Dockerfile \
  --tag "$image_tag" \
  .
