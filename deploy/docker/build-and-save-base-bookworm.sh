#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKERFILE="${ROOT_DIR}/deploy/docker/base-bookworm/Dockerfile"
BUILD_CONTEXT="${ROOT_DIR}/deploy/docker/base-bookworm"
IMAGES_DIR="${ROOT_DIR}/deploy/images"
DOCKER_BIN="${DOCKER_BIN:-docker}"
IMAGE_TAG="${IMAGE_TAG:-forward-stub-base:bookworm}"
OUTPUT_FILE="${OUTPUT_FILE:-${IMAGES_DIR}/forward-stub-base-bookworm.tar.gz}"

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

require_dir() {
  local path="$1"
  if [[ ! -d "${path}" ]]; then
    fail "Required directory not found: ${path}"
  fi
}

require_file() {
  local path="$1"
  if [[ ! -f "${path}" ]]; then
    fail "Required file not found: ${path}"
  fi
}

require_docker_daemon() {
  if ! "${DOCKER_BIN}" version >/dev/null 2>&1; then
    fail "Docker is not available or daemon is unreachable via: ${DOCKER_BIN}"
  fi
}

require_command "${DOCKER_BIN}"
require_command gzip
require_file "${DOCKERFILE}"
require_dir "${BUILD_CONTEXT}"
require_docker_daemon

mkdir -p "${IMAGES_DIR}"
mkdir -p "$(dirname "${OUTPUT_FILE}")"

tmp_tar=$(mktemp "${TMPDIR:-/tmp}/forward-stub-base-bookworm.XXXXXX.tar")
cleanup() {
  rm -f "${tmp_tar}"
}
trap cleanup EXIT

echo "[INFO] Building Bookworm base image"
echo "[INFO] Dockerfile: ${DOCKERFILE}"
echo "[INFO] Build context: ${BUILD_CONTEXT}"
echo "[INFO] Image tag: ${IMAGE_TAG}"
echo "[INFO] Output archive: ${OUTPUT_FILE}"

"${DOCKER_BIN}" build \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_TAG}" \
  "${BUILD_CONTEXT}"

echo "[INFO] Saving image to temporary tar: ${tmp_tar}"
"${DOCKER_BIN}" save -o "${tmp_tar}" "${IMAGE_TAG}"

echo "[INFO] Compressing archive to: ${OUTPUT_FILE}"
gzip -c "${tmp_tar}" > "${OUTPUT_FILE}"

echo "[INFO] Done: ${OUTPUT_FILE}"
