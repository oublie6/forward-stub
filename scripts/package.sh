#!/usr/bin/env bash
# package.sh 用于跨平台打包发布产物：
# 1) 根据 TARGETS 循环编译目标平台二进制；
# 2) 将 README 与示例配置一并打入压缩包；
# 3) 统一输出到 OUT_DIR 便于 CI/CD 归档。
set -euo pipefail

# APP_NAME: 产物名称（同时影响可执行文件名与压缩包前缀）。
APP_NAME=${APP_NAME:-forward-stub}
# VERSION: 默认读取 git describe，若仓库无 tag 则回退 dev。
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
# OUT_DIR: 打包输出目录。
OUT_DIR=${OUT_DIR:-dist}
# TARGETS: 目标平台列表，格式为 "GOOS/GOARCH"，可配置多个目标。
TARGETS=${TARGETS:-linux/arm64}
# MAIN_PKG: go build 的主包路径，默认当前目录。
MAIN_PKG=${MAIN_PKG:-.}
# GOFLAGS: 默认强制使用 vendor 目录，离线构建更稳定。
GOFLAGS=${GOFLAGS:--mod=vendor}

# 统一裁剪符号信息，减小二进制体积。
LDFLAGS="-s -w"

mkdir -p "${OUT_DIR}"
# 记录绝对路径，避免进入临时目录后输出位置变化。
OUT_ABS=$(cd "${OUT_DIR}" && pwd)

echo "Packaging ${APP_NAME} version=${VERSION}"
for target in ${TARGETS}; do
  # 从 GOOS/GOARCH 字符串拆分出平台参数。
  GOOS=${target%/*}
  GOARCH=${target#*/}
  # 每个平台在独立临时目录构建，避免文件互相污染。
  WORK_DIR=$(mktemp -d)
  BIN_NAME=${APP_NAME}
  if [[ "${GOOS}" == "windows" ]]; then
    BIN_NAME="${APP_NAME}.exe"
  fi

  echo "-> building ${GOOS}/${GOARCH}"
  # 关闭 CGO 以提升可移植性，产出静态友好的发布二进制。
  CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
    GOFLAGS="${GOFLAGS}" go build -trimpath -ldflags "${LDFLAGS}" -o "${WORK_DIR}/${BIN_NAME}" "${MAIN_PKG}"

  # 附带基础文档和示例配置，降低使用方上手成本。
  cp -f README.md "${WORK_DIR}/"
  mkdir -p "${WORK_DIR}/configs"
  cp -f configs/system.example.json "${WORK_DIR}/configs/"
  cp -f configs/business.example.json "${WORK_DIR}/configs/"
  cp -f configs/example.json "${WORK_DIR}/configs/"

  ARCHIVE_BASE="${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}"
  # Windows 生态优先 zip，其他平台默认 tar.gz。
  if [[ "${GOOS}" == "windows" ]]; then
    (cd "${WORK_DIR}" && zip -qr "${OUT_ABS}/${ARCHIVE_BASE}.zip" .)
  else
    tar -C "${WORK_DIR}" -czf "${OUT_ABS}/${ARCHIVE_BASE}.tar.gz" .
  fi
  rm -rf "${WORK_DIR}"
  echo "   done: ${OUT_DIR}/${ARCHIVE_BASE}"
done

echo "All packages generated under ${OUT_DIR}/"
