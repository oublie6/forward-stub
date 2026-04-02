#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${ROOT_DIR}"
source scripts/skydds/env.sh

CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .

echo "[INFO] loop prerequisites (octet):"
echo "  1) external SkyDDS publisher -> topic pdxpInputTopic (OctetMsg)"
echo "  2) external SkyDDS subscriber <- topic pdxpOutputTopic (OctetMsg)"
echo "  3) run: ./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds.business.example.json"

echo "[INFO] loop prerequisites (batch_octet):"
echo "  1) external SkyDDS batch publisher -> topic pdxpBatchInputTopic (BatchOctetMsg)"
echo "  2) external SkyDDS batch subscriber <- topic pdxpBatchOutputTopic (BatchOctetMsg)"
echo "  3) run: ./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds-batch.business.example.json"
