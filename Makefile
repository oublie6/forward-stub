# Makefile 统一封装项目常用命令：构建、校验、打包、镜像与清理。
# 通过变量覆盖可在本地和 CI 共享同一套流水线入口。
APP_NAME ?= forword-stub
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS ?= -mod=vendor

.PHONY: build build-linux test vet package package-all docker-build clean

# build: 本机构建当前平台二进制到 bin/。
build:
	CGO_ENABLED=0 GOFLAGS="$(GOFLAGS)" go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(APP_NAME) .

# build-linux: 调用脚本输出 linux/arm64 二进制到 dist/linux。
build-linux:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) GOOS=linux GOARCH=arm64 OUT_DIR=dist/linux ./scripts/build-linux.sh

# test: 执行全部 Go 包测试（当前主要用于可编译性验证）。
test:
	GOFLAGS="$(GOFLAGS)" go test ./...

# vet: 运行 go vet 静态诊断。
vet:
	GOFLAGS="$(GOFLAGS)" go vet ./...

# package: 仅打包 linux/arm64（aarch64）平台。
package:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/arm64" ./scripts/package.sh

# package-all: 一次性打包多平台制品。
package-all:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/arm64 linux/amd64 windows/amd64" ./scripts/package.sh

# docker-build: 先构建 Linux 二进制，再制作运行时镜像。
docker-build: build-linux
	docker build --build-arg BINARY_PATH=dist/linux/$(APP_NAME) -t $(APP_NAME):$(VERSION) .

# clean: 清理本地构建产物。
clean:
	rm -rf bin dist
