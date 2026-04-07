# Intentionally keep the base-image build on the arm64-only mainline.
FROM --platform=linux/arm64 golang:1.25-bookworm

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy

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
