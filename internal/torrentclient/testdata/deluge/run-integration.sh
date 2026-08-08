#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)
cd "$repo_root"

active_container=""
active_import_dir=""

cleanup_current() {
  if [[ -n "$active_container" ]]; then
    docker stop "$active_container" >/dev/null 2>&1 || true
    active_container=""
  fi

  if [[ -n "$active_import_dir" && -d "$active_import_dir" ]]; then
    if command -v trash >/dev/null 2>&1; then
      trash "$active_import_dir"
    elif command -v gio >/dev/null 2>&1; then
      gio trash "$active_import_dir"
    else
      printf 'Temporary integration data remains at %s\n' "$active_import_dir" >&2
    fi
    active_import_dir=""
  fi
}

trap cleanup_current EXIT INT TERM

run_matrix_entry() {
  local client_type=$1
  local dockerfile=$2
  local image=$3
  local published_port=""
  local ready=false

  printf 'Building %s fixture\n' "$client_type"
  docker build -f "$dockerfile" -t "$image" .

  active_import_dir=$(mktemp -d "${TMPDIR:-/tmp}/seasonpackarr-${client_type}.XXXXXX")
  active_import_dir=$(cd "$active_import_dir" && pwd -P)
  active_container="seasonpackarr-${client_type}-integration-$$"

  docker run -d --rm \
    --name "$active_container" \
    -p 127.0.0.1::58846 \
    -v "$active_import_dir:$active_import_dir" \
    "$image" >/dev/null

  for _ in {1..30}; do
    published_port=$(docker port "$active_container" 58846/tcp | awk -F: 'END { print $NF }')
    if [[ -n "$published_port" ]] && (: >/dev/tcp/127.0.0.1/"$published_port") 2>/dev/null; then
      ready=true
      break
    fi
    sleep 1
  done

  if [[ "$ready" != true ]]; then
    docker logs "$active_container"
    return 1
  fi

  # The TCP listener opens before the V2 daemon finishes initializing RPC.
  sleep 2

  printf 'Running %s live integration suite on port %s\n' "$client_type" "$published_port"
  SEASONPACKARR_TEST_DELUGE_TYPE="$client_type" \
  SEASONPACKARR_TEST_DELUGE_HOST=127.0.0.1 \
  SEASONPACKARR_TEST_DELUGE_PORT="$published_port" \
  SEASONPACKARR_TEST_DELUGE_USER=seasonpackarr \
  SEASONPACKARR_TEST_DELUGE_PASS=integration \
  SEASONPACKARR_TEST_IMPORT_DIR="$active_import_dir" \
    go test -v -count=1 -run TestDelugeImportLive ./internal/torrentclient

  cleanup_current
}

run_matrix_entry \
  deluge-v1 \
  internal/torrentclient/testdata/deluge/Dockerfile.v1 \
  seasonpackarr-deluge-test:v1

run_matrix_entry \
  deluge-v2 \
  internal/torrentclient/testdata/deluge/Dockerfile.v2 \
  seasonpackarr-deluge-test:v2
