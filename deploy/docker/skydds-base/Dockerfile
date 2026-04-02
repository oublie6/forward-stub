FROM golang:1.25-bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /workspace

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      file \
      gcc \
      g++ \
      gzip \
      libstdc++6 \
      make \
      perl \
      tar \
      tcpdump \
      xz-utils; \
    rm -rf /var/lib/apt/lists/*

RUN mkdir -p \
    /workspace/third_party/skydds/packages \
    /workspace/third_party/skydds/sdk

COPY third_party/skydds/packages/ /workspace/third_party/skydds/packages/
COPY third_party/skydds/sdk/ /workspace/third_party/skydds/sdk/

ENV SKY_DDS=/workspace/third_party/skydds/sdk
ENV DDS_ROOT=/workspace/third_party/skydds/sdk/DDS
ENV ACE_ROOT=/workspace/third_party/skydds/sdk/ACE_wrappers
ENV TAO_ROOT=/workspace/third_party/skydds/sdk/ACE_wrappers/TAO
ENV LD_LIBRARY_PATH=/workspace/third_party/skydds/sdk/lib:/workspace/third_party/skydds/sdk/DDS/lib:/workspace/third_party/skydds/sdk/ACE_wrappers/lib
