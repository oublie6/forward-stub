#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKERFILE="${ROOT_DIR}/deploy/docker/skydds-runtime/Dockerfile"
IMAGES_DIR="${ROOT_DIR}/deploy/images"
PKG_DIR="${ROOT_DIR}/third_party/skydds/packages"
SDK_DIR="${ROOT_DIR}/third_party/skydds/sdk"
DOCKER_BIN="${DOCKER_BIN:-docker}"
BASE_IMAGE_TAG="${BASE_IMAGE_TAG:-forward-stub-skydds-base:bookworm}"
IMAGE_TAG="${IMAGE_TAG:-forward-stub-skydds-runtime:bookworm}"
OUTPUT_FILE="${OUTPUT_FILE:-${IMAGES_DIR}/forward-stub-skydds-runtime-bookworm.tar.gz}"

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

require_nonempty_dir() {
  local path="$1"
  if ! find "${path}" -mindepth 1 ! -name '.gitkeep' -print -quit | grep -q .; then
    fail "Directory exists but has no usable content: ${path}"
  fi
}

require_docker_daemon() {
  if ! "${DOCKER_BIN}" version >/dev/null 2>&1; then
    fail "Docker is not available or daemon is unreachable via: ${DOCKER_BIN}"
  fi
}

require_local_image() {
  local image="$1"
  if ! "${DOCKER_BIN}" image inspect "${image}" >/dev/null 2>&1; then
    fail "Base image not found locally: ${image}. Build and load it first."
  fi
}

require_command "${DOCKER_BIN}"
require_command gzip
require_dir "${PKG_DIR}"
require_dir "${SDK_DIR}"
require_nonempty_dir "${PKG_DIR}"
require_nonempty_dir "${SDK_DIR}"
require_file "${DOCKERFILE}"
require_docker_daemon
require_local_image "${BASE_IMAGE_TAG}"

mkdir -p "${IMAGES_DIR}"
mkdir -p "$(dirname "${OUTPUT_FILE}")"

echo "[INFO] Building SkyDDS runtime image"
echo "[INFO] Dockerfile: ${DOCKERFILE}"
echo "[INFO] Base image: ${BASE_IMAGE_TAG}"
echo "[INFO] Image tag: ${IMAGE_TAG}"
echo "[INFO] Output archive: ${OUTPUT_FILE}"

"${DOCKER_BIN}" build \
  -f "${DOCKERFILE}" \
  --build-arg "SKYDDS_BASE_IMAGE=${BASE_IMAGE_TAG}" \
  -t "${IMAGE_TAG}" \
  "${ROOT_DIR}"

tmp_tar=$(mktemp "${TMPDIR:-/tmp}/skydds-runtime.XXXXXX.tar")
cleanup() {
  rm -f "${tmp_tar}"
}
trap cleanup EXIT

echo "[INFO] Saving image to temporary tar: ${tmp_tar}"
"${DOCKER_BIN}" save -o "${tmp_tar}" "${IMAGE_TAG}"

echo "[INFO] Compressing archive to: ${OUTPUT_FILE}"
gzip -c "${tmp_tar}" > "${OUTPUT_FILE}"

echo "[INFO] Done: ${OUTPUT_FILE}"
