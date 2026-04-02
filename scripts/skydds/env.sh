#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SDK_DIR="${ROOT_DIR}/third_party/skydds/sdk"

if [[ ! -d "${SDK_DIR}" ]]; then
  echo "[ERROR] SDK directory not found: ${SDK_DIR}" >&2
  echo "Run scripts/skydds/setup_linux.sh first." >&2
  return 1 2>/dev/null || exit 1
fi

export SKY_DDS="${SDK_DIR}"
export DDS_ROOT="${SDK_DIR}/DDS"
export ACE_ROOT="${SDK_DIR}/ACE_wrappers"
export TAO_ROOT="${ACE_ROOT}/TAO"
export LD_LIBRARY_PATH="${DDS_ROOT}/lib:${ACE_ROOT}/lib:${LD_LIBRARY_PATH:-}"

echo "[INFO] SKY_DDS=${SKY_DDS}"
echo "[INFO] DDS_ROOT=${DDS_ROOT}"
echo "[INFO] ACE_ROOT=${ACE_ROOT}"
echo "[INFO] TAO_ROOT=${TAO_ROOT}"
echo "[INFO] LD_LIBRARY_PATH updated"
