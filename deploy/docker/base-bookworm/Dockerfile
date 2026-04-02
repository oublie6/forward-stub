FROM golang:1.25-bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      file \
      g++ \
      gcc \
      gzip \
      iproute2 \
      libstdc++6 \
      make \
      net-tools \
      perl \
      procps \
      tar \
      tcpdump \
      xz-utils; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
