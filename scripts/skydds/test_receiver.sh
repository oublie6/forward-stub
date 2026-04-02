#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${ROOT_DIR}"
source scripts/skydds/env.sh

CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .
go test ./src/config -run SkyDDS -count=1

echo "[INFO] receiver path smoke command (requires a running SkyDDS publisher)"
echo "./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds.business.example.json"
