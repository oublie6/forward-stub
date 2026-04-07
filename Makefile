# forward-stub 当前主线仅围绕 SkyDDS + deploy/docker 组织。
# 不再提供根目录旧 Dockerfile 入口。

APP_NAME ?= forward-stub
GOFLAGS ?= -mod=vendor
DOCKER_BIN ?= docker
DOCKER_PLATFORM ?= linux/arm64
RUNTIME_IMAGE ?= forward-stub:skydds-bookworm-runtime-arm64

.PHONY: test vet perf clean \
	build-skydds \
	docker-build-skydds-base docker-load-skydds-base \
	docker-build-skydds-runtime docker-load-skydds-runtime docker-run-skydds-runtime

# test: 执行全部 Go 单元测试。
test:
	GOFLAGS="$(GOFLAGS)" go test ./...

# vet: 执行 go vet 静态检查。
vet:
	GOFLAGS="$(GOFLAGS)" go vet ./...

# perf: 运行仓库现有 runtime benchmark。
perf:
	GOFLAGS="$(GOFLAGS)" go test ./src/runtime -run '^$$' -bench BenchmarkDispatchMatrix -benchmem -benchtime=2s
	GOFLAGS="$(GOFLAGS)" go test ./src/runtime -run '^$$' -bench BenchmarkScenarioForwarding -benchmem -benchtime=2s

# clean: 清理本地构建输出。
clean:
	rm -rf bin dist

# build-skydds: 本地按 SkyDDS 主线编译（依赖 scripts/skydds/env.sh）。
build-skydds:
	bash -c 'set -euo pipefail; source scripts/skydds/env.sh; CGO_ENABLED=1 GOFLAGS="$(GOFLAGS)" go build -trimpath -tags skydds -o bin/$(APP_NAME) .'

# docker-build-skydds-base: 构建并导出离线基础镜像（Bookworm，默认 aarch64 / linux/arm64）。
docker-build-skydds-base:
	DOCKER_BIN="$(DOCKER_BIN)" PLATFORM="$(DOCKER_PLATFORM)" ./deploy/docker/build-and-save-base-bookworm.sh

# docker-load-skydds-base: 导入离线基础镜像归档。
docker-load-skydds-base:
	DOCKER_BIN="$(DOCKER_BIN)" PLATFORM="$(DOCKER_PLATFORM)" ./deploy/docker/load-base-bookworm.sh

# docker-build-skydds-runtime: 构建并导出 SkyDDS 运行时镜像（Bookworm 主线，默认 aarch64 / linux/arm64）。
docker-build-skydds-runtime:
	DOCKER_BIN="$(DOCKER_BIN)" PLATFORM="$(DOCKER_PLATFORM)" ./deploy/docker/build-and-save-skydds-runtime-bookworm.sh

# docker-load-skydds-runtime: 导入 SkyDDS 运行时镜像归档。
docker-load-skydds-runtime:
	DOCKER_BIN="$(DOCKER_BIN)" PLATFORM="$(DOCKER_PLATFORM)" ./deploy/docker/load-skydds-runtime-bookworm.sh

# docker-run-skydds-runtime: 本地运行 SkyDDS 运行时镜像（需要外部挂载配置）。
docker-run-skydds-runtime:
	$(DOCKER_BIN) run --rm -it $(RUNTIME_IMAGE)
