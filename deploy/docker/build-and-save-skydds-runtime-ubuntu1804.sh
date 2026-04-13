#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT_DIR=$(cd "${SCRIPT_DIR}/../.." && pwd)
DOCKERFILE="${ROOT_DIR}/deploy/docker/skydds-runtime-ubuntu1804/Dockerfile"
BUILD_CONTEXT="${ROOT_DIR}"
IMAGES_DIR="${ROOT_DIR}/deploy/images"
PACKAGES_DIR="${ROOT_DIR}/third_party/skydds/packages"
DOCKER_BIN="${DOCKER_BIN:-docker}"
BUILDER="${BUILDER:-default}"
PLATFORM="linux/arm64"
IMAGE_TAG="${IMAGE_TAG:-forward-stub:skydds-ubuntu1804-runtime-arm64}"
OUTPUT_FILE="${OUTPUT_FILE:-${IMAGES_DIR}/forward-stub-skydds-ubuntu1804-runtime-arm64.tar.gz}"
BASE_IMAGE="${BASE_IMAGE:-forward-stub-base:ubuntu1804-arm64}"
HOST_HTTP_PROXY="${HOST_HTTP_PROXY:-http://127.0.0.1:7890}"
HOST_HTTPS_PROXY="${HOST_HTTPS_PROXY:-${HOST_HTTP_PROXY}}"
HOST_ALL_PROXY="${HOST_ALL_PROXY:-socks5://127.0.0.1:7891}"
HOST_NO_PROXY="${HOST_NO_PROXY:-127.0.0.1,localhost}"

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

ensure_local_builder() {
  local inspect_output
  if ! inspect_output=$("${DOCKER_BIN}" buildx inspect "${BUILDER}" 2>/dev/null); then
    fail "Docker buildx builder not found: ${BUILDER}"
  fi
  if [[ "${inspect_output}" != *$'\nDriver:        docker'* && "${inspect_output}" != Driver:\ docker* ]]; then
    fail "Builder ${BUILDER} must use the docker driver so the arm64 image is loaded into the local daemon"
  fi
}

bootstrap_builder() {
  "${DOCKER_BIN}" buildx use "${BUILDER}" >/dev/null 2>&1 || true
  "${DOCKER_BIN}" buildx inspect --bootstrap "${BUILDER}" >/dev/null
}

require_skydds_package() {
  shopt -s nullglob
  local archives=("${PACKAGES_DIR}"/*.tar.gz)
  shopt -u nullglob
  if [[ ${#archives[@]} -eq 0 ]]; then
    fail "No SkyDDS .tar.gz package found in ${PACKAGES_DIR}"
  fi
}

require_base_image() {
  if ! "${DOCKER_BIN}" image inspect "${BASE_IMAGE}" >/dev/null 2>&1; then
    fail "Required local base image not found: ${BASE_IMAGE}"
  fi
}

maybe_enable_host_proxy() {
  if exec 9<>/dev/tcp/127.0.0.1/7890; then
    exec 9>&-
    echo "[INFO] Enabling host Clash proxy for build steps via ${HOST_HTTP_PROXY}"
    BUILD_NETWORK_ARGS=(--network host)
    BUILD_PROXY_ARGS=(
      --build-arg "HTTP_PROXY=${HOST_HTTP_PROXY}"
      --build-arg "HTTPS_PROXY=${HOST_HTTPS_PROXY}"
      --build-arg "ALL_PROXY=${HOST_ALL_PROXY}"
      --build-arg "NO_PROXY=${HOST_NO_PROXY}"
      --build-arg "http_proxy=${HOST_HTTP_PROXY}"
      --build-arg "https_proxy=${HOST_HTTPS_PROXY}"
      --build-arg "all_proxy=${HOST_ALL_PROXY}"
      --build-arg "no_proxy=${HOST_NO_PROXY}"
    )
    return
  fi

  echo "[WARN] Host proxy listener 127.0.0.1:7890 is not reachable; building without explicit proxy args"
  BUILD_NETWORK_ARGS=()
  BUILD_PROXY_ARGS=()
}

require_command "${DOCKER_BIN}"
require_command gzip
require_file "${DOCKERFILE}"
require_dir "${BUILD_CONTEXT}"
require_dir "${PACKAGES_DIR}"
require_skydds_package
require_docker_daemon
require_buildx
ensure_local_builder
bootstrap_builder
require_base_image
maybe_enable_host_proxy

mkdir -p "${IMAGES_DIR}"
mkdir -p "$(dirname "${OUTPUT_FILE}")"

tmp_tar=$(mktemp "${TMPDIR:-/tmp}/forward-stub-skydds-ubuntu1804-runtime.XXXXXX.tar")
cleanup() {
  rm -f "${tmp_tar}"
}
trap cleanup EXIT

echo "[INFO] Building SkyDDS runtime image (Ubuntu 18.04)"
echo "[INFO] Dockerfile: ${DOCKERFILE}"
echo "[INFO] Build context: ${BUILD_CONTEXT}"
echo "[INFO] Target platform: ${PLATFORM} (aarch64 == linux/arm64)"
echo "[INFO] Buildx builder: ${BUILDER}"
echo "[INFO] Image tag: ${IMAGE_TAG}"
echo "[INFO] Output archive: ${OUTPUT_FILE}"
echo "[INFO] Base image dependency: ${BASE_IMAGE} must already exist for ${PLATFORM}"

"${DOCKER_BIN}" buildx build \
  --builder "${BUILDER}" \
  --platform "${PLATFORM}" \
  --pull=false \
  --load \
  "${BUILD_NETWORK_ARGS[@]}" \
  "${BUILD_PROXY_ARGS[@]}" \
  -f "${DOCKERFILE}" \
  -t "${IMAGE_TAG}" \
  "${BUILD_CONTEXT}"

echo "[INFO] Inspecting local image: ${IMAGE_TAG}"
"${DOCKER_BIN}" image inspect "${IMAGE_TAG}" --format '{{.Id}} {{.Architecture}}/{{.Os}}'

echo "[INFO] Saving image to temporary tar: ${tmp_tar}"
"${DOCKER_BIN}" save -o "${tmp_tar}" "${IMAGE_TAG}"

echo "[INFO] Compressing archive to: ${OUTPUT_FILE}"
gzip -c "${tmp_tar}" > "${OUTPUT_FILE}"

echo "[INFO] Done: ${OUTPUT_FILE}"
