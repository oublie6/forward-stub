FROM forward-stub-base:bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
WORKDIR /workspace

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
