#!/usr/bin/env bash
set -euo pipefail

APP_NAME=${APP_NAME:-forword-stub}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
GOOS=${GOOS:-linux}
GOARCH=${GOARCH:-amd64}
MAIN_PKG=${MAIN_PKG:-.}
OUT_DIR=${OUT_DIR:-dist/linux}
BINARY_NAME=${BINARY_NAME:-${APP_NAME}}

LDFLAGS=${LDFLAGS:-"-s -w -X main.version=${VERSION}"}

mkdir -p "${OUT_DIR}"
OUT_FILE="${OUT_DIR}/${BINARY_NAME}"

echo "Building ${APP_NAME} (${GOOS}/${GOARCH}) version=${VERSION}"
CGO_ENABLED=0 GOOS="${GOOS}" GOARCH="${GOARCH}" \
  go build -trimpath -ldflags "${LDFLAGS}" -o "${OUT_FILE}" "${MAIN_PKG}"

chmod +x "${OUT_FILE}"
echo "Binary generated: ${OUT_FILE}"
