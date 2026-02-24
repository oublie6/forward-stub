#!/usr/bin/env bash
set -euo pipefail

APP_NAME=${APP_NAME:-forword-stub}
VERSION=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
OUT_DIR=${OUT_DIR:-dist}
TARGETS=${TARGETS:-linux/amd64}
MAIN_PKG=${MAIN_PKG:-.}

LDFLAGS="-s -w -X main.version=${VERSION}"

mkdir -p "${OUT_DIR}"
OUT_ABS=$(cd "${OUT_DIR}" && pwd)

echo "Packaging ${APP_NAME} version=${VERSION}"
for target in ${TARGETS}; do
  GOOS=${target%/*}
  GOARCH=${target#*/}
  WORK_DIR=$(mktemp -d)
  BIN_NAME=${APP_NAME}
  if [[ "${GOOS}" == "windows" ]]; then
    BIN_NAME="${APP_NAME}.exe"
  fi

  echo "-> building ${GOOS}/${GOARCH}"
  CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} \
    go build -trimpath -ldflags "${LDFLAGS}" -o "${WORK_DIR}/${BIN_NAME}" "${MAIN_PKG}"

  cp -f README.md "${WORK_DIR}/"
  mkdir -p "${WORK_DIR}/configs"
  cp -f configs/example.json "${WORK_DIR}/configs/"

  ARCHIVE_BASE="${APP_NAME}_${VERSION}_${GOOS}_${GOARCH}"
  if [[ "${GOOS}" == "windows" ]]; then
    (cd "${WORK_DIR}" && zip -qr "${OUT_ABS}/${ARCHIVE_BASE}.zip" .)
  else
    tar -C "${WORK_DIR}" -czf "${OUT_ABS}/${ARCHIVE_BASE}.tar.gz" .
  fi
  rm -rf "${WORK_DIR}"
  echo "   done: ${OUT_DIR}/${ARCHIVE_BASE}"
done

echo "All packages generated under ${OUT_DIR}/"
