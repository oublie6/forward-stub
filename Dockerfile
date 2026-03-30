ARG TARGETOS=linux
ARG TARGETARCH=arm64

# 在 Go 镜像中直接构建并运行，避免依赖 BuildKit 特性（如 RUN --mount）。
FROM --platform=$TARGETOS/$TARGETARCH golang:1.25-alpine

WORKDIR /app

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
    go build -trimpath -ldflags "-s -w" -o /app/forward-stub .

# 拷贝 tcpdump 离线包归档并安装（无网络）。
COPY deploy/tcpdump-bundle.md /tmp/tcpdump-bundle.md
RUN mkdir -p /tmp/tcpdump-apk \
    && awk '/^```base64$/{flag=1;next}/^```$/{if(flag){exit}}flag' /tmp/tcpdump-bundle.md | base64 -d > /tmp/tcpdump.tar.gz \
    && tar -xzf /tmp/tcpdump.tar.gz -C /tmp/tcpdump-apk --strip-components=1 \
    && apk add --no-network --allow-untrusted /tmp/tcpdump-apk/*.apk.md \
    && rm -rf /tmp/tcpdump-apk /tmp/tcpdump.tar.gz /tmp/tcpdump-bundle.md

# 运行参数在 docker run / kubectl run 时显式指定。
ENTRYPOINT ["/app/forward-stub"]
