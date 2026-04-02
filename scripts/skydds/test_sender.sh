#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${ROOT_DIR}"
source scripts/skydds/env.sh

echo "[INFO] building with SkyDDS tag"
CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .

echo "[INFO] running config validation for SkyDDS sender"
go test ./src/config -run SkyDDS -count=1

echo "[INFO] sender path smoke command (requires a running SkyDDS subscriber)"
echo "./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds.business.example.json"
