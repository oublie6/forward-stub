# Ubuntu 18.04 arm64 userspace baseline, aligned with the vendor runtime requirements.
FROM --platform=linux/arm64 ubuntu:18.04

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy
ARG GO_VERSION=1.25.8

RUN set -eux; \
    unset HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      curl \
      file \
      g++-7 \
      gcc-7 \
      gzip \
      iproute2 \
      libc6-dev \
      libstdc++6 \
      make \
      net-tools \
      perl \
      procps \
      tar \
      tcpdump \
      wget \
      xz-utils; \
    update-alternatives --install /usr/bin/gcc gcc /usr/bin/gcc-7 70; \
    update-alternatives --install /usr/bin/g++ g++ /usr/bin/g++-7 70; \
    arch="$(dpkg --print-architecture)"; \
    case "${arch}" in \
      arm64) go_arch='arm64' ;; \
      *) echo "unsupported dpkg architecture for Go bootstrap: ${arch}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz" -o /tmp/go.tgz; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm -f /tmp/go.tgz; \
    /usr/local/go/bin/go version; \
    echo "[verify] uname -m"; uname -m; \
    echo "[verify] gcc --version"; gcc --version; \
    echo "[verify] g++ --version"; g++ --version; \
    echo "[verify] ldd --version"; ldd --version; \
    echo "[verify] getconf GNU_LIBC_VERSION"; getconf GNU_LIBC_VERSION || true; \
    echo "[verify] go version"; /usr/local/go/bin/go version; \
    echo "[verify] /usr/include/stdlib.h"; ls -l /usr/include/stdlib.h; \
    echo "[verify] /usr/include/stropts.h"; \
    if [[ -e /usr/include/stropts.h ]]; then \
      ls -l /usr/include/stropts.h; \
    else \
      echo "old userspace baseline is aligned, but stropts.h is still absent in the selected distro/userspace; next step should move to ACE / SkyDDS platform-compat handling instead of faking glibc headers"; \
    fi; \
    rm -rf /var/lib/apt/lists/*

ENV GOROOT=/usr/local/go
ENV PATH=/usr/local/go/bin:${PATH}
ENV CC=/usr/bin/gcc
ENV CXX=/usr/bin/g++

WORKDIR /workspace
