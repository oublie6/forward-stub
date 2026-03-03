# Makefile 统一封装项目常用命令：构建、校验、打包、镜像与清理。
# 通过变量覆盖可在本地和 CI 共享同一套流水线入口。
APP_NAME ?= forward-stub
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GOFLAGS ?= -mod=vendor
IMAGE ?= $(APP_NAME):$(VERSION)
CCR_IMAGE ?=
CCR_REGISTRY ?= ccr.ccs.tencentyun.com
CCR_NAMESPACE ?=
CCR_REPOSITORY ?= $(APP_NAME)
CCR_TAG ?= $(VERSION)
CCR_TARGET_IMAGE ?= $(CCR_REGISTRY)/$(CCR_NAMESPACE)/$(CCR_REPOSITORY):$(CCR_TAG)
CONTAINER_NAME ?= $(APP_NAME)
RUN_ARGS ?=

.PHONY: build build-linux test perf verify vet package package-all docker-build docker-push docker-push-ccr docker-run docker-build-push docker-build-run docker-build-push-ccr clean

# build: 本机构建当前平台二进制到 bin/。
build:
	CGO_ENABLED=0 GOFLAGS="$(GOFLAGS)" go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$(APP_NAME) .

# build-linux: 调用脚本输出 linux/arm64 二进制到 dist/linux。
build-linux:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) GOOS=linux GOARCH=arm64 OUT_DIR=dist/linux ./scripts/build-linux.sh

# test: 执行全部 Go 包测试（当前主要用于可编译性验证）。
test:
	GOFLAGS="$(GOFLAGS)" go test ./...

# perf: 执行基础性能测试（runtime 笛卡尔积基准 + 本地 UDP/TCP 压测扫频）。
perf:
	GOFLAGS="$(GOFLAGS)" go test ./src/runtime -run '^$$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=2s
	GOFLAGS="$(GOFLAGS)" go run ./cmd/bench -mode both -duration 4s -warmup 1s -payload-size 512 -workers 2 -pps-sweep 2000,4000,8000 -log-level error

# verify: 每次改动建议执行（功能 + 性能）。
verify: test perf

# vet: 运行 go vet 静态诊断。
vet:
	GOFLAGS="$(GOFLAGS)" go vet ./...

# package: 仅打包 linux/arm64（aarch64）平台。
package:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/arm64" ./scripts/package.sh

# package-all: 一次性打包多平台制品。
package-all:
	APP_NAME=$(APP_NAME) VERSION=$(VERSION) TARGETS="linux/arm64 linux/amd64 windows/amd64" ./scripts/package.sh

# docker-build: 基于当前 Dockerfile 构建容器镜像。
docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

# docker-push: 推送镜像到仓库；可通过 CCR_IMAGE 覆盖目标仓库地址。
docker-push:
	@if [ -n "$(CCR_IMAGE)" ]; then \
		echo "tag $(IMAGE) -> $(CCR_IMAGE)"; \
		docker tag $(IMAGE) $(CCR_IMAGE); \
		docker push $(CCR_IMAGE); \
	else \
		echo "push $(IMAGE)"; \
		docker push $(IMAGE); \
	fi

# docker-run: 使用 host 网络模式启动本地容器；可通过 RUN_ARGS 追加参数。
docker-run:
	@if docker ps -a --format '{{.Names}}' | grep -wq "$(CONTAINER_NAME)"; then \
		echo "remove existed container $(CONTAINER_NAME)"; \
		docker rm -f $(CONTAINER_NAME); \
	fi
	docker run -d --network host --name $(CONTAINER_NAME) $(RUN_ARGS) $(IMAGE)

# docker-build-push: 一次完成镜像构建并推送。
docker-build-push: docker-build docker-push

# docker-push-ccr: 推送镜像到腾讯云 CCR 指定地址。
docker-push-ccr:
	@if [ -z "$(CCR_NAMESPACE)" ]; then \
		echo "CCR_NAMESPACE is required, example: make docker-push-ccr CCR_NAMESPACE=my-team"; \
		exit 1; \
	fi
	@echo "tag $(IMAGE) -> $(CCR_TARGET_IMAGE)"
	docker tag $(IMAGE) $(CCR_TARGET_IMAGE)
	docker push $(CCR_TARGET_IMAGE)

# docker-build-push-ccr: 一次完成镜像构建并推送到腾讯云 CCR。
docker-build-push-ccr: docker-build docker-push-ccr

# docker-build-run: 一次完成镜像构建并在本地启动容器。
docker-build-run: docker-build docker-run

# clean: 清理本地构建产物。
clean:
	rm -rf bin dist
