FROM golang:1.25-bookworm AS builder

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
WORKDIR /workspace

# 必须先拷贝整个仓库（包含 third_party/skydds/packages/）。
COPY . .

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      file \
      g++ \
      gcc \
      make \
      perl \
      tar \
      unzip \
      xz-utils \
      libstdc++6; \
    rm -rf /var/lib/apt/lists/*; \
    rm -rf /workspace/third_party/skydds/sdk/*; \
    /workspace/scripts/skydds/setup_linux.sh; \
    source /workspace/scripts/skydds/env.sh; \
    CGO_ENABLED=1 GOFLAGS=-mod=vendor go build -trimpath -tags skydds -o /workspace/bin/forward-stub .

FROM debian:bookworm-slim

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
WORKDIR /app

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      libstdc++6; \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /workspace/bin/forward-stub /app/forward-stub
COPY --from=builder /workspace/third_party/skydds/sdk /opt/skydds/sdk

ENV SKY_DDS=/opt/skydds/sdk \
    DDS_ROOT=/opt/skydds/sdk/DDS \
    ACE_ROOT=/opt/skydds/sdk/ACE_wrappers \
    TAO_ROOT=/opt/skydds/sdk/ACE_wrappers/TAO \
    LD_LIBRARY_PATH=/opt/skydds/sdk/DDS/lib:/opt/skydds/sdk/ACE_wrappers/lib

ENTRYPOINT ["/app/forward-stub"]
