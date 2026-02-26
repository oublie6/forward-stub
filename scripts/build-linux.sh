#!/usr/bin/env bash
# build-linux.sh 用于快速生成 Linux 运行二进制：
# - 常用于 Docker 镜像构建前的宿主机构建步骤；
# - 也可在 CI 中作为统一的 Linux 制品构建入口。
set -euo pipefail

# APP_NAME: 应用名，仅用于默认二进制命名。
APP_NAME=${APP_NAME:-forward-stub}
# VERSION: 注入 main.version 的版本号。
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
# GOOS/GOARCH: 默认构建 linux/arm64（aarch64），可按需覆盖。
GOOS=${GOOS:-linux}
GOARCH=${GOARCH:-arm64}
# MAIN_PKG: 主包路径。
MAIN_PKG=${MAIN_PKG:-.}
# GOFLAGS: 默认强制使用 vendor 目录，离线构建更稳定。
GOFLAGS=${GOFLAGS:--mod=vendor}
# OUT_DIR: 输出目录。
OUT_DIR=${OUT_DIR:-dist/linux}
# BINARY_NAME: 输出文件名，默认等于 APP_NAME。
BINARY_NAME=${BINARY_NAME:-${APP_NAME}}

# 默认优化参数：去符号并注入版本信息。
LDFLAGS=${LDFLAGS:-"-s -w -X main.version=${VERSION}"}

mkdir -p "${OUT_DIR}"
OUT_FILE="${OUT_DIR}/${BINARY_NAME}"

echo "Building ${APP_NAME} (${GOOS}/${GOARCH}) version=${VERSION}"
# 关闭 CGO 让二进制更易部署到精简运行时镜像（如 distroless）。
CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
  GOFLAGS="${GOFLAGS}" go build -trimpath -ldflags "${LDFLAGS}" -o "${OUT_FILE}" "${MAIN_PKG}"

# 确保输出文件可执行。
chmod +x "${OUT_FILE}"
echo "Binary generated: ${OUT_FILE}"
