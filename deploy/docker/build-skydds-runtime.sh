#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

echo "[INFO] build-skydds-runtime.sh is kept as a compatibility wrapper."
echo "[INFO] Redirecting to deploy/docker/build-and-save-skydds-runtime.sh"

exec "${SCRIPT_DIR}/build-and-save-skydds-runtime.sh"
