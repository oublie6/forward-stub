FROM golang:1.25-bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /workspace

# 必须先拷贝整个仓库（包含 third_party/skydds/packages/）。
COPY . .

# 解压 SkyDDS 安装包到 third_party/skydds/sdk，再编译 skydds 版本二进制。
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

ENTRYPOINT ["/workspace/bin/forward-stub"]
