#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKERFILE="${ROOT_DIR}/deploy/docker/skydds-bookworm/Dockerfile"
BUILD_CONTEXT="${ROOT_DIR}"
IMAGES_DIR="${ROOT_DIR}/deploy/images"
PACKAGES_DIR="${ROOT_DIR}/third_party/skydds/packages"
DOCKER_BIN="${DOCKER_BIN:-docker}"
PLATFORM="${PLATFORM:-linux/arm64}"
IMAGE_TAG="${IMAGE_TAG:-forward-stub:skydds-bookworm}"
OUTPUT_FILE="${OUTPUT_FILE:-${IMAGES_DIR}/forward-stub-skydds-bookworm.tar.gz}"

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

require_buildx() {
  if ! "${DOCKER_BIN}" buildx version >/dev/null 2>&1; then
    fail "Docker Buildx is required to build ${PLATFORM} images via: ${DOCKER_BIN} buildx"
  fi
}

require_skydds_package() {
  shopt -s nullglob
  local archives=("${PACKAGES_DIR}"/*.tar.gz "${PACKAGES_DIR}"/*.tgz "${PACKAGES_DIR}"/*.tar.xz "${PACKAGES_DIR}"/*.zip)
  shopt -u nullglob
  if [[ ${#archives[@]} -eq 0 ]]; then
    fail "No SkyDDS package found in ${PACKAGES_DIR}"
  fi
}

require_command "${DOCKER_BIN}"
require_command gzip
require_file "${DOCKERFILE}"
require_dir "${BUILD_CONTEXT}"
require_dir "${PACKAGES_DIR}"
require_skydds_package
require_docker_daemon
require_buildx

mkdir -p "${IMAGES_DIR}"
mkdir -p "$(dirname "${OUTPUT_FILE}")"

tmp_tar=$(mktemp "${TMPDIR:-/tmp}/forward-stub-skydds-bookworm.XXXXXX.tar")
cleanup() {
  rm -f "${tmp_tar}"
}
trap cleanup EXIT

echo "[INFO] Building SkyDDS service image (Bookworm)"
echo "[INFO] Dockerfile: ${DOCKERFILE}"
echo "[INFO] Build context: ${BUILD_CONTEXT}"
echo "[INFO] Target platform: ${PLATFORM} (aarch64 == linux/arm64)"
echo "[INFO] Image tag: ${IMAGE_TAG}"
echo "[INFO] Output archive: ${OUTPUT_FILE}"

"${DOCKER_BIN}" buildx build \
  --platform "${PLATFORM}" \
  --load \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_TAG}" \
  "${BUILD_CONTEXT}"

echo "[INFO] Saving image to temporary tar: ${tmp_tar}"
"${DOCKER_BIN}" save -o "${tmp_tar}" "${IMAGE_TAG}"

echo "[INFO] Compressing archive to: ${OUTPUT_FILE}"
gzip -c "${tmp_tar}" > "${OUTPUT_FILE}"

echo "[INFO] Done: ${OUTPUT_FILE}"
