#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
compose_file="$root_dir/testdata/container/compose.yaml"
project="archive-wrapper-test-${CI_JOB_ID:-$$}-${RANDOM}"
image="${ARCHIVE_WRAPPER_IMAGE:-archive-wrapper:container-test}"
export ARCHIVE_WRAPPER_IMAGE="$image"
tmp_dir=$(mktemp -d)
inspect_container_id=""

compose=(docker compose --project-name "$project" --file "$compose_file")

cleanup() {
  local status=$?
  if (( status != 0 )); then
    "${compose[@]}" ps >&2 || true
    "${compose[@]}" logs --no-color >&2 || true
    if [[ -n ${ARCHIVE_WRAPPER_ARTIFACT_DIR:-} ]]; then
      mkdir -p "$ARCHIVE_WRAPPER_ARTIFACT_DIR"
      "${compose[@]}" ps --all >"$ARCHIVE_WRAPPER_ARTIFACT_DIR/compose-ps.txt" 2>&1 || true
      "${compose[@]}" logs --no-color >"$ARCHIVE_WRAPPER_ARTIFACT_DIR/compose.log" 2>&1 || true
      docker image inspect "$image" >"$ARCHIVE_WRAPPER_ARTIFACT_DIR/image-inspect.json" 2>&1 || true
    fi
  fi
  if [[ -n $inspect_container_id ]]; then
    docker rm --force "$inspect_container_id" >/dev/null 2>&1 || true
  fi
  "${compose[@]}" --profile lock --profile isolation down --volumes --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$tmp_dir"
  exit "$status"
}
trap cleanup EXIT

wait_for_container_state() {
  local service=$1
  local expected=$2
  local timeout=${3:-60}
  local container_id
  container_id=$("${compose[@]}" ps --all --quiet "$service")
  for ((attempt = 0; attempt < timeout; attempt++)); do
    if [[ $(docker inspect --format '{{.State.Status}}' "$container_id") == "$expected" ]]; then
      return 0
    fi
    sleep 1
  done
  echo "$service did not reach state $expected" >&2
  return 1
}

