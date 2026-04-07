# Intentionally build the runtime image on top of the local arm64-only base tag.
FROM --platform=linux/arm64 forward-stub-base:bookworm-arm64

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
WORKDIR /workspace

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy

# 必须先拷贝整个仓库（包含 third_party/skydds/packages/）。
COPY . .

RUN set -eux; \
    rm -rf /workspace/third_party/skydds/sdk/*; \
    /workspace/scripts/skydds/setup_linux.sh; \
    source /workspace/scripts/skydds/env.sh; \
    CGO_ENABLED=1 GOFLAGS=-mod=vendor go build -trimpath -tags skydds -o /workspace/bin/forward-stub .

ENV SKY_DDS=/workspace/third_party/skydds/sdk \
    DDS_ROOT=/workspace/third_party/skydds/sdk/DDS \
    ACE_ROOT=/workspace/third_party/skydds/sdk/ACE_wrappers \
    TAO_ROOT=/workspace/third_party/skydds/sdk/ACE_wrappers/TAO \
    LD_LIBRARY_PATH=/workspace/third_party/skydds/sdk/DDS/lib:/workspace/third_party/skydds/sdk/ACE_wrappers/lib

ENTRYPOINT ["/workspace/bin/forward-stub"]
