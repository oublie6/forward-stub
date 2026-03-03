#!/usr/bin/env bash
set -euo pipefail

# 在受限环境中启动独立 dockerd（不依赖 systemd），并完成：
# 1) 拉取 Dockerfile 基础镜像
# 2) 导出到项目目录 docker/base-images
# 3) 执行镜像构建验证

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
DOCKER_HOST_URI=${DOCKER_HOST_URI:-unix:///tmp/forward-stub-docker.sock}
CONTAINERD_SOCK=${CONTAINERD_SOCK:-/tmp/forward-stub-containerd.sock}
PRELOADED_IMAGE_DIR=${PRELOADED_IMAGE_DIR:-${ROOT_DIR}/deploy/images}

BUILDER_IMAGE=${BUILDER_IMAGE:-golang:1.25-alpine}
VERSION=${VERSION:-dev}
TARGETOS=${TARGETOS:-linux}
TARGETARCH=${TARGETARCH:-arm64}
PULL_RETRIES=${PULL_RETRIES:-3}
PULL_RETRY_INTERVAL_SEC=${PULL_RETRY_INTERVAL_SEC:-3}
# 支持配置多个镜像源（逗号分隔），例如：
# IMAGE_MIRRORS="docker.m.daocloud.io,mirror.ccs.tencentyun.com"
IMAGE_MIRRORS=${IMAGE_MIRRORS:-}

mkdir -p "${ROOT_DIR}/docker/base-images"
mkdir -p /tmp/forward-stub-containerd-root /tmp/forward-stub-containerd-state /tmp/forward-stub-docker-data /tmp/forward-stub-docker-exec

# 清理上一次残留
rm -f /tmp/forward-stub-dockerd.pid /tmp/forward-stub-containerd.sock /tmp/forward-stub-docker.sock
if [[ -f /tmp/forward-stub-containerd.pid ]]; then
  kill "$(cat /tmp/forward-stub-containerd.pid)" >/dev/null 2>&1 || true
fi

cleanup() {
  if [[ -f /tmp/forward-stub-dockerd.pid ]]; then
    kill "$(cat /tmp/forward-stub-dockerd.pid)" >/dev/null 2>&1 || true
  fi
  if [[ -f /tmp/forward-stub-containerd.pid ]]; then
    kill "$(cat /tmp/forward-stub-containerd.pid)" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

trim() {
  local s=$1
  # shellcheck disable=SC2001
  echo "$(echo "${s}" | sed 's/^ *//;s/ *$//')"
}

# docker 镜像名是否包含 registry 前缀。
has_registry_prefix() {
  local img=$1
  if [[ "${img}" != */* ]]; then
    return 1
  fi
  local first=${img%%/*}
  [[ "${first}" == *.* || "${first}" == *:* || "${first}" == "localhost" ]]
}

# 将原镜像重写到指定镜像源。
rewrite_image_for_mirror() {
  local image=$1
  local mirror=$2
  if has_registry_prefix "${image}"; then
    echo "${mirror}/${image#*/}"
  else
    echo "${mirror}/library/${image}"
  fi
}

# 构建拉取候选列表（原图 + 多镜像源重写）。
build_pull_candidates() {
  local image=$1
  local -a out
  out+=("${image}")

  if [[ -n "${IMAGE_MIRRORS}" ]]; then
    IFS=',' read -r -a mirrors <<<"${IMAGE_MIRRORS}"
    for m in "${mirrors[@]}"; do
      m=$(trim "${m}")
      [[ -z "${m}" ]] && continue
      out+=("$(rewrite_image_for_mirror "${image}" "${m}")")
    done
  fi

  printf '%s\n' "${out[@]}"
}

pull_with_retries() {
  local canonical_image=$1
  local -a candidates
  local candidate
  local attempt

  mapfile -t candidates < <(build_pull_candidates "${canonical_image}")

  echo "[pull] 镜像 ${canonical_image}，候选源：${candidates[*]}"
  for candidate in "${candidates[@]}"; do
    for attempt in $(seq 1 "${PULL_RETRIES}"); do
      echo "[pull] 尝试 ${attempt}/${PULL_RETRIES}: ${candidate}"
      if DOCKER_HOST="${DOCKER_HOST_URI}" docker pull "${candidate}"; then
        if [[ "${candidate}" != "${canonical_image}" ]]; then
          DOCKER_HOST="${DOCKER_HOST_URI}" docker tag "${candidate}" "${canonical_image}"
        fi
        echo "[pull] 成功: ${canonical_image} <= ${candidate}"
        return 0
      fi

      if [[ "${attempt}" -lt "${PULL_RETRIES}" ]]; then
        sleep "${PULL_RETRY_INTERVAL_SEC}"
      fi
    done
  done

  echo "[pull] 失败: ${canonical_image}（所有镜像源均重试后失败）"
  return 1
}

image_exists_locally() {
  local image=$1
  DOCKER_HOST="${DOCKER_HOST_URI}" docker image inspect "${image}" >/dev/null 2>&1
}

# 一些离线 tar 包内镜像标签可能带 docker.io 前缀，这里做一次归一化打标。
normalize_repo_tag() {
  local image=$1
  if [[ "${image}" == docker.io/library/* ]]; then
    echo "${image#docker.io/library/}"
    return 0
  fi
  if [[ "${image}" == docker.io/* ]]; then
    echo "${image#docker.io/}"
    return 0
  fi
  return 1
}

load_preloaded_images() {
  local image_dir=$1
  local file
  local loaded_tags
  local tag
  local normalized

  if [[ ! -d "${image_dir}" ]]; then
    echo "[preload] 跳过：目录不存在 ${image_dir}"
    return 0
  fi

  shopt -s nullglob
  for file in "${image_dir}"/*.tar; do
    echo "[preload] 加载镜像包: ${file}"
    DOCKER_HOST="${DOCKER_HOST_URI}" docker load -i "${file}"

    # 基于 tar manifest 获取 RepoTags 并补充归一化标签。
    loaded_tags=$(tar -xOf "${file}" manifest.json 2>/dev/null \
      | tr '{},[]' '\n' \
      | sed -n 's/.*"RepoTags":\[\(.*\)\].*/\1/p' \
      | tr ',' '\n' \
      | sed -e 's/"//g' -e 's/^ *//' -e 's/ *$//' \
      | sed '/^$/d' || true)

    while IFS= read -r tag; do
      [[ -z "${tag}" ]] && continue
      if normalized=$(normalize_repo_tag "${tag}"); then
        DOCKER_HOST="${DOCKER_HOST_URI}" docker tag "${tag}" "${normalized}"
      fi
    done <<<"${loaded_tags}"
  done
  shopt -u nullglob
}

nohup containerd \
  --address "${CONTAINERD_SOCK}" \
  --root /tmp/forward-stub-containerd-root \
  --state /tmp/forward-stub-containerd-state \
  >/tmp/forward-stub-containerd.log 2>&1 &
echo $! >/tmp/forward-stub-containerd.pid

nohup dockerd \
  --host "${DOCKER_HOST_URI}" \
  --containerd "${CONTAINERD_SOCK}" \
  --data-root /tmp/forward-stub-docker-data \
  --exec-root /tmp/forward-stub-docker-exec \
  --pidfile /tmp/forward-stub-dockerd.pid \
  --storage-driver=vfs \
  --iptables=false \
  --bridge=none \
  --ip-forward=false \
  --ip-masq=false \
  >/tmp/forward-stub-dockerd.log 2>&1 &

for i in $(seq 1 40); do
  if DOCKER_HOST="${DOCKER_HOST_URI}" docker info >/dev/null 2>&1; then
    break
  fi
  sleep 1
  if [[ "$i" == "40" ]]; then
    echo "dockerd 启动失败，请检查 /tmp/forward-stub-dockerd.log"
    exit 1
  fi
done

load_preloaded_images "${PRELOADED_IMAGE_DIR}"

if image_exists_locally "${BUILDER_IMAGE}"; then
  echo "[pull] 已存在本地镜像，跳过拉取: ${BUILDER_IMAGE}"
else
  pull_with_retries "${BUILDER_IMAGE}"
fi

DOCKER_HOST="${DOCKER_HOST_URI}" docker save -o "${ROOT_DIR}/docker/base-images/${BUILDER_IMAGE//[\/:]/_}.tar" "${BUILDER_IMAGE}"

DOCKER_HOST="${DOCKER_HOST_URI}" docker build -t forward-stub:test --build-arg VERSION="${VERSION}" --build-arg TARGETOS="${TARGETOS}" --build-arg TARGETARCH="${TARGETARCH}" "${ROOT_DIR}"
DOCKER_HOST="${DOCKER_HOST_URI}" docker image inspect forward-stub:test --format '{{.Id}} {{.Architecture}} {{.Os}}'

ls -lh "${ROOT_DIR}/docker/base-images"
