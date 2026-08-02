#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT

for output in first second; do
  make -C "$root_dir" docker-build-multiarch \
    VERSION="${VERSION:-dev}" \
    COMMIT_SHA="${COMMIT_SHA:-unknown}" \
    SOURCE_DATE_EPOCH="${SOURCE_DATE_EPOCH:-0}" \
    BUILD_DATE="${BUILD_DATE:-unknown}" \
    DOCKER_BUILDER="${DOCKER_BUILDER:-archive-wrapper-builder}" \
    DOCKER_PLATFORMS="${DOCKER_PLATFORMS:-linux/amd64,linux/arm64}" \
    OCI_OUTPUT="$tmp_dir/$output.oci.tar"
done

first_sum=$(sha256sum "$tmp_dir/first.oci.tar" | cut -d' ' -f1)
second_sum=$(sha256sum "$tmp_dir/second.oci.tar" | cut -d' ' -f1)
[[ $first_sum == "$second_sum" ]]
echo "reproducible OCI sha256=$first_sum"
