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

# 运行参数在 docker run / kubectl run 时显式指定。
ENTRYPOINT ["/app/forward-stub"]
