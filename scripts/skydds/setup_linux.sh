#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
PKG_DIR="${ROOT_DIR}/third_party/skydds/packages"
SDK_DIR="${ROOT_DIR}/third_party/skydds/sdk"

mkdir -p "${PKG_DIR}" "${SDK_DIR}"

if compgen -G "${SDK_DIR}/*" >/dev/null; then
  echo "[INFO] SDK directory already has content: ${SDK_DIR}"
else
  shopt -s nullglob
  archives=("${PKG_DIR}"/*.tar.gz "${PKG_DIR}"/*.tgz "${PKG_DIR}"/*.tar.xz "${PKG_DIR}"/*.zip)
  shopt -u nullglob
  if [[ ${#archives[@]} -eq 0 ]]; then
    echo "[ERROR] No SkyDDS package found in ${PKG_DIR}" >&2
    echo "Please copy SkyDDS Linux package into third_party/skydds/packages/ first." >&2
    exit 1
  fi
  pkg="${archives[0]}"
  echo "[INFO] Extracting package: ${pkg}"
  case "${pkg}" in
    *.tar.gz|*.tgz) tar -xzf "${pkg}" -C "${SDK_DIR}" --strip-components=1 ;;
    *.tar.xz) tar -xJf "${pkg}" -C "${SDK_DIR}" --strip-components=1 ;;
    *.zip) unzip -q "${pkg}" -d "${SDK_DIR}" ;;
    *) echo "[ERROR] Unsupported archive type: ${pkg}" >&2; exit 1 ;;
  esac
fi

echo "[INFO] Setup done. Next step:"
echo "  source scripts/skydds/env.sh"
