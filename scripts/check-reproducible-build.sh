#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

version=${VERSION:-dev}
commit_sha=${COMMIT_SHA:-$(git -C "$root_dir" rev-parse HEAD)}
source_date_epoch=${SOURCE_DATE_EPOCH:-$(git -C "$root_dir" show -s --format=%ct HEAD)}
build_date=${BUILD_DATE:-$(date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ)}
ldflags="-s -w -X main.version=$version -X main.commitSHA=$commit_sha -X main.buildDate=$build_date"

for output in first second; do
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
    go build -tags=purego -trimpath -buildvcs=false \
    -ldflags="$ldflags" -o "$tmp_dir/$output" "$root_dir/cmd/archive-wrapper"
done

first_sum=$(sha256sum "$tmp_dir/first" | cut -d' ' -f1)
second_sum=$(sha256sum "$tmp_dir/second" | cut -d' ' -f1)
[[ $first_sum == "$second_sum" ]]
echo "reproducible binary sha256=$first_sum"
