#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
image_tag="${IMAGE_TAG:-zai-proxy:ci}"
build_output="$(mktemp)"
trap 'rm -f "$build_output"' EXIT

cd "$repo_root"

mapfile -t go_files < <(find . -type f -name '*.go' -not -path './.git/*' -print)
if (( ${#go_files[@]} == 0 )); then
  echo "no Go files found" >&2
  exit 1
fi

if formatted=$(gofmt -l "${go_files[@]}"); [[ -n "$formatted" ]]; then
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
