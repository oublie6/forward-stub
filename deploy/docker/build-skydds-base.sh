#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

echo "[INFO] build-skydds-base.sh is kept as a compatibility wrapper."
echo "[INFO] Redirecting to deploy/docker/build-and-save-skydds-base.sh"

exec "${SCRIPT_DIR}/build-and-save-skydds-base.sh"
