# syntax=docker/dockerfile:1.7

ARG TARGETOS=linux
ARG TARGETARCH=arm64

# 在 Go 构建镜像中直接编译源码，将完整构建链路交给 CI/CD 流水线。
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

WORKDIR /src

# 版本号可由流水线注入（例如 git tag / commit sha）。
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

# 先拷贝 go mod 与 vendor，加速缓存命中。
COPY go.mod go.sum ./
COPY vendor ./vendor

# 再拷贝源码。
COPY . .

# 生成 Linux 静态二进制，便于运行在 distroless 最小镜像。
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    GOFLAGS=-mod=vendor \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/forward-stub .

# 使用 distroless nonroot 作为最小运行时镜像：
# - 减少攻击面；
# - 无 shell 与包管理器，适合生产环境运行 Go 静态二进制。
FROM --platform=$TARGETOS/$TARGETARCH gcr.io/distroless/static-debian12:nonroot

# 应用运行目录。
WORKDIR /app

# 日志目录可通过 docker volume/pvc 挂载到宿主机。
VOLUME ["/var/log/forward-stub"]

# 拷贝 builder 阶段产物并设置 nonroot 所有权。
COPY --from=builder --chown=nonroot:nonroot /out/forward-stub /app/forward-stub
# 拷贝默认示例配置，便于容器开箱即跑（生产可挂载覆盖）。
COPY --chown=nonroot:nonroot configs/example.json /app/configs/example.json

# 以非 root 用户运行，满足容器安全基线。
USER nonroot:nonroot

# 固定入口为服务进程。
ENTRYPOINT ["/app/forward-stub"]
# 默认使用容器内配置文件；日志级别请在配置文件 logging.level 中设置。
CMD ["-config", "/app/configs/example.json"]
