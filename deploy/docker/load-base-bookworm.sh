#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKER_BIN="${DOCKER_BIN:-docker}"
OUTPUT_FILE="${OUTPUT_FILE:-${ROOT_DIR}/deploy/images/forward-stub-base-bookworm.tar.gz}"

fail() {
  echo "[ERROR] $*" >&2
  exit 1
}

require_command() {
  local cmd="$1"
  if ! command -v "${cmd}" >/dev/null 2>&1; then
    fail "Required command not found: ${cmd}"
  fi
}

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    fail "Image archive not found: ${path}"
  fi
}

require_docker_daemon() {
  if ! "${DOCKER_BIN}" version >/dev/null 2>&1; then
    fail "Docker is not available or daemon is unreachable via: ${DOCKER_BIN}"
  fi
}

require_command "${DOCKER_BIN}"
require_command gunzip
require_file "${OUTPUT_FILE}"
require_docker_daemon

echo "[INFO] Loading image archive: ${OUTPUT_FILE}"
gunzip -c "${OUTPUT_FILE}" | "${DOCKER_BIN}" load
