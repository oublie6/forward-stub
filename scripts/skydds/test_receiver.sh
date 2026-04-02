#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${ROOT_DIR}"
source scripts/skydds/env.sh

echo "[INFO] building with SkyDDS tag"
CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .

echo "[INFO] config/unit checks for octet + batch"
go test ./src/config -run SkyDDS -count=1
go test ./src/receiver -run SkyDDS -count=1

echo "[INFO] octet receiver smoke command"
echo "./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds.business.example.json"

echo "[INFO] batch_octet receiver smoke command"
echo "./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds-batch.business.example.json"
