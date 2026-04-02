ARG SKYDDS_BASE_IMAGE=forward-stub-skydds-base:bookworm
FROM ${SKYDDS_BASE_IMAGE} AS builder

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /workspace

COPY go.mod go.sum ./
COPY vendor ./vendor
COPY . .

RUN set -euo pipefail; \
    CGO_ENABLED=1 GOFLAGS=-mod=vendor go build -trimpath -tags skydds -o /out/forward-stub .

RUN set -euo pipefail; \
    sdk_root=/workspace/third_party/skydds/sdk; \
    runtime_root=/out/skydds-sdk; \
    mkdir -p "${runtime_root}"; \
    if [[ -d "${sdk_root}/lib" ]]; then cp -a "${sdk_root}/lib" "${runtime_root}/"; fi; \
    if [[ -d "${sdk_root}/DDS/lib" ]]; then \
      mkdir -p "${runtime_root}/DDS"; \
      cp -a "${sdk_root}/DDS/lib" "${runtime_root}/DDS/"; \
    fi; \
    if [[ -d "${sdk_root}/ACE_wrappers/lib" ]]; then \
      mkdir -p "${runtime_root}/ACE_wrappers"; \
      cp -a "${sdk_root}/ACE_wrappers/lib" "${runtime_root}/ACE_wrappers/"; \
    fi

FROM debian:bookworm-slim

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /app

RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
      bash \
      ca-certificates \
      libstdc++6 \
      tcpdump; \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/forward-stub /app/forward-stub
COPY --from=builder /out/skydds-sdk /opt/skydds/sdk

ENV SKY_DDS=/opt/skydds/sdk
ENV DDS_ROOT=/opt/skydds/sdk/DDS
ENV ACE_ROOT=/opt/skydds/sdk/ACE_wrappers
ENV TAO_ROOT=/opt/skydds/sdk/ACE_wrappers/TAO
ENV LD_LIBRARY_PATH=/opt/skydds/sdk/lib:/opt/skydds/sdk/DDS/lib:/opt/skydds/sdk/ACE_wrappers/lib

ENTRYPOINT ["/app/forward-stub"]
