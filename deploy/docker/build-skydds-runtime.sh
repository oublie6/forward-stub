#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKERFILE="${ROOT_DIR}/deploy/docker/skydds-runtime/Dockerfile"
PKG_DIR="${ROOT_DIR}/third_party/skydds/packages"
SDK_DIR="${ROOT_DIR}/third_party/skydds/sdk"
DOCKER_BIN="${DOCKER_BIN:-docker}"
BASE_IMAGE_TAG="${BASE_IMAGE_TAG:-forward-stub-skydds-base:bookworm}"
IMAGE_TAG="${IMAGE_TAG:-forward-stub-skydds-runtime:bookworm}"

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

require_docker_daemon() {
  if ! "${DOCKER_BIN}" version >/dev/null 2>&1; then
    fail "Docker is not available or daemon is unreachable via: ${DOCKER_BIN}"
  fi
}

require_dir() {
  local path="$1"
  if [[ ! -d "${path}" ]]; then
    fail "Required directory not found: ${path}"
  fi
}

require_nonempty_dir() {
  local path="$1"
  if ! find "${path}" -mindepth 1 ! -name '.gitkeep' -print -quit | grep -q .; then
    fail "Directory exists but has no usable content: ${path}"
  fi
}

require_command "${DOCKER_BIN}"
require_docker_daemon
require_dir "${PKG_DIR}"
require_dir "${SDK_DIR}"
require_nonempty_dir "${PKG_DIR}"
require_nonempty_dir "${SDK_DIR}"

if [[ ! -f "${DOCKERFILE}" ]]; then
  fail "Dockerfile not found: ${DOCKERFILE}"
fi

if ! "${DOCKER_BIN}" image inspect "${BASE_IMAGE_TAG}" >/dev/null 2>&1; then
  fail "Base image not found locally: ${BASE_IMAGE_TAG}. Build it first with deploy/docker/build-skydds-base.sh"
fi

echo "[INFO] Building SkyDDS runtime image"
echo "[INFO] Dockerfile: ${DOCKERFILE}"
echo "[INFO] Base image: ${BASE_IMAGE_TAG}"
echo "[INFO] Image tag: ${IMAGE_TAG}"

"${DOCKER_BIN}" build \
  -f "${DOCKERFILE}" \
  --build-arg "SKYDDS_BASE_IMAGE=${BASE_IMAGE_TAG}" \
  -t "${IMAGE_TAG}" \
  "${ROOT_DIR}"
