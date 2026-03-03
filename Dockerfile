ARG TARGETOS=linux
ARG TARGETARCH=arm64

# 在 Go 镜像中直接构建并运行，避免依赖 BuildKit 特性（如 RUN --mount）。
FROM --platform=$TARGETOS/$TARGETARCH golang:1.25-alpine

WORKDIR /app

# 版本号可由流水线注入（例如 git tag / commit sha）。
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

# 先拷贝 go mod 与 vendor，加速缓存命中。
COPY go.mod go.sum ./
COPY vendor ./vendor

# 再拷贝源码。
COPY . .

# 直接在容器镜像内编译产物，兼容老版本 docker build。
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    GOFLAGS=-mod=vendor \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /app/forward-stub .

# 创建非 root 用户，兼容 k8s 中 runAsNonRoot 的安全策略。
RUN addgroup -S app && adduser -S -D -H -u 65532 -G app app

# 日志目录可通过 docker volume/pvc 挂载到宿主机。
VOLUME ["/var/log/forward-stub"]

# 默认使用容器内配置文件；日志级别请在配置文件 logging.level 中设置。
USER 65532:65532
ENTRYPOINT ["/app/forward-stub"]
CMD ["-config", "/app/configs/example.json"]
