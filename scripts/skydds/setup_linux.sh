#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PKG_DIR="${ROOT_DIR}/third_party/skydds/packages"
SDK_DIR="${ROOT_DIR}/third_party/skydds/sdk"

mkdir -p "${PKG_DIR}" "${SDK_DIR}"

if find "${SDK_DIR}" -mindepth 1 ! -name .gitkeep -print -quit | grep -q .; then
  echo "[INFO] SDK directory already has content: ${SDK_DIR}"
else
  shopt -s nullglob
  archives=("${PKG_DIR}"/*.tar.gz)
  shopt -u nullglob
  if [[ ${#archives[@]} -eq 0 ]]; then
    echo "[ERROR] No SkyDDS package found in ${PKG_DIR}" >&2
    echo "Please copy SkyDDS Linux .tar.gz package into third_party/skydds/packages/ first." >&2
    exit 1
  fi
  pkg="${archives[0]}"
  echo "[INFO] Extracting package: ${pkg}"
  tar -xzf "${pkg}" -C "${SDK_DIR}" --strip-components=1
fi

if [[ -f "${SDK_DIR}/lib/libhiredis.so.1.2.1-dev" && ! -e "${SDK_DIR}/lib/libhiredis.so" ]]; then
  ln -s libhiredis.so.1.2.1-dev "${SDK_DIR}/lib/libhiredis.so"
fi

echo "[INFO] Setup done. Next step:"
echo "  source scripts/skydds/env.sh"