wait_for_healthy() {
  local service=$1
  local timeout=${2:-90}
  local container_id
  container_id=$("${compose[@]}" ps --all --quiet "$service")
  for ((attempt = 0; attempt < timeout; attempt++)); do
    if [[ $(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$container_id") == healthy ]]; then
      return 0
    fi
    sleep 1
  done
  echo "$service did not become healthy" >&2
  return 1
}

wait_for_probe_failure() {
  local service=$1
  local timeout=${2:-30}
  for ((attempt = 0; attempt < timeout; attempt++)); do
    if ! "${compose[@]}" exec -T "$service" /usr/local/bin/archive-wrapper healthcheck --timeout 1s >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "$service health probe did not fail" >&2
  return 1
}

published_address() {
  "${compose[@]}" port "$1" 9095 | head -n 1 | sed -E 's/^0\.0\.0\.0:/127.0.0.1:/; s/^\[::\]:/[::1]:/'
}

inspect_image_contract() {
  local image_user image_entrypoint image_command version_label revision_label created_label version_output
  image_user=$(docker image inspect --format '{{.Config.User}}' "$image")
  image_entrypoint=$(docker image inspect --format '{{json .Config.Entrypoint}}' "$image")
  image_command=$(docker image inspect --format '{{json .Config.Cmd}}' "$image")
  version_label=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image")
  revision_label=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")
  created_label=$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.created"}}' "$image")
  version_output=$(docker run --rm --entrypoint /usr/local/bin/archive-wrapper "$image" version)

  [[ $image_user == 65532:65532 ]]
  [[ $image_entrypoint == '["/usr/local/bin/archive-wrapper"]' ]]
  [[ $image_command == '["run"]' ]]
  [[ $version_output == *"version=$version_label"* ]]
  [[ $version_output == *"commit=$revision_label"* ]]
  [[ $version_output == *"build_date=$created_label"* ]]

  if docker run --rm --entrypoint /bin/sh "$image" -c true >/dev/null 2>&1; then
    echo "runtime image unexpectedly contains a shell" >&2
    return 1
  fi

  inspect_container_id=$(docker create "$image")
  docker export "$inspect_container_id" >"$tmp_dir/rootfs.tar"
  docker rm "$inspect_container_id" >/dev/null
  inspect_container_id=""
  tar -tf "$tmp_dir/rootfs.tar" >"$tmp_dir/rootfs-files"
  if grep -qE '(^|/)(\.env|config\.yaml)$|^src/|^var/lib/archive-wrapper/data/leveldb/' "$tmp_dir/rootfs-files"; then
    echo "runtime image contains source, config, secret, or runtime state" >&2
    return 1
  fi
}

if [[ ${ARCHIVE_WRAPPER_SKIP_BUILD:-0} != 1 ]]; then
  make -C "$root_dir" docker-build IMAGE="$image"
fi
inspect_image_contract

# The wrapper must remain alive but non-serving until PostgreSQL is available.
"${compose[@]}" up --detach --no-deps wrapper1
wait_for_container_state wrapper1 running
wait_for_probe_failure wrapper1
wrapper_address=$(published_address wrapper1)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container,purego ./tests/container -run '^TestUnavailableQueryIsRejected$' -count=1

"${compose[@]}" up --detach postgres
wait_for_healthy postgres
wait_for_healthy wrapper1

wrapper_address=$(published_address wrapper1)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestConcurrentClients$' -count=1

# A PostgreSQL outage withdraws readiness, and recovery does not require a wrapper restart.
"${compose[@]}" stop postgres
wait_for_probe_failure wrapper1
wrapper_address=$(published_address wrapper1)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestUnavailableQueryIsRejected$' -count=1
"${compose[@]}" start postgres
wait_for_healthy postgres
wait_for_healthy wrapper1

# Restart uses the same command and volume and must preserve the indexed cursor.
"${compose[@]}" stop --timeout 15 wrapper1
wrapper_exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$("${compose[@]}" ps --all --quiet wrapper1)")
[[ $wrapper_exit_code -eq 0 ]]
"${compose[@]}" start wrapper1
wait_for_healthy wrapper1
wrapper_address=$(published_address wrapper1)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestConcurrentClients$' -count=1

# A second owner of the same LevelDB volume must fail instead of serving stale state.
"${compose[@]}" --profile lock up --detach --no-deps wrapper-lock
wait_for_container_state wrapper-lock exited
lock_exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$("${compose[@]}" ps -aq wrapper-lock)")
[[ $lock_exit_code -ne 0 ]]
"${compose[@]}" logs --no-color wrapper-lock | grep -q 'database is locked'

wrapper_id=$("${compose[@]}" ps -q wrapper1)
[[ $(docker inspect --format '{{.Config.User}}' "$wrapper_id") == 65532:65532 ]]
[[ $(docker inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$wrapper_id") == true ]]

logs=$("${compose[@]}" logs --no-color wrapper1 wrapper-lock)
if grep -qE 'container-secret|postgres://archive:' <<<"$logs"; then
  echo "container logs exposed PostgreSQL credentials" >&2
  exit 1
fi

data_volume="${project}_wrapper1_data"
docker run --rm --volume "$data_volume:/data:ro" \
  --entrypoint /bin/sh \
  postgres:17-bookworm@sha256:4f736ae292687621d4dbe0d499ffd024a36bd2ee7d8ca6f2ccd4c800f047b394 \
  -c 'test "$(stat -c %u:%g /data/data)" = 65532:65532 && test ! -e /data/archive-wrapper.log'

# Independent wrapper state must remain isolated even when PostgreSQL is shared.
"${compose[@]}" --profile isolation up --detach --no-deps wrapper2 wrapper3
wait_for_healthy wrapper2
wait_for_healthy wrapper3
wrapper1_address=$(published_address wrapper1)
wrapper2_address=$(published_address wrapper2)
wrapper3_address=$(published_address wrapper3)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper1_address,$wrapper2_address,$wrapper3_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestWrapperIsolation$' -count=1

"${compose[@]}" stop --timeout 15 wrapper2
wrapper2_exit_code=$(docker inspect --format '{{.State.ExitCode}}' "$("${compose[@]}" ps --all --quiet wrapper2)")
[[ $wrapper2_exit_code -eq 0 ]]
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper1_address,$wrapper3_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestWrapperIsolation$' -count=1

"${compose[@]}" start wrapper2
wait_for_healthy wrapper2
wrapper2_address=$(published_address wrapper2)
ARCHIVE_WRAPPER_TEST_ADDRESSES="$wrapper1_address,$wrapper2_address,$wrapper3_address" \
  GOCACHE="${GOCACHE:-/tmp/go-build-cache}" \
  go test -tags=container ./tests/container -run '^TestWrapperIsolation$' -count=1

echo "container lifecycle, shared-client, and wrapper-isolation smoke tests passed"
