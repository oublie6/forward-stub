# SkyDDS Docker Images

本目录只补充 SkyDDS 相关镜像与构建脚本，不替换根目录 `Dockerfile` 的通用非 SkyDDS 构建路径。

## 1. 为什么 SkyDDS 主线不再使用 Alpine

- SkyDDS 接入依赖 Go + cgo + C++ + 动态库，主线运行时需要沿用 glibc 兼容链路。
- Alpine 默认是 musl，SkyDDS 及其依赖库在 glibc 环境下更稳妥，联调与排障成本更低。
- 根目录 `Dockerfile` 仍可继续服务现有非 SkyDDS 场景；SkyDDS 主线镜像单独放在 `deploy/docker/`。

## 2. 为什么主线选择 Bookworm / glibc

- `golang:1.25-bookworm` 适合作为 SkyDDS builder，便于同时提供 Go、cgo、gcc/g++、make 和常见诊断工具。
- `debian:bookworm-slim` 适合作为运行镜像，保留 glibc 运行时与必需动态库环境，同时避免塞入大量编译工具。
- 这条链路与 `scripts/skydds/env.sh` 的目录约定一致，便于本地和容器内对齐。

## 3. 为什么 Kali 只作为 debug 镜像

- Kali 面向临时排障和安全工具使用，不适合作为常驻主服务基础镜像。
- 主服务镜像需要可控、稳定、最小化的运行环境，因此主线采用 Debian Bookworm。
- `deploy/docker/skydds-debug-kali/Dockerfile` 仅提供临时诊断入口，不应替代 runtime 镜像。

## 4. SkyDDS 目录约定

- 安装包放置目录：`third_party/skydds/packages/`
- SDK 解压目录：`third_party/skydds/sdk/`

构建脚本会同时检查这两个目录；若目录不存在或只有 `.gitkeep`，脚本会直接失败，不会假装构建成功。

## 5. 镜像说明

### `skydds-base`

- Dockerfile：`deploy/docker/skydds-base/Dockerfile`
- 基础镜像：`golang:1.25-bookworm`
- 用途：SkyDDS 版本 `forward-stub` 的 builder / base 镜像
- 内置工具：`gcc g++ make perl bash tcpdump ca-certificates file tar xz-utils libstdc++6`
- 保留目录：
  - `/workspace/third_party/skydds/packages`
  - `/workspace/third_party/skydds/sdk`

### `skydds-runtime`

- Dockerfile：`deploy/docker/skydds-runtime/Dockerfile`
- 运行镜像：`debian:bookworm-slim`
- 用途：运行带 `-tags skydds` 构建出的 `forward-stub`
- 特点：只保留应用、SkyDDS 运行所需动态库与 `tcpdump`，不附带整套编译工具链

### `skydds-debug-kali`

- Dockerfile：`deploy/docker/skydds-debug-kali/Dockerfile`
- 基础镜像：`kalilinux/kali-rolling`
- 用途：临时排障、安全工具、网络抓包与问题定位
- 限制：不是主服务镜像

## 6. 构建方式

在仓库根目录执行：

```bash
./deploy/docker/build-skydds-base.sh
./deploy/docker/build-skydds-runtime.sh
./deploy/docker/build-skydds-debug.sh
```

可选变量：

```bash
IMAGE_TAG=registry.example.com/forward-stub-skydds-base:bookworm ./deploy/docker/build-skydds-base.sh
BASE_IMAGE_TAG=forward-stub-skydds-base:bookworm IMAGE_TAG=registry.example.com/forward-stub-skydds-runtime:bookworm ./deploy/docker/build-skydds-runtime.sh
IMAGE_TAG=registry.example.com/forward-stub-skydds-debug:kali ./deploy/docker/build-skydds-debug.sh
```

说明：

- `build-skydds-runtime.sh` 默认依赖本地已存在的 `forward-stub-skydds-base:bookworm`。
- 若 base 镜像不存在，runtime 构建脚本会明确报错，要求先构建 base。

## 7. tcpdump 权限注意事项

- 容器内使用 `tcpdump` 通常需要额外能力，例如 `--cap-add NET_ADMIN --cap-add NET_RAW`。
- 某些环境还需要 `--network host` 或显式挂载目标网络命名空间。
- 若平台策略禁止提升能力，容器内即使安装了 `tcpdump` 也可能无法抓包。

示例：

```bash
docker run --rm -it \
  --cap-add NET_ADMIN \
  --cap-add NET_RAW \
  forward-stub-skydds-debug:kali
```

## 8. 验证边界

本目录提供的是 SkyDDS 镜像入口和前置检查，不替代真实联调。

- 已明确覆盖：目录约定、构建入口、运行时基础镜像选择、`tcpdump` 依赖保留
- 需要你在本机继续完成：真实 `docker build`、真实 SDK 动态库联调、容器内抓包权限验证
