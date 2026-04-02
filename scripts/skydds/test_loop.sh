#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "${ROOT_DIR}"
source scripts/skydds/env.sh

CGO_ENABLED=1 go build -tags skydds -o bin/forward-stub .

echo "[INFO] loop prerequisites:"
echo "  1) SkyDDS external publisher publishes topic pdxpInputTopic (domain 0, OctetMsg)."
echo "  2) SkyDDS external subscriber subscribes topic pdxpOutputTopic (domain 0, OctetMsg)."
echo "  3) then run command below in another terminal:"
echo "./bin/forward-stub -system-config ./configs/minimal.system.example.json -business-config ./configs/skydds.business.example.json"
